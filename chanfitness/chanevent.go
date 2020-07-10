package chanfitness

import (
	"fmt"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/routing/route"
)

type eventType int

const (
	peerOnlineEvent eventType = iota
	peerOfflineEvent
)

// String provides string representations of channel events.
func (e eventType) String() string {
	switch e {
	case peerOnlineEvent:
		return "peer_online"

	case peerOfflineEvent:
		return "peer_offline"
	}

	return "unknown"
}

// channelEvent is a a timestamped event which is observed on a per channel
// basis.
type channelEvent struct {
	timestamp time.Time
	eventType eventType
}

// chanEventLog stores all events that have occurred over a channel's lifetime.
type chanEventLog struct {
	// channelPoint is the outpoint for the channel's funding transaction.
	channelPoint wire.OutPoint

	// peer is the compressed public key of the peer being monitored.
	peer route.Vertex

	// events is a log of timestamped events observed for the channel.
	// This list holds a set of events that we have committed to allocating
	// resources to.
	events []*channelEvent

	// stagedEvent represents an event that is pending addition to the
	// events list. It has not yet been added because we rate limit the
	// frequency that we store events at. We need to store this value
	// in the log (rather than just ignore events) so that we can flush the
	// aggregate outcome to our event log once the rate limiting period has
	// ended.
	//
	// Take the following example:
	// - Peer online event recorded
	// - Peer offline event, not recorded due to rate limit
	// - No more events, we incorrectly believe our peer to be offline
	// Instead of skipping events, we stage the most recent event during the
	// rate limited period so that we know what happened (on aggregate)
	// while we were rate limiting events.
	//
	// Note that we currently only store offline/online events so we can
	// use this field to track our online state. With the addition of other
	// event types, we need to only stage online/offline events, or split
	// them out.
	stagedEvent *channelEvent

	// flapCount counts the number of offline events we have processed
	// for our peer. This gives us an idea of the reliability of
	// our peers, and provides us with the ability to rate limit them.
	flapCount uint32

	// now is expected to return the current time. It is supplied as an
	// external function to enable deterministic unit tests.
	now func() time.Time

	// openedAt tracks the first time this channel was seen. This is not
	// necessarily the time that it confirmed on chain because channel
	// events are not persisted at present.
	openedAt time.Time

	// closedAt is the time that the channel was closed. If the channel has
	// not been closed yet, it is zero.
	closedAt time.Time
}

// newEventLog creates an event log for a channel with the openedAt time set.
func newEventLog(channelPoint wire.OutPoint, peer route.Vertex,
	now func() time.Time, flapCount uint32) *chanEventLog {

	eventlog := &chanEventLog{
		channelPoint: channelPoint,
		peer:         peer,
		flapCount:    flapCount,
		now:          now,
		openedAt:     now(),
	}

	return eventlog
}

// close sets the closing time for an event log.
func (e *chanEventLog) close() {
	e.closedAt = e.now()
}

// listEvents returns all of the events that our event log has tracked,
// including events that are staged for addition to our set of events but have
// not yet been committed to (because we rate limit and store only the aggregate
// outcome over a period).
func (e *chanEventLog) listEvents() []*channelEvent {
	if e.stagedEvent == nil {
		return e.events
	}

	return append(e.events, e.stagedEvent)
}

// add appends an event with the given type and current time to the event log.
// The open time for the eventLog will be set to the event's timestamp if it is
// not set yet. This add function rate limits our peer events based on our
// peer's flap rate.
func (e *chanEventLog) add(eventType eventType) {
	// If the channel is already closed, return early without adding an
	// event.
	if !e.closedAt.IsZero() {
		return
	}

	// Once we know our channel is open, we increment our flap count. Note
	// that we do this for every event because we are currently *only*
	// tracking peer online/offline events. If we expand our event types to
	// include other events, we should only track flaps for online/offline
	// events.
	if eventType == peerOfflineEvent {
		e.flapCount++
	}

	event := &channelEvent{
		timestamp: e.now(),
		eventType: eventType,
	}

	// If we have no staged events, we can just stage this event and return.
	if e.stagedEvent == nil {
		e.stagedEvent = event
		return
	}

	// We get the amount of time we require between events according to
	// peer flap count.
	aggregation := getRateLimit(e.flapCount)
	nextRecordTime := e.stagedEvent.timestamp.Add(aggregation)
	flushEvent := nextRecordTime.Before(event.timestamp)

	// If enough time has passed since our last staged event, we add our
	// event to our in-memory list.
	if flushEvent {
		e.events = append(e.events, e.stagedEvent)

		log.Debugf("Channel %v recording event: %v",
			e.channelPoint, eventType)
	}

	// Finally, we replace our staged event with the new event we received.
	e.stagedEvent = event
}

// onlinePeriod represents a period of time over which a peer was online.
type onlinePeriod struct {
	start, end time.Time
}

// getOnlinePeriods returns a list of all the periods that the event log has
// recorded the remote peer as being online. In the unexpected case where there
// are no events, the function returns early. Online periods are defined as a
// peer online event which is terminated by a peer offline event. If the event
// log ends on a peer online event, it appends a final period which is
// calculated until the present. This function expects the event log provided
// to be ordered by ascending timestamp, and can tolerate multiple consecutive
// online or offline events.
func (e *chanEventLog) getOnlinePeriods() []*onlinePeriod {
	// Return early if there are no events, there are no online periods.
	if len(e.events) == 0 {
		return nil
	}

	var (
		// previousEvent tracks the last event that we had that was of
		// a different type to our own. It is used to determine the
		// start time of our online periods when we experience an
		// offline event, and to track our last recorded state.
		previousEvent *channelEvent
		onlinePeriods []*onlinePeriod
	)

	// Loop through all events to build a list of periods that the peer was
	// online. Online periods are added when they are terminated with a peer
	// offline event. If the log ends on an online event, the period between
	// the online event and the present is not tracked. The type of the most
	// recent event is tracked using the offline bool so that we can add a
	// final online period if necessary.
	for _, event := range e.listEvents() {
		switch event.eventType {
		case peerOnlineEvent:
			// If our previous event is nil, we just set it and
			// break out of the switch.
			if previousEvent == nil {
				previousEvent = event
				break
			}

			// If our previous event was an offline event, we update
			// it to this event. We do not do this if it was an
			// online event because duplicate online events would
			// progress our online timestamp forward (rather than
			// keep it at our earliest online event timestamp).
			if previousEvent.eventType == peerOfflineEvent {
				previousEvent = event
			}

		case peerOfflineEvent:
			// If our previous event is nil, we just set it and
			// break out of the switch since we cannot record an
			// online period from this single event.
			if previousEvent == nil {
				previousEvent = event
				break
			}

			// If the last event we saw was an online event, we
			// add an online period to our set and progress our
			// previous event to this offline event. We do not
			// do this if we have had duplicate offline events
			// because we would be tracking the most recent offline
			// event rather than the only one
			if previousEvent.eventType == peerOnlineEvent {
				onlinePeriods = append(
					onlinePeriods, &onlinePeriod{
						start: previousEvent.timestamp,
						end:   event.timestamp,
					},
				)

				previousEvent = event
			}
		}
	}

	// If the last event was an peer offline event, we do not need to
	// calculate a final online period and can return online periods as is.
	if previousEvent.eventType == peerOfflineEvent {
		return onlinePeriods
	}

	// The log ended on an online event, so we need to add a final online
	// event. If the channel is closed, this period is until channel
	// closure. It it is still open, we calculate it until the present.
	finalEvent := &onlinePeriod{
		start: previousEvent.timestamp,
		end:   e.closedAt,
	}
	if finalEvent.end.IsZero() {
		finalEvent.end = e.now()
	}

	// Add the final online period to the set and return.
	return append(onlinePeriods, finalEvent)
}

// uptime calculates the total uptime we have recorded for a channel over the
// inclusive range specified. An error is returned if the end of the range is
// before the start or a zero end time is returned.
func (e *chanEventLog) uptime(start, end time.Time) (time.Duration, error) {
	// Error if we are provided with an invalid range to calculate uptime
	// for.
	if end.Before(start) {
		return 0, fmt.Errorf("end time: %v before start time: %v",
			end, start)
	}
	if end.IsZero() {
		return 0, fmt.Errorf("zero end time")
	}

	var uptime time.Duration

	for _, p := range e.getOnlinePeriods() {
		// The online period ends before the range we're looking at, so
		// we can skip over it.
		if p.end.Before(start) {
			continue
		}
		// The online period starts after the range we're looking at, so
		// can stop calculating uptime.
		if p.start.After(end) {
			break
		}

		// If the online period starts before our range, shift the start
		// time up so that we only calculate uptime from the start of
		// our range.
		if p.start.Before(start) {
			p.start = start
		}

		// If the online period ends before our range, shift the end
		// time forward so that we only calculate uptime until the end
		// of the range.
		if p.end.After(end) {
			p.end = end
		}

		uptime += p.end.Sub(p.start)
	}

	return uptime, nil
}
