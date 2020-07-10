package chanfitness

import (
	"errors"
	"testing"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/subscribe"
	"github.com/stretchr/testify/require"
)

var (
	// testNow is the current time tests will use.
	testNow = time.Unix(1592465134, 0)

	// testClock provides a mocked clock for testing.
	testClock = clock.NewTestClock(testNow)
)

// TestStartStoreError tests the starting of the store in cases where the setup
// functions fail. It does not test the mechanics of consuming events because
// these are covered in a separate set of tests.
func TestStartStoreError(t *testing.T) {
	// Ok and erroring subscribe functions are defined here to de-clutter
	// tests.
	okSubscribeFunc := func() (subscribe.Subscription, error) {
		return newMockSubscription(t), nil
	}

	errSubscribeFunc := func() (subscribe.Subscription, error) {
		return nil, errors.New("intentional test err")
	}

	tests := []struct {
		name          string
		ChannelEvents func() (subscribe.Subscription, error)
		PeerEvents    func() (subscribe.Subscription, error)
		GetChannels   func() ([]*channeldb.OpenChannel, error)
	}{
		{
			name:          "Channel events fail",
			ChannelEvents: errSubscribeFunc,
		},
		{
			name:          "Peer events fail",
			ChannelEvents: okSubscribeFunc,
			PeerEvents:    errSubscribeFunc,
		},
		{
			name:          "Get open channels fails",
			ChannelEvents: okSubscribeFunc,
			PeerEvents:    okSubscribeFunc,
			GetChannels: func() ([]*channeldb.OpenChannel, error) {
				return nil, errors.New("intentional test err")
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			store := NewChannelEventStore(&Config{
				SubscribeChannelEvents: test.ChannelEvents,
				SubscribePeerEvents:    test.PeerEvents,
				GetOpenChannels:        test.GetChannels,
				Clock:                  testClock,
			})

			err := store.Start()
			// Check that we receive an error, because the test only
			// checks for error cases.
			if err == nil {
				t.Fatalf("Expected error on startup, got: nil")
			}
		})
	}
}

// TestMonitorChannelEvents tests the store's handling of channel and peer
// events. It tests for the unexpected cases where we receive a channel open for
// an already known channel and but does not test for closing an unknown channel
// because it would require custom logic in the test to prevent iterating
// through an eventLog which does not exist. This test does not test handling
// of uptime and lifespan requests, as they are tested in their own tests.
func TestMonitorChannelEvents(t *testing.T) {
	tests := []struct {
		name string

		// generateEvents sends a set of updates into our mocked
		// subscription and returns the channel ID that events were
		// genreated for.
		generateEvents func(ctx *chanEventStoreTestCtx) wire.OutPoint

		// expectedEvents is the expected set of event types in the store.
		expectedEvents []channelEvent
	}{
		{
			name: "Channel opened, peer comes online",
			generateEvents: func(ctx *chanEventStoreTestCtx) wire.OutPoint {
				peer, _, channel := ctx.createChannel()
				ctx.peerEvent(peer, true)

				return channel
			},
			expectedEvents: []channelEvent{
				{
					eventType: peerOnlineEvent,
				},
			},
		},
		{
			name: "Duplicate channel open events",
			generateEvents: func(ctx *chanEventStoreTestCtx) wire.OutPoint {
				// Add a open channel and peer online event.
				peer, pubkey, channel := ctx.createChannel()
				ctx.peerEvent(peer, true)

				// Send a duplicate channel open event.
				ctx.sendChannelOpenedUpdate(pubkey, channel)

				return channel
			},
			expectedEvents: []channelEvent{
				{
					eventType: peerOnlineEvent,
				},
			},
		},
		{
			name: "Channel opened, peer already online",
			generateEvents: func(ctx *chanEventStoreTestCtx) wire.OutPoint {
				peer, pubkey, channel := ctx.newChannel()

				// Send an online event so that our peer is
				// tracked as already online.
				ctx.peerEvent(peer, true)

				// Send a channel opened event for our online
				// peer.
				ctx.sendChannelOpenedUpdate(pubkey, channel)

				return channel
			},
			expectedEvents: []channelEvent{
				{
					eventType: peerOnlineEvent,
				},
			},
		},

		{
			name: "Channel opened, peer offline, closed",
			generateEvents: func(ctx *chanEventStoreTestCtx) wire.OutPoint {
				// Create a channel.
				peer, _, channel := ctx.createChannel()

				// Publish peer offline event.
				ctx.peerEvent(peer, false)

				// Close the channel.
				ctx.closeChannel(channel)

				return channel
			},
			expectedEvents: []channelEvent{
				{
					eventType: peerOfflineEvent,
				},
			},
		},
		{
			name: "Event after channel close not recorded",
			generateEvents: func(ctx *chanEventStoreTestCtx) wire.OutPoint {
				// Create a channel.
				peer, _, channel := ctx.createChannel()

				// Immediately close the channel.
				ctx.closeChannel(channel)

				// Add a peer online event after we have closed.
				ctx.peerEvent(peer, true)

				return channel
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			testCtx := newChanEventStoreTestCtx(t)
			testCtx.start()

			// Add events to the store.
			chanPoint := test.generateEvents(testCtx)

			// Shutdown the store so that we can safely access our
			// events.
			testCtx.stop()

			testCtx.assertEvents(chanPoint, test.expectedEvents)
		})
	}
}

// TestStoreFlapCount tests flushing of flap counts to disk on timer ticks and
// on store shutdown.
func TestStoreFlapCount(t *testing.T) {
	testCtx := newChanEventStoreTestCtx(t)
	testCtx.start()

	pubkey, _, channel := testCtx.createChannel()
	testCtx.peerEvent(pubkey, false)

	// Now, we tick our flap count ticker. We expect our main goroutine to
	// flush our tick count to disk.
	testCtx.tickFlapCount()

	// Since we just tracked a offline event, we expect a single flap for
	// our peer.
	expectedUpdate := map[route.Vertex]map[wire.OutPoint]uint32{
		pubkey: {
			channel: 1,
		},
	}

	testCtx.assertFlapCountUpdated()
	testCtx.assertFlapCountUpdates(expectedUpdate)

	// Create three events for out peer, online/offline/online.
	testCtx.peerEvent(pubkey, true)
	testCtx.peerEvent(pubkey, false)
	testCtx.peerEvent(pubkey, true)

	// Trigger another write.
	testCtx.tickFlapCount()

	// Since we have processed 2 offline events for our peer, we update our
	// expected online map to have a flap count of 2 for this peer.
	expectedUpdate[pubkey][channel] = 2
	testCtx.assertFlapCountUpdated()
	testCtx.assertFlapCountUpdates(expectedUpdate)

	testCtx.stop()
}

// TestGetChanInfo tests the GetChanInfo function for the cases where a channel
// is known and unknown to the store. It also tests the unexpected edge cases
// where a tracked channel does not have any events recorded.
func TestGetChanInfo(t *testing.T) {
	// Set a non-zero flap count
	var flapCount uint32 = 20

	twoHoursAgo := testNow.Add(time.Hour * -2)
	fourHoursAgo := testNow.Add(time.Hour * -4)

	tests := []struct {
		name string

		channelPoint wire.OutPoint

		// events is the set of events we expect to find in the channel
		// store.
		events []*channelEvent

		// openedAt is the time the channel is recorded as open by the
		// store.
		openedAt time.Time

		// closedAt is the time the channel is recorded as closed by the
		// store. If the channel is still open, this value is zero.
		closedAt time.Time

		// channelFound is true if we expect to find the channel in the
		// store.
		channelFound bool

		// expectedUptime is the total uptime we expect.
		expectedUptime time.Duration

		// expectedLifetime is the total lifetime we expect.
		expectedLifetime time.Duration

		expectedError error
	}{
		{
			name:          "No events",
			channelFound:  true,
			expectedError: nil,
			openedAt:      testNow,
		},
		{
			name: "50% Uptime",
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOnlineEvent,
				},
				{
					timestamp: twoHoursAgo,
					eventType: peerOfflineEvent,
				},
			},
			openedAt:         fourHoursAgo,
			expectedUptime:   time.Hour * 2,
			expectedLifetime: time.Hour * 4,
			channelFound:     true,
			expectedError:    nil,
		},
		{
			name:          "Channel not found",
			channelFound:  false,
			expectedError: ErrChannelNotFound,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			ctx := newChanEventStoreTestCtx(t)

			// If we're supposed to find the channel for this test,
			// add events for it to the store.
			if test.channelFound {
				ctx.store.channels[test.channelPoint] = &chanEventLog{
					events:    test.events,
					now:       testClock.Now,
					openedAt:  test.openedAt,
					closedAt:  test.closedAt,
					flapCount: flapCount,
				}
			}

			ctx.start()

			defer func() {
				ctx.stop()
			}()

			info, err := ctx.store.GetChanInfo(test.channelPoint)
			require.Equal(t, test.expectedError, err)

			// If we expect an error, our info will be nil so we
			// exit early rather than check its values.
			if err != nil {
				return
			}

			require.Equal(t, test.expectedUptime, info.Uptime)
			require.Equal(t, test.expectedLifetime, info.Lifetime)
			require.Equal(t, flapCount, info.FlapCount)
		})
	}
}

// TestAddChannel tests that channels are added to the event store with
// appropriate timestamps. This test addresses a bug where offline channels
// did not have an opened time set, and checks that an online event is set for
// peers that are online at the time that a channel is opened.
func TestAddChannel(t *testing.T) {
	ctx := newChanEventStoreTestCtx(t)
	ctx.start()

	// Create a channel for a peer that is not online yet.
	_, _, channel1 := ctx.createChannel()

	// Get a set of values for another channel, but do not create it yet.
	//
	peer2, pubkey2, channel2 := ctx.newChannel()
	ctx.peerEvent(peer2, true)
	ctx.sendChannelOpenedUpdate(pubkey2, channel2)

	ctx.stop()

	// Assert that our peer that was offline on connection has no events
	// and our peer that was online on connection has one.
	ctx.assertEvents(channel1, []channelEvent{})
	ctx.assertEvents(channel2, []channelEvent{
		{
			eventType: peerOnlineEvent,
		},
	})
}
