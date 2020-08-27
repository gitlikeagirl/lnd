// Package chanfitness monitors the behaviour of channels to provide insight
// into the health and performance of a channel. This is achieved by maintaining
// an event store which tracks events for each channel.
//
// Lifespan: the period that the channel has been known to the scoring system.
// Note that lifespan may not equal the channel's full lifetime because data is
// not currently persisted.
//
// Uptime: the total time within a given period that the channel's remote peer
// has been online.
package chanfitness

import (
	"errors"
	"sync"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/channelnotifier"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/peernotifier"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/subscribe"
)

var (
	// errShuttingDown is returned when the store cannot respond to a query
	// because it has received the shutdown signal.
	errShuttingDown = errors.New("channel event store shutting down")

	// ErrChannelNotFound is returned when a query is made for a channel
	// that the event store does not have knowledge of.
	ErrChannelNotFound = errors.New("channel not found in event store")

	// ErrPeerNotFound is returned when a query is made for a channel
	// that has a peer that the event store is not currently tracking.
	ErrPeerNotFound = errors.New("peer not found in event store")
)

// ChannelEventStore maintains a set of event logs for the node's channels to
// provide insight into the performance and health of channels.
type ChannelEventStore struct {
	cfg *Config

	// peers tracks all of our currently monitored peers and their channels.
	peers map[route.Vertex]peerMonitor

	// chanInfoRequests serves requests for information about our channel.
	chanInfoRequests chan channelInfoRequest

	quit chan struct{}

	wg sync.WaitGroup
}

// Config provides the event store with functions required to monitor channel
// activity. All elements of the config must be non-nil for the event store to
// operate.
type Config struct {
	// SubscribeChannelEvents provides a subscription client which provides
	// a stream of channel events.
	SubscribeChannelEvents func() (subscribe.Subscription, error)

	// SubscribePeerEvents provides a subscription client which provides a
	// stream of peer online/offline events.
	SubscribePeerEvents func() (subscribe.Subscription, error)

	// GetOpenChannels provides a list of existing open channels which is
	// used to populate the ChannelEventStore with a set of channels on
	// startup.
	GetOpenChannels func() ([]*channeldb.OpenChannel, error)

	// Clock is the time source that the subsystem uses, provided here
	// for ease of testing.
	Clock clock.Clock
}

type channelInfoRequest struct {
	peer         route.Vertex
	channelPoint wire.OutPoint
	responseChan chan channelInfoResponse
}

type channelInfoResponse struct {
	info *ChannelInfo
	err  error
}

// NewChannelEventStore initializes an event store with the config provided.
// Note that this function does not start the main event loop, Start() must be
// called.
func NewChannelEventStore(config *Config) *ChannelEventStore {
	store := &ChannelEventStore{
		cfg:              config,
		peers:            make(map[route.Vertex]peerMonitor),
		chanInfoRequests: make(chan channelInfoRequest),
		quit:             make(chan struct{}),
	}

	return store
}

// Start adds all existing open channels to the event store and starts the main
// loop which records channel and peer events, and serves requests for
// information from the store. If this function fails, it cancels its existing
// subscriptions and returns an error.
func (c *ChannelEventStore) Start() error {
	// Create a subscription to channel events.
	channelClient, err := c.cfg.SubscribeChannelEvents()
	if err != nil {
		return err
	}

	// Create a subscription to peer events. If an error occurs, cancel the
	// existing subscription to channel events and return.
	peerClient, err := c.cfg.SubscribePeerEvents()
	if err != nil {
		channelClient.Cancel()
		return err
	}

	// cancel should be called to cancel all subscriptions if an error
	// occurs.
	cancel := func() {
		channelClient.Cancel()
		peerClient.Cancel()
	}

	// Add the existing set of channels to the event store. This is required
	// because channel events will not be triggered for channels that exist
	// at startup time.
	channels, err := c.cfg.GetOpenChannels()
	if err != nil {
		cancel()
		return err
	}

	log.Infof("Adding %v channels to event store", len(channels))

	for _, ch := range channels {
		peerKey, err := route.NewVertexFromBytes(
			ch.IdentityPub.SerializeCompressed(),
		)
		if err != nil {
			cancel()
			return err
		}

		// Add existing channels to the channel store with an initial
		// peer online or offline event.
		c.addChannel(ch.FundingOutpoint, peerKey)
	}

	// Start a goroutine that consumes events from all subscriptions.
	c.wg.Add(1)
	go c.consume(&subscriptions{
		channelUpdates: channelClient.Updates(),
		peerUpdates:    peerClient.Updates(),
		cancel:         cancel,
	})

	return nil
}

// Stop terminates all goroutines started by the event store.
func (c *ChannelEventStore) Stop() {
	log.Info("Stopping event store")

	// Stop the consume goroutine.
	close(c.quit)

	c.wg.Wait()
}

// addChannel checks whether we are already tracking a channel's peer, creates a
// new peer log to track it if we are not yet monitoring it, and adds the
// channel.
func (c *ChannelEventStore) addChannel(channelPoint wire.OutPoint,
	peer route.Vertex) {

	peerMonitor, ok := c.peers[peer]
	if !ok {
		peerMonitor = newPeerLog(c.cfg.Clock)
		c.peers[peer] = peerMonitor
	}

	if err := peerMonitor.addChannel(channelPoint); err != nil {
		log.Errorf("could not add channel: %v", err)
	}
}

// closeChannel records a closed time for a channel, and returns early is the
// channel is not known to the event store. We log warnings (rather than errors)
// when we cannot find a peer/channel because channels that we restore from a
// static channel backup do not have their open notified, so the event store
// never learns about them, but they are closed using the regular flow so we
// will try to remove them on close. At present, we cannot easily distinguish
// between these closes and others.
func (c *ChannelEventStore) closeChannel(channelPoint wire.OutPoint,
	peer route.Vertex) {

	peerMonitor, ok := c.peers[peer]
	if !ok {
		log.Warnf("peer not known to store: %v", peer)
		return
	}

	if err := peerMonitor.removeChannel(channelPoint); err != nil {
		log.Warnf("could not remove channel: %v", err)
	}
}

// peerEvent creates a peer monitor for a peer if we do not currently have
// one, and adds an online event to it.
func (c *ChannelEventStore) peerEvent(peer route.Vertex, online bool) {
	// If we are not currently tracking events for this peer, add a peer
	// log for it.
	peerMonitor, ok := c.peers[peer]
	if !ok {
		peerMonitor = newPeerLog(c.cfg.Clock)
		c.peers[peer] = peerMonitor
	}

	peerMonitor.onlineEvent(online)
}

// subscriptions abstracts away from subscription clients to allow for mocking.
type subscriptions struct {
	channelUpdates <-chan interface{}
	peerUpdates    <-chan interface{}
	cancel         func()
}

// consume is the event store's main loop. It consumes subscriptions to update
// the event store with channel and peer events, and serves requests for channel
// uptime and lifespan.
func (c *ChannelEventStore) consume(subscriptions *subscriptions) {
	defer c.wg.Done()
	defer subscriptions.cancel()

	// Consume events until the channel is closed.
	for {
		select {
		// Process channel opened and closed events.
		case e := <-subscriptions.channelUpdates:
			switch event := e.(type) {
			// A new channel has been opened, we must add the
			// channel to the store and record a channel open event.
			case channelnotifier.OpenChannelEvent:
				compressed := event.Channel.IdentityPub.SerializeCompressed()
				peerKey, err := route.NewVertexFromBytes(
					compressed,
				)
				if err != nil {
					log.Errorf("Could not get vertex "+
						"from: %v", compressed)
				}

				c.addChannel(
					event.Channel.FundingOutpoint, peerKey,
				)

			// A channel has been closed, we must remove the channel
			// from the store and record a channel closed event.
			case channelnotifier.ClosedChannelEvent:
				compressed := event.CloseSummary.RemotePub.SerializeCompressed()
				peerKey, err := route.NewVertexFromBytes(
					compressed,
				)
				if err != nil {
					log.Errorf("Could not get vertex "+
						"from: %v", compressed)
					continue
				}

				c.closeChannel(
					event.CloseSummary.ChanPoint, peerKey,
				)
			}

		// Process peer online and offline events.
		case e := <-subscriptions.peerUpdates:
			switch event := e.(type) {
			// We have reestablished a connection with our peer,
			// and should record an online event for any channels
			// with that peer.
			case peernotifier.PeerOnlineEvent:
				c.peerEvent(event.PubKey, true)

			// We have lost a connection with our peer, and should
			// record an offline event for any channels with that
			// peer.
			case peernotifier.PeerOfflineEvent:
				c.peerEvent(event.PubKey, false)
			}

		// Serve all requests for channel lifetime.
		case req := <-c.chanInfoRequests:
			var resp channelInfoResponse

			resp.info, resp.err = c.getChanInfo(req)
			req.responseChan <- resp

		// Exit if the store receives the signal to shutdown.
		case <-c.quit:
			return
		}
	}
}

// ChannelInfo provides the set of information that the event store has recorded
// for a channel.
type ChannelInfo struct {
	// Lifetime is the total amount of time we have monitored the channel
	// for.
	Lifetime time.Duration

	// Uptime is the total amount of time that the channel peer has been
	// observed as online during the monitored lifespan.
	Uptime time.Duration
}

// GetChanInfo gets all the information we have on a channel in the event store.
func (c *ChannelEventStore) GetChanInfo(channelPoint wire.OutPoint,
	peer route.Vertex) (*ChannelInfo, error) {

	request := channelInfoRequest{
		peer:         peer,
		channelPoint: channelPoint,
		responseChan: make(chan channelInfoResponse),
	}

	// Send a request for the channel's information to the main event loop,
	// or return early with an error if the store has already received a
	// shutdown signal.
	select {
	case c.chanInfoRequests <- request:
	case <-c.quit:
		return nil, errShuttingDown
	}

	// Return the response we receive on the response channel or exit early
	// if the store is instructed to exit.
	select {
	case resp := <-request.responseChan:
		return resp.info, resp.err

	case <-c.quit:
		return nil, errShuttingDown
	}
}

// getChanInfo collects channel information for a channel. It gets uptime over
// the full lifetime of the channel.
func (c *ChannelEventStore) getChanInfo(req channelInfoRequest) (*ChannelInfo,
	error) {

	peerMonitor, ok := c.peers[req.peer]
	if !ok {
		return nil, ErrPeerNotFound
	}

	lifetime, uptime, err := peerMonitor.channelUptime(req.channelPoint)
	if err != nil {
		return nil, err
	}

	return &ChannelInfo{
		Lifetime: lifetime,
		Uptime:   uptime,
	}, nil
}
