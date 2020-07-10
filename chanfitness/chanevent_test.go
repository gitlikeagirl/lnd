package chanfitness

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAdd tests adding events to an event log. It tests the case where the
// channel is open, and should have an event added, and the case where it is
// closed and the event should not be added.
func TestAdd(t *testing.T) {
	tests := []struct {
		name           string
		eventLog       *chanEventLog
		event          eventType
		expectedEvents []*channelEvent
	}{
		{
			name: "Channel open",
			eventLog: &chanEventLog{
				now: testClock.Now,
			},
			event: peerOnlineEvent,
			expectedEvents: []*channelEvent{
				&channelEvent{
					eventType: peerOnlineEvent,
					timestamp: testNow,
				},
			},
		},
		{
			name: "Channel closed, event not added",
			eventLog: &chanEventLog{
				now:      testClock.Now,
				closedAt: testNow,
			},
			event:          peerOnlineEvent,
			expectedEvents: nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			test.eventLog.add(test.event)

			require.Equal(
				t, test.expectedEvents, test.eventLog.events,
			)
		})
	}
}

// TestGetOnlinePeriod tests the getOnlinePeriod function. It tests the case
// where no events present, and the case where an additional online period
// must be added because the event log ends on an online event.
func TestGetOnlinePeriod(t *testing.T) {
	fourHoursAgo := testNow.Add(time.Hour * -4)
	threeHoursAgo := testNow.Add(time.Hour * -3)
	twoHoursAgo := testNow.Add(time.Hour * -2)
	oneHourAgo := testNow.Add(time.Hour * -1)

	tests := []struct {
		name           string
		events         []*channelEvent
		expectedOnline []*onlinePeriod
		openedAt       time.Time
		closedAt       time.Time
	}{
		{
			name: "No events",
		},
		{
			name: "Start on online period",
			events: []*channelEvent{
				{
					timestamp: threeHoursAgo,
					eventType: peerOnlineEvent,
				},
				{
					timestamp: twoHoursAgo,
					eventType: peerOfflineEvent,
				},
			},
			expectedOnline: []*onlinePeriod{
				{
					start: threeHoursAgo,
					end:   twoHoursAgo,
				},
			},
		},
		{
			name: "Start on offline period",
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOfflineEvent,
				},
			},
		},
		{
			name: "End on an online period, channel not closed",
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOnlineEvent,
				},
			},
			expectedOnline: []*onlinePeriod{
				{
					start: fourHoursAgo,
					end:   testNow,
				},
			},
		},
		{
			name: "End on an online period, channel closed",
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOnlineEvent,
				},
			},
			expectedOnline: []*onlinePeriod{
				{
					start: fourHoursAgo,
					end:   oneHourAgo,
				},
			},
			closedAt: oneHourAgo,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			score := &chanEventLog{
				events:   test.events,
				now:      testClock.Now,
				openedAt: test.openedAt,
				closedAt: test.closedAt,
			}

			online := score.getOnlinePeriods()

			require.Equal(t, test.expectedOnline, online)
		})

	}
}

// TestUptime tests channel uptime calculation based on its event log.
func TestUptime(t *testing.T) {
	fourHoursAgo := testNow.Add(time.Hour * -4)
	threeHoursAgo := testNow.Add(time.Hour * -3)
	twoHoursAgo := testNow.Add(time.Hour * -2)
	oneHourAgo := testNow.Add(time.Hour * -1)

	tests := []struct {
		name string

		// opened at is the time the channel was recorded as being open,
		// and is never expected to be zero.
		openedAt time.Time

		// closed at is the time the channel was recorded as being
		// closed, and can have a zero value if the channel is not
		// closed.
		closedAt time.Time

		// events is the set of event log that we are calculating uptime
		// for.
		events []*channelEvent

		// startTime is the beginning of the period that we are
		// calculating uptime for, it cannot have a zero value.
		startTime time.Time

		// endTime is the end of the period that we are calculating
		// uptime for, it cannot have a zero value.
		endTime time.Time

		// expectedUptime is the amount of uptime we expect to be
		// calculated over the period specified by startTime and
		// endTime.
		expectedUptime time.Duration

		// expectErr is set to true if we expect an error to be returned
		// when calling the uptime function.
		expectErr bool
	}{
		{
			name:      "End before start",
			endTime:   threeHoursAgo,
			startTime: testNow,
			expectErr: true,
		},
		{
			name:      "Zero end time",
			expectErr: true,
		},
		{
			name:     "Online event and closed",
			openedAt: fourHoursAgo,
			closedAt: oneHourAgo,
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOnlineEvent,
				},
			},
			startTime:      fourHoursAgo,
			endTime:        testNow,
			expectedUptime: time.Hour * 3,
		},
		{
			name:     "Online event and not closed",
			openedAt: fourHoursAgo,
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOnlineEvent,
				},
			},
			startTime:      fourHoursAgo,
			endTime:        testNow,
			expectedUptime: time.Hour * 4,
		},
		{
			name:     "Offline event and closed",
			openedAt: fourHoursAgo,
			closedAt: threeHoursAgo,
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOfflineEvent,
				},
			},
			startTime: fourHoursAgo,
			endTime:   testNow,
		},
		{
			name:     "Online event before close",
			openedAt: fourHoursAgo,
			closedAt: oneHourAgo,
			events: []*channelEvent{
				{
					timestamp: twoHoursAgo,
					eventType: peerOnlineEvent,
				},
			},
			startTime:      fourHoursAgo,
			endTime:        testNow,
			expectedUptime: time.Hour,
		},
		{
			name:     "Online then offline event",
			openedAt: fourHoursAgo,
			closedAt: oneHourAgo,
			events: []*channelEvent{
				{
					timestamp: threeHoursAgo,
					eventType: peerOnlineEvent,
				},
				{
					timestamp: twoHoursAgo,
					eventType: peerOfflineEvent,
				},
			},
			startTime:      fourHoursAgo,
			endTime:        testNow,
			expectedUptime: time.Hour,
		},
		{
			name:     "Online event before uptime period",
			openedAt: fourHoursAgo,
			closedAt: oneHourAgo,
			events: []*channelEvent{
				{
					timestamp: threeHoursAgo,
					eventType: peerOnlineEvent,
				},
			},
			startTime:      twoHoursAgo,
			endTime:        testNow,
			expectedUptime: time.Hour,
		},
		{
			name:     "Offline event after uptime period",
			openedAt: fourHoursAgo,
			events: []*channelEvent{
				{
					timestamp: fourHoursAgo,
					eventType: peerOnlineEvent,
				},
				{
					timestamp: testNow.Add(time.Hour),
					eventType: peerOfflineEvent,
				},
			},
			startTime:      twoHoursAgo,
			endTime:        testNow,
			expectedUptime: time.Hour * 2,
		},
		{
			name:     "All events within period",
			openedAt: fourHoursAgo,
			events: []*channelEvent{
				{
					timestamp: twoHoursAgo,
					eventType: peerOnlineEvent,
				},
			},
			startTime:      threeHoursAgo,
			endTime:        oneHourAgo,
			expectedUptime: time.Hour,
		},
		{
			name:     "Multiple online and offline",
			openedAt: testNow.Add(time.Hour * -8),
			events: []*channelEvent{
				{
					timestamp: testNow.Add(time.Hour * -7),
					eventType: peerOnlineEvent,
				},
				{
					timestamp: testNow.Add(time.Hour * -6),
					eventType: peerOfflineEvent,
				},
				{
					timestamp: testNow.Add(time.Hour * -5),
					eventType: peerOnlineEvent,
				},
				{
					timestamp: testNow.Add(time.Hour * -4),
					eventType: peerOfflineEvent,
				},
				{
					timestamp: testNow.Add(time.Hour * -3),
					eventType: peerOnlineEvent,
				},
			},
			startTime:      testNow.Add(time.Hour * -8),
			endTime:        oneHourAgo,
			expectedUptime: time.Hour * 4,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			score := &chanEventLog{
				events:   test.events,
				now:      testClock.Now,
				openedAt: test.openedAt,
				closedAt: test.closedAt,
			}

			uptime, err := score.uptime(
				test.startTime, test.endTime,
			)
			require.Equal(t, test.expectErr, err != nil)
			require.Equal(t, test.expectedUptime, uptime)
		})
	}
}
