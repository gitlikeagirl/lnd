package chanacceptor

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/lnwire"
)

// ChainedAcceptor represents a conjunction of ChannelAcceptor results.
type ChainedAcceptor struct {
	// acceptors is a map of ChannelAcceptors that will be evaluated when
	// the ChainedAcceptor's Accept method is called.
	acceptors    map[uint64]ChannelAcceptor
	acceptorsMtx sync.RWMutex

	acceptorID uint64 // To be used atomically.
}

// NewChainedAcceptor initializes a ChainedAcceptor.
func NewChainedAcceptor() *ChainedAcceptor {
	return &ChainedAcceptor{
		acceptors: make(map[uint64]ChannelAcceptor),
	}
}

// AddAcceptor adds a ChannelAcceptor to this ChainedAcceptor.
func (c *ChainedAcceptor) AddAcceptor(acceptor ChannelAcceptor) uint64 {
	id := atomic.AddUint64(&c.acceptorID, 1)

	c.acceptorsMtx.Lock()
	c.acceptors[id] = acceptor
	c.acceptorsMtx.Unlock()

	// Return the id so that a caller can call RemoveAcceptor.
	return id
}

// RemoveAcceptor removes a ChannelAcceptor from this ChainedAcceptor given
// an ID.
func (c *ChainedAcceptor) RemoveAcceptor(id uint64) {
	c.acceptorsMtx.Lock()
	delete(c.acceptors, id)
	c.acceptorsMtx.Unlock()
}

// Accept evaluates the results of all ChannelAcceptors in the acceptors map
// and returns the conjunction of all these predicates.
//
// NOTE: Part of the ChannelAcceptor interface.
func (c *ChainedAcceptor) Accept(req *ChannelAcceptRequest) *ChannelAcceptResponse {
	c.acceptorsMtx.RLock()
	defer c.acceptorsMtx.RUnlock()

	var finalResp ChannelAcceptResponse

	for _, acceptor := range c.acceptors {
		// Call our acceptor to determine whether we want to accept this
		// channel.
		acceptorResponse := acceptor.Accept(req)

		// If we should reject the channel, we can just exit early. This
		// has the effect of returning the error belonging to our first
		// failed acceptor.
		if acceptorResponse.RejectChannel() {
			return acceptorResponse
		}

		// If we have accepted the channel, we need to set the other
		// fields that were set in the response. However, since we are
		// dealing with multiple responses, we need to make sure that we
		// have not received inconsistent values (eg a csv delay of 1
		// from one acceptor, and a delay of 120 from another). We
		// set each value on our final response if it has not been set
		// yet, and allow duplicate sets if the value is the same. If
		// we cannot set a field, we return an error response.
		var err error
		finalResp, err = mergeResponse(finalResp, *acceptorResponse)
		if err != nil {
			log.Errorf("response for: %x has inconsistent values: %v",
				req.OpenChanMsg.PendingChannelID, err)

			return NewChannelAcceptResponse(
				false, errChannelRejected.Error(), nil, 0, 0,
				0, 0, 0, 0,
			)
		}
	}

	// If we have gone through all of our acceptors with no objections, we
	// can return an acceptor with a nil error.
	return &finalResp
}

// mergeResponse takes two channel accept responses, and attempts to merge their
// fields, failing if any fields conflict (are non-zero and not equal). It
// returns a new response that has all the merged fields in it.
func mergeResponse(current, new ChannelAcceptResponse) (ChannelAcceptResponse,
	error) {

	val, err := mergeValue(
		"csv delay", current.CSVDelay == 0, new.CSVDelay == 0,
		current.CSVDelay, new.CSVDelay,
	)
	if err != nil {
		return current, err
	}
	current.CSVDelay = val.(uint16)

	val, err = mergeValue(
		"htlc limit", current.HtlcLimit == 0, new.HtlcLimit == 0,
		current.HtlcLimit, new.HtlcLimit,
	)
	if err != nil {
		return current, err
	}
	current.HtlcLimit = val.(uint16)

	val, err = mergeValue(
		"min htlc in", current.MinHtlcIn == 0,
		new.MinHtlcIn == 0, current.MinHtlcIn,
		new.MinHtlcIn,
	)
	if err != nil {
		return current, err
	}
	current.MinHtlcIn = val.(lnwire.MilliSatoshi)

	val, err = mergeValue(
		"min depth", current.MinAcceptDepth == 0,
		new.MinAcceptDepth == 0, current.MinAcceptDepth,
		new.MinAcceptDepth,
	)
	if err != nil {
		return current, err
	}
	current.MinAcceptDepth = val.(uint16)

	val, err = mergeValue(
		"in flight total", current.InFlightTotal == 0,
		new.InFlightTotal == 0, current.InFlightTotal,
		new.InFlightTotal,
	)
	if err != nil {
		return current, err
	}
	current.InFlightTotal = val.(lnwire.MilliSatoshi)

	val, err = mergeValue(
		"reserve", current.Reserve == 0, new.Reserve == 0,
		current.Reserve, new.Reserve,
	)
	if err != nil {
		return current, err
	}
	current.Reserve = val.(btcutil.Amount)

	val, err = mergeValue(
		"upfront shutdown", current.UpfrontShutdown == nil,
		new.UpfrontShutdown == nil, current.UpfrontShutdown,
		new.UpfrontShutdown,
	)
	if err != nil {
		return current, err
	}
	current.UpfrontShutdown = val.(lnwire.DeliveryAddress)

	return current, nil
}

// mergeValue checks that a current value for a field and the new proposed
// value do not contradict each other, and returns the new value that should
// be set.
func mergeValue(name string, currentEmpty bool, newEmpty bool,
	current, new interface{}) (interface{}, error) {

	switch {
	// If the current value is empty, then we can set any new value we want.
	case currentEmpty:
		return new, nil

	// If there is no new value proposed, then we can just stick with the
	// old value.
	case newEmpty:
		return current, nil

	// If both values are not empty, but are not equal, then we have an
	// inconsistent set of values.
	case current != new:
		return 0, fmt.Errorf("multiple values set for: %v, %v and %v",
			name, current, new)

	// Values are equal, all ok.
	default:
		return current, nil
	}
}

// A compile-time constraint to ensure ChainedAcceptor implements the
// ChannelAcceptor interface.
var _ ChannelAcceptor = (*ChainedAcceptor)(nil)
