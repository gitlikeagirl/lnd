package contractcourt

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/chainntnfs"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/channeldb/kvdb"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwallet"
)

const (
	outgoingContestHtlcExpiry = 110
)

// TestHtlcOutgoingResolverTimeout tests resolution of an offered htlc that
// timed out.
func TestHtlcOutgoingResolverTimeout(t *testing.T) {
	t.Run("single stage, no report", func(t *testing.T) {
		testOutgoingResolverTimeout(
			t, newOutgoingResolverTestContext(t), nil,
		)
	})

	t.Run("two stage, htlc timeout report", func(t *testing.T) {
		// We create a test context with a non-nil timeout tx to test
		// two stage claim. We add items in the timeout tx witness
		// because we use it during resolution.
		var timeoutTxValue int64 = 10000
		timeoutTx := &wire.MsgTx{
			TxIn: []*wire.TxIn{
				{
					PreviousOutPoint: testChanPoint1,
					Witness:          [][]byte{{}},
				},
			},
			TxOut: []*wire.TxOut{{
				Value: timeoutTxValue,
			}},
		}
		timeoutTxHash := timeoutTx.TxHash()

		res := lnwallet.OutgoingHtlcResolution{
			SignedTimeoutTx: timeoutTx,
			SweepSignDesc:   testSignDesc,
			Expiry:          outgoingContestHtlcExpiry,
		}

		ctxWithTimeoutTx := newOutgoingResolverTestContext(
			t, outgoingResolutionOption(res),
		)

		expectedReport := &channeldb.ResolverReport{
			OutPoint:        testChanPoint1,
			Amount:          btcutil.Amount(timeoutTxValue),
			ResolverOutcome: channeldb.ResolverTypeOutgoingHtlcTimeoutTx,
			SpendTxID:       &timeoutTxHash,
		}

		testOutgoingResolverTimeout(
			t, ctxWithTimeoutTx, []*channeldb.ResolverReport{
				expectedReport,
			},
		)
	})
}

// testOutgoingResolverTimeout tests timeout of outgoing htlcs. It takes a
// list of the resolver reports it expects to be generated.
func testOutgoingResolverTimeout(t *testing.T, ctx *outgoingResolverTestContext,
	expectedReports []*channeldb.ResolverReport) {

	t.Parallel()
	defer timeout(t)()

	// Start the resolution process in a goroutine.
	ctx.resolve()

	// Notify arrival of the block after which the timeout path of the htlc
	// unlocks.
	ctx.notifyEpoch(outgoingContestHtlcExpiry - 1)

	// Assert that the resolver finishes without error and transforms in a
	// timeout resolver.
	ctx.waitForResult(true)

	// Check that we have the number of reports we required.
	expectedReportsCount := len(expectedReports)
	if expectedReportsCount != len(ctx.reports) {
		t.Fatalf("expected: %v reports, got: %v", expectedReportsCount,
			len(ctx.reports))
	}

	for i, report := range ctx.reports {
		if !reflect.DeepEqual(report, expectedReports[i]) {
			t.Fatalf("expected: %v, got: %v", expectedReports[i],
				report)
		}
	}
}

// TestHtlcOutgoingResolverRemoteClaim tests resolution of an offered htlc that
// is claimed by the remote party.
func TestHtlcOutgoingResolverRemoteClaim(t *testing.T) {
	t.Parallel()
	defer timeout(t)()

	// Setup the resolver with our test resolution and start the resolution
	// process.
	ctx := newOutgoingResolverTestContext(t)
	ctx.resolve()

	// The remote party sweeps the htlc. Notify our resolver of this event.
	preimage := lntypes.Preimage{}
	spendTx := &wire.MsgTx{
		TxIn: []*wire.TxIn{
			{
				Witness: [][]byte{
					{0}, {1}, {2}, preimage[:],
				},
			},
		},
	}
	spendHash := spendTx.TxHash()

	ctx.notifier.spendChan <- &chainntnfs.SpendDetail{
		SpendingTx:    spendTx,
		SpenderTxHash: &spendHash,
	}

	// We expect the extracted preimage to be added to the witness beacon.
	<-ctx.preimageDB.newPreimages

	// We also expect a resolution message to the incoming side of the
	// circuit.
	<-ctx.resolutionChan

	// Assert that the resolver finishes without error.
	ctx.waitForResult(false)

	// Finally, check that we have a report as expected.
	expectedReport := &channeldb.ResolverReport{
		OutPoint:        wire.OutPoint{},
		Amount:          0,
		ResolverOutcome: channeldb.ResolverOutcomeOutgoingHtlcClaim,
		SpendTxID:       &spendHash,
	}

	assertResolverReport(t, ctx.reports, expectedReport)
}

type resolveResult struct {
	err          error
	nextResolver ContractResolver
}

type outgoingResolverTestContext struct {
	resolver           *htlcOutgoingContestResolver
	notifier           *mockNotifier
	preimageDB         *mockWitnessBeacon
	resolverResultChan chan resolveResult
	resolutionChan     chan ResolutionMsg
	reports            []*channeldb.ResolverReport
	t                  *testing.T
}

// outgoingResolverOption is a functional option which can be applied
// to an outgoing resolver test context to customize default test context.
type outgoingResolverOption func(ctx *outgoingResolverTestContext)

// resolutionOption sets the htlcResolution on an incoming resolver test
// context.
func outgoingResolutionOption(res lnwallet.OutgoingHtlcResolution) outgoingResolverOption {
	return func(ctx *outgoingResolverTestContext) {
		ctx.resolver.htlcResolution = res
	}
}

func newOutgoingResolverTestContext(t *testing.T,
	opts ...outgoingResolverOption) *outgoingResolverTestContext {

	notifier := &mockNotifier{
		epochChan: make(chan *chainntnfs.BlockEpoch),
		spendChan: make(chan *chainntnfs.SpendDetail),
		confChan:  make(chan *chainntnfs.TxConfirmation),
	}

	checkPointChan := make(chan struct{}, 1)
	resolutionChan := make(chan ResolutionMsg, 1)

	preimageDB := newMockWitnessBeacon()

	onionProcessor := &mockOnionProcessor{}

	testCtx := &outgoingResolverTestContext{
		notifier:       notifier,
		preimageDB:     preimageDB,
		resolutionChan: resolutionChan,
		t:              t,
	}

	chainCfg := ChannelArbitratorConfig{
		ChainArbitratorConfig: ChainArbitratorConfig{
			Notifier:   notifier,
			PreimageDB: preimageDB,
			DeliverResolutionMsg: func(msgs ...ResolutionMsg) error {
				if len(msgs) != 1 {
					return fmt.Errorf("expected 1 "+
						"resolution msg, instead got %v",
						len(msgs))
				}

				resolutionChan <- msgs[0]
				return nil
			},
			OnionProcessor: onionProcessor,
		},
		PutResolverReport: func(_ kvdb.RwTx,
			report *channeldb.ResolverReport) error {

			testCtx.reports = append(testCtx.reports, report)

			return nil
		},
	}

	outgoingRes := lnwallet.OutgoingHtlcResolution{
		Expiry: outgoingContestHtlcExpiry,
		SweepSignDesc: input.SignDescriptor{
			Output: &wire.TxOut{},
		},
	}

	cfg := ResolverConfig{
		ChannelArbitratorConfig: chainCfg,
		Checkpoint: func(_ ContractResolver,
			closure func(_ kvdb.RwTx) error) error {

			// If our closure is non-nil, run it with a nil tx.
			if closure != nil {
				if err := closure(nil); err != nil {
					return err
				}
			}

			checkPointChan <- struct{}{}
			return nil
		},
	}

	testCtx.resolver = &htlcOutgoingContestResolver{
		htlcTimeoutResolver: htlcTimeoutResolver{
			contractResolverKit: *newContractResolverKit(cfg),
			htlcResolution:      outgoingRes,
			htlc: channeldb.HTLC{
				RHash:     testResHash,
				OnionBlob: testOnionBlob,
			},
		},
	}

	// Apply all functional options to test context.
	for _, opt := range opts {
		opt(testCtx)
	}

	return testCtx
}

func (i *outgoingResolverTestContext) resolve() {
	// Start resolver.
	i.resolverResultChan = make(chan resolveResult, 1)
	go func() {
		nextResolver, err := i.resolver.Resolve()
		i.resolverResultChan <- resolveResult{
			nextResolver: nextResolver,
			err:          err,
		}
	}()

	// Notify initial block height.
	i.notifyEpoch(testInitialBlockHeight)
}

func (i *outgoingResolverTestContext) notifyEpoch(height int32) {
	i.notifier.epochChan <- &chainntnfs.BlockEpoch{
		Height: height,
	}
}

func (i *outgoingResolverTestContext) waitForResult(expectTimeoutRes bool) {
	i.t.Helper()

	result := <-i.resolverResultChan
	if result.err != nil {
		i.t.Fatal(result.err)
	}

	if !expectTimeoutRes {
		if result.nextResolver != nil {
			i.t.Fatal("expected no next resolver")
		}
		return
	}

	_, ok := result.nextResolver.(*htlcTimeoutResolver)
	if !ok {
		i.t.Fatal("expected htlcTimeoutResolver")
	}
}
