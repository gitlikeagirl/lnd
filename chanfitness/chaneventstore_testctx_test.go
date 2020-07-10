package chanfitness

import (
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/channelnotifier"
	"github.com/lightningnetwork/lnd/peernotifier"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/subscribe"
	"github.com/stretchr/testify/require"
)

// timeout is the amount of time we allow our blocking test calls.
var timeout = time.Second

// chanEventStoreTestCtx is a helper struct which can be used to test the
// channel event store.
type chanEventStoreTestCtx struct {
	t *testing.T

	store *ChannelEventStore

	// existingChannels is the set of channels that the store should be
	// created with. This field must be set before starting the store
	// otherwise the store will not take them into account.
	existingChannels []*channeldb.OpenChannel

	channelSubscription *mockSubscription
	peerSubscription    *mockSubscription

	// channelIdx is an index which will be used to deterministically add
	// channels with unique outpoints to the test context.
	channelIdx int

	// stopped is closed when our test context is fully shutdown. It is
	// used to prevent calling of functions which can only be called after
	// shutdown.
	stopped chan struct{}
}

// newChanEventStoreTestCtx creates a test context which can be used to test
// the event store.
func newChanEventStoreTestCtx(t *testing.T) *chanEventStoreTestCtx {
	testCtx := &chanEventStoreTestCtx{
		t:                   t,
		channelSubscription: newMockSubscription(t),
		peerSubscription:    newMockSubscription(t),
		stopped:             make(chan struct{}),
	}

	cfg := &Config{
		Clock: testClock,
		SubscribeChannelEvents: func() (subscribe.Subscription, error) {
			return testCtx.channelSubscription, nil
		},
		SubscribePeerEvents: func() (subscribe.Subscription, error) {
			return testCtx.peerSubscription, nil
		},
		GetOpenChannels: func() ([]*channeldb.OpenChannel, error) {
			return testCtx.existingChannels, nil
		},
	}

	testCtx.store = NewChannelEventStore(cfg)

	return testCtx
}

// start starts the channel event store's subscribe servers and the store
// itself.
func (c *chanEventStoreTestCtx) start() {
	require.NoError(c.t, c.store.Start())
}

// stop stops the channel event store's subscribe servers and the store itself.
func (c *chanEventStoreTestCtx) stop() {
	c.channelSubscription.stop()
	c.peerSubscription.stop()
	c.store.Stop()

	close(c.stopped)
}

// newChannel creates a new, unique test channel.
func (c *chanEventStoreTestCtx) newChannel() (route.Vertex, *btcec.PublicKey,
	wire.OutPoint) {

	// Create a new pubkey/privkey.
	privKey, err := btcec.NewPrivateKey(btcec.S256())
	require.NoError(c.t, err)
	pubKey := privKey.PubKey()

	// Create vertex from our pubkey.
	vertex, err := route.NewVertexFromBytes(pubKey.SerializeCompressed())
	require.NoError(c.t, err)

	// Create a channel point using our channel index, then increment it.
	chanPoint := wire.OutPoint{
		Hash:  [chainhash.HashSize]byte{1, 2, 3},
		Index: uint32(c.channelIdx),
	}
	c.channelIdx++

	return vertex, pubKey, chanPoint
}

// createChannel creates a new channel, notifies the event store that it has
// been created and returns the peer vertex, pubkey and channel point.
func (c *chanEventStoreTestCtx) createChannel() (route.Vertex, *btcec.PublicKey,
	wire.OutPoint) {

	vertex, pubKey, chanPoint := c.newChannel()
	c.sendChannelOpenedUpdate(pubKey, chanPoint)

	return vertex, pubKey, chanPoint
}

// closeChannel sends a close channel event to our subscribe server.
func (c *chanEventStoreTestCtx) closeChannel(channel wire.OutPoint) {
	update := channelnotifier.ClosedChannelEvent{
		CloseSummary: &channeldb.ChannelCloseSummary{
			ChanPoint: channel,
		},
	}

	c.channelSubscription.sendUpdate(update)
}

// peerEvent sends a peer online or offline event to the store for the peer
// provided.
func (c *chanEventStoreTestCtx) peerEvent(peer route.Vertex, online bool) {
	var update interface{}
	if online {
		update = peernotifier.PeerOnlineEvent{PubKey: peer}
	} else {
		update = peernotifier.PeerOfflineEvent{PubKey: peer}
	}

	c.peerSubscription.sendUpdate(update)
}

// sendChannelOpenedUpdate notifies the test event store that a channel has
// been opened.
func (c *chanEventStoreTestCtx) sendChannelOpenedUpdate(pubkey *btcec.PublicKey,
	channel wire.OutPoint) {

	update := channelnotifier.OpenChannelEvent{
		Channel: &channeldb.OpenChannel{
			FundingOutpoint: channel,
			IdentityPub:     pubkey,
		},
	}

	c.channelSubscription.sendUpdate(update)
}

// assertEvents asserts that a channel event store has an expected set of
// events. This function can only be called once the test context's store has
// been stopped.
func (c *chanEventStoreTestCtx) assertEvents(channel wire.OutPoint,
	events []channelEvent) {

	// Fail if our store has not been stopped, because we will have data
	// races if we try to assert a set of events while the store may still
	// be updating them.
	select {
	case <-c.stopped:

	default:
		c.t.Fatalf("cannot assert events with an active store")
	}

	eventLog, ok := c.store.channels[channel]
	require.True(c.t, ok, "channel not found")

	// Check that our opened time is always non-zero, covering a
	// previous bug where open times were not set for channels with
	// peers who are offline when we add the channel to the store.
	require.False(c.t, eventLog.openedAt.IsZero(), "zero open time")

	// Assert that we have the number of events we expect.
	actualEvents := eventLog.events
	require.Len(c.t, actualEvents, len(events), "event count wrong")

	// Run through our expected events and check that we have the
	// right kind. We do not check time (at this stage) because it
	// is not mocked.
	for i, event := range events {
		require.Equal(c.t, event.eventType, actualEvents[i].eventType,
			"event type wrong")
	}
}

// mockSubscription is a mock subscription client that blocks on sends into the
// updates channel. We use this mock rather than an actual subscribe client
// because they do not block, which makes tests race (because we have no way
// to guarantee that the test client consumes the update before shutdown).
type mockSubscription struct {
	t       *testing.T
	updates chan interface{}
	quit    chan struct{}
}

// newMockSubscription creates a mock subscription.
func newMockSubscription(t *testing.T) *mockSubscription {
	return &mockSubscription{
		t:       t,
		updates: make(chan interface{}),
		quit:    make(chan struct{}),
	}
}

// stop can be used by the test to stop our subscription. We close the updates
// channel so that any further attempts to write will panic, and close the quit
// channel, which is the behaviour of the real subscribe client.
func (m *mockSubscription) stop() {
	close(m.quit)
}

// sendUpdate sends an update into our updates channel, mocking the dispatch of
// an update from a subscription server. This call will fail the test if the
// update is not consumed within our timeout.
func (m *mockSubscription) sendUpdate(update interface{}) {
	select {
	case m.updates <- update:

	case <-time.After(timeout):
		m.t.Fatalf("update: %v timeout", update)
	}
}

// Updates returns the updates channel for the mock.
func (m *mockSubscription) Updates() <-chan interface{} {
	return m.updates
}

// Quit returns the mock's quit channel. We close this when we stop the mock.
func (m *mockSubscription) Quit() <-chan struct{} {
	return m.quit
}

// Cancel should be called in case the client no longer wants to subscribe for
// updates from the server.
func (m *mockSubscription) Cancel() {
	close(m.updates)
}
