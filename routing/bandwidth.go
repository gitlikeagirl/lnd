package routing

import (
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/kvdb"
	"github.com/lightningnetwork/lnd/lnwire"
)

// bandwidthHints is provides hints about the currently available balance in
// our channels.
type bandwidthHints interface {
	// availableBandwidth returns the total available bandwidth for a
	// channel and a bool indicating whether the channel hint was found.
	// If the channel is unavailable, a zero amount is returned. It takes
	// an optional htlc amount parameter which can be used to validate the
	// amount against the channel's flow constraints.
	availableBandwidth(channelID uint64,
		htlcAmount *lnwire.MilliSatoshi) (lnwire.MilliSatoshi, bool)
}

// linkQuery is the function signature used to query the link for the currently
// available bandwidth for an edge.
type linkQuery func(*channeldb.ChannelEdgeInfo,
	*lnwire.MilliSatoshi) lnwire.MilliSatoshi

// bandwidthManager is an implementation of the bandwidthHints interface which
// queries the link for our latest local channel balances.
type bandwidthManager struct {
	linkQuery  linkQuery
	localChans map[uint64]*channeldb.ChannelEdgeInfo
}

// newBandwidthManager creates a bandwidth manager for the source node provided
// which is used to obtain hints from the lower layer w.r.t the available
// bandwidth of edges on the network. Currently, we'll only obtain bandwidth
// hints for the edges we directly have open ourselves. Obtaining these hints
// allows us to reduce the number of extraneous attempts as we can skip channels
// that are inactive, or just don't have enough bandwidth to carry the payment.
func newBandwidthManager(sourceNode *channeldb.LightningNode,
	linkQuery linkQuery) (*bandwidthManager, error) {

	manager := &bandwidthManager{
		linkQuery:  linkQuery,
		localChans: make(map[uint64]*channeldb.ChannelEdgeInfo),
	}

	// First, we'll collect the set of outbound edges from the target
	// source node and add them to our bandwidth manager's map of channels.
	err := sourceNode.ForEachChannel(nil, func(tx kvdb.RTx,
		edgeInfo *channeldb.ChannelEdgeInfo,
		_, _ *channeldb.ChannelEdgePolicy) error {

		manager.localChans[edgeInfo.ChannelID] = edgeInfo

		return nil
	})
	if err != nil {
		return nil, err
	}

	return manager, nil
}

func (b *bandwidthManager) availableBandwidth(channelID uint64,
	amount *lnwire.MilliSatoshi) (lnwire.MilliSatoshi, bool) {

	channel, ok := b.localChans[channelID]
	if !ok {
		return 0, false
	}

	return b.linkQuery(channel, amount), true
}
