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
	"github.com/lightningnetwork/lnd/ticker"
)

var (
	// errShuttingDown is returned when the store cannot respond to a query
	// because it has received the shutdown signal.
	errShuttingDown = errors.New("channel event store shutting down")

	// ErrChannelNotFound is returned when a query is made for a channel
	// that the event store does not have knowledge of.
	ErrChannelNotFound = errors.New("channel not found in event store")
)

// ChannelEventStore maintains a set of event logs for the node's channels to
// provide insight into the performance and health of channels.
type ChannelEventStore struct {
	cfg *Config

	// channels maps channel points to event logs.
	channels map[wire.OutPoint]*chanEventLog

	// peers tracks the current online status of peers based on online
	// and offline events.
	peers map[route.Vertex]bool

	// chanInfoRequests serves requests for information about our channel.
	chanInfoRequests chan channelInfoRequest

	quit chan struct{}

	wg sync.WaitGroup
}

// FlapCountFlushRate determines how often we write peer total flap count to
// disk.
var FlapCountFlushRate = time.Hour

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

	// WriteFlapCounts records the flap count for a set of peers on disk.
	WriteFlapCount func(map[route.Vertex]map[wire.OutPoint]uint32) error

	// ReadFlapCount gets the flap count for a peer on disk.
	ReadFlapCount func(_ route.Vertex, _ wire.OutPoint) (uint32, error)

	// FlapCountTicker is a ticker which controls how often we flush our
	// peer's flap count to disk.
	FlapCountTicker ticker.Ticker
}

type channelInfoRequest struct {
	channelPoint wire.OutPoint
	start        time.Time
	end          time.Time
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
		channels:         make(map[wire.OutPoint]*chanEventLog),
		peers:            make(map[route.Vertex]bool),
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

	c.cfg.FlapCountTicker.Stop()

	// Stop the consume goroutine.
	close(c.quit)

	c.wg.Wait()
}

// addChannel adds a new channel to the ChannelEventStore's map of channels with
// an initial peer online state (if the peer is online). If the channel is
// already present in the map, the function returns early. This function should
// be called to add existing channels on startup and when open channel events
// are observed.
func (c *ChannelEventStore) addChannel(channelPoint wire.OutPoint,
	peer route.Vertex) {

	// Check for the unexpected case where the channel is already in the
	// store.
	_, ok := c.channels[channelPoint]
	if ok {
		log.Errorf("Channel %v duplicated in channel store",
			channelPoint)
		return
	}

	// Read flap count from disk, ignoring errors that we get when we have
	// not recorded flap count for this peer before.
	flapCount, err := c.cfg.ReadFlapCount(peer, channelPoint)
	switch err {
	// If we do not have any records for this peer fallthrough.
	case channeldb.ErrNoPeerBucket:

	// If we do not have any records for this channel fallthrough.
	case channeldb.ErrNoPeerChanBucket:

	// If we do not have flap count recorded for this peer (which may happen
	// for existing peers where we have not flushed our value to disk yet),
	// fallthrough.
	case channeldb.ErrNoFlapCount:

	// Fallthrough on nil error.
	case nil:

	// Return if we get an unexpected error.
	default:
		log.Errorf("Could not get flap count: %v", err)
		return
	}

	// Create an event log for the channel with our historic flap count.
	eventLog := newEventLog(channelPoint, peer, c.cfg.Clock.Now, flapCount)

	// If the peer is already online, add a peer online event to record
	// the starting state of the peer.
	if c.peers[peer] {
		eventLog.add(peerOnlineEvent)
	}

	c.channels[channelPoint] = eventLog
}

// closeChannel records a closed time for a channel, and returns early is the
// channel is not known to the event store.
func (c *ChannelEventStore) closeChannel(channelPoint wire.OutPoint) {
	// Check for the unexpected case where the channel is unknown to the
	// store.
	eventLog, ok := c.channels[channelPoint]
	if !ok {
		log.Errorf("Close channel %v unknown to store", channelPoint)
		return
	}

	eventLog.close()
}

// peerEvent adds a peer online or offline event to all channels we currently
// have open with a peer.
func (c *ChannelEventStore) peerEvent(peer route.Vertex, event eventType) {
	// Track current online status of peers in the channelEventStore.
	c.peers[peer] = event == peerOnlineEvent

	for _, eventLog := range c.channels {
		if eventLog.peer == peer {
			eventLog.add(event)
		}
	}
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
	// On exit, we will cancel our subscriptions and write our most recent
	// flap counts to disk. This ensures that we have consistent data in
	// the case of a graceful shutdown. If we do not shutdown gracefully,
	// our worst case is data from our last flap count tick (1H).
	defer func() {
		defer c.wg.Done()

		subscriptions.cancel()

		if err := c.recordFlapCount(); err != nil {
			log.Errorf("error recording flap on shutdown: %v", err)
		}
	}()

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
				c.closeChannel(event.CloseSummary.ChanPoint)
			}

		// Process peer online and offline events.
		case e := <-subscriptions.peerUpdates:
			switch event := e.(type) {
			// We have reestablished a connection with our peer,
			// and should record an online event for any channels
			// with that peer.
			case peernotifier.PeerOnlineEvent:
				c.peerEvent(event.PubKey, peerOnlineEvent)

			// We have lost a connection with our peer, and should
			// record an offline event for any channels with that
			// peer.
			case peernotifier.PeerOfflineEvent:
				c.peerEvent(event.PubKey, peerOfflineEvent)
			}

		// Serve all requests for channel lifetime.
		case req := <-c.chanInfoRequests:
			var resp channelInfoResponse

			resp.info, resp.err = c.getChanInfo(req)
			req.responseChan <- resp

		case <-c.cfg.FlapCountTicker.Ticks():
			if err := c.recordFlapCount(); err != nil {
				log.Errorf("could not record flap "+
					"count: %v", err)
			}

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

	// FlapCount is the number of times the channel peer has gone offline
	// over our record of the peer. Note that these flaps are tracked across
	// channels (some of these flaps may have been before this channel was
	// opened).
	FlapCount uint32
}

// GetChanInfo gets all the information we have on a channel in the event store.
func (c *ChannelEventStore) GetChanInfo(channelPoint wire.OutPoint) (*ChannelInfo,
	error) {

	request := channelInfoRequest{
		channelPoint: channelPoint,
		start:        time.Time{},
		end:          time.Time{},
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
// the full life of the channel.
func (c *ChannelEventStore) getChanInfo(req channelInfoRequest) (*ChannelInfo,
	error) {

	// Look for the channel in our current set.
	channel, ok := c.channels[req.channelPoint]
	if !ok {
		return nil, ErrChannelNotFound
	}

	// If our channel is not closed, we want to calculate uptime until the
	// present.
	endTime := channel.closedAt
	if endTime.IsZero() {
		endTime = c.cfg.Clock.Now()
	}

	uptime, err := channel.uptime(channel.openedAt, endTime)
	if err != nil {
		return nil, err
	}

	return &ChannelInfo{
		Lifetime:  endTime.Sub(channel.openedAt),
		Uptime:    uptime,
		FlapCount: channel.flapCount,
	}, nil
}

// recordFlapCount will record our flap count for each channel we are currently
// tracking.
func (c *ChannelEventStore) recordFlapCount() error {
	log.Debugf("recording flap count for: %v channels", len(c.channels))

	flapCounts := make(map[route.Vertex]map[wire.OutPoint]uint32)

	for _, channel := range c.channels {
		// Don't bother storing flap counts for channels that have not
		// flapped.
		if channel.flapCount == 0 {
			continue
		}

		// Get our peer's current set of channels and flap counts, if
		// we don't yet have any channels, we create a map.
		channels, ok := flapCounts[channel.peer]
		if !ok {
			channels = make(map[wire.OutPoint]uint32)
		}

		// Add the flap count for our channel to our set of channels and
		// set the entry for our peer.
		channels[channel.channelPoint] = channel.flapCount
		flapCounts[channel.peer] = channels
	}

	return c.cfg.WriteFlapCount(flapCounts)
}
