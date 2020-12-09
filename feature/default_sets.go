package feature

import "github.com/lightningnetwork/lnd/lnwire"

// setDesc describes which feature bits should be advertised in which feature
// sets.
type setDesc map[lnwire.FeatureBit]map[Set]struct{}

// defaultSetDesc are the default set descriptors for generating feature
// vectors. Each set is annotated with the corresponding identifier from BOLT 9
// indicating where it should be advertised.
var defaultSetDesc = setDesc{
	lnwire.DataLossProtectRequired: {
		SetInit:    {}, // I
		SetNodeAnn: {}, // N
	},
	lnwire.GossipQueriesOptional: {
		SetInit:    {}, // I
		SetNodeAnn: {}, // N
	},
	lnwire.TLVOnionPayloadOptional: {
		SetInit:         {}, // I
		SetNodeAnn:      {}, // N
		SetInvoice:      {}, // 9
		SetLegacyGlobal: {},
	},
	lnwire.StaticRemoteKeyRequired: {
		SetInit:         {}, // I
		SetNodeAnn:      {}, // N
		SetLegacyGlobal: {},
	},
	lnwire.UpfrontShutdownScriptOptional: {
		SetInit:    {}, // I
		SetNodeAnn: {}, // N
	},
	// We're using lnd 0.7 for our test, which doesn't know about this
	// feature bit either. Downgrade to optional in this commit (which will
	// be removed before merge) for simple testing.
	lnwire.PaymentAddrOptional: {
		SetInit:    {}, // I
		SetNodeAnn: {}, // N
		SetInvoice: {}, // 9
	},
	lnwire.MPPOptional: {
		SetInit:    {}, // I
		SetNodeAnn: {}, // N
		SetInvoice: {}, // 9
	},
	lnwire.AnchorsOptional: {
		SetInit:    {}, // I
		SetNodeAnn: {}, // N
	},
	lnwire.WumboChannelsOptional: {
		SetInit:    {}, // I
		SetNodeAnn: {}, // N
	},
}
