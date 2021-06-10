package itest

import (
	"context"
	"testing"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
)

// testMaxHtlcPathfind tests the case where we try to send a payment over a
// channel where we have already reached the limit of the number of htlcs that
// we may add to the remote party's commitment. This test asserts that we do
// not attempt to use the full channel at all in our pathfinding.
func testMaxHtlcPathfind(net *lntest.NetworkHarness, t *harnessTest) {
	ctxb := context.Background()

	// Setup a channel between Alice and Bob where Alice will only allow
	// Bob to add a maximum of 5 htlcs to here commitment.
	maxHtlcs := 5

	ctxt, _ := context.WithTimeout(ctxb, channelOpenTimeout)
	chanPoint := openChannelAndAssert(
		ctxt, t, net, net.Alice, net.Bob,
		lntest.OpenChannelParams{
			Amt:            1000000,
			PushAmt:        800000,
			RemoteMaxHtlcs: uint16(maxHtlcs),
		},
	)

	// Wait for Alice and Bob to receive the channel edge from the
	// funding manager.
	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	err := net.Alice.WaitForNetworkChannelOpen(ctxt, chanPoint)
	require.NoError(t.t, err, "alice does not have open channel")

	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	err = net.Bob.WaitForNetworkChannelOpen(ctxt, chanPoint)
	require.NoError(t.t, err, "bob does not have open channel")

	// Alice and bob should have one channel open with each other now.
	assertNodeNumChannels(t, net.Alice, 1)
	assertNodeNumChannels(t, net.Bob, 1)

	// Send our maximum number of htlcs from Bob -> Alice so that we get
	// to a point where Alice won't accept any more htlcs on the channel.
	for i := 0; i < maxHtlcs; i++ {
		acceptHoldInvoice(t.t, i, net.Bob, net.Alice)
	}

	err = assertNumActiveHtlcs([]*lntest.HarnessNode{
		net.Alice, net.Bob,
	}, maxHtlcs)
	require.NoError(t.t, err, "htlcs not active")

	// Now we send a payment from Alice -> Bob to sanity check that our
	// commitment limit is not applied in the opposite direction.
	acceptHoldInvoice(t.t, maxHtlcs, net.Alice, net.Bob)
	err = assertNumActiveHtlcs([]*lntest.HarnessNode{
		net.Alice, net.Bob,
	}, maxHtlcs+1)
	require.NoError(t.t, err, "htlcs not active")

	// Now, e're going to try to send another payment from Bob -> Alice.
	// We've hit our max remote htlcs, so we expect this payment to spin
	// out dramatically with pathfinding.
	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	payment, err := net.Bob.RouterClient.SendPaymentV2(
		ctxt, &routerrpc.SendPaymentRequest{
			Amt:            1000,
			Dest:           net.Alice.PubKey[:],
			TimeoutSeconds: 60,
			FeeLimitSat:    1000000,
			MaxParts:       10,
			Amp:            true,
		},
	)
	require.NoError(t.t, err, "send payment failed")

	update, err := payment.Recv()
	require.NoError(t.t, err, "no payment in flight update")
	require.Equal(t.t, lnrpc.Payment_IN_FLIGHT, update.Status,
		"payment not inflight")

	update, err = payment.Recv()
	require.NoError(t.t, err, "no payment failed update")
	require.Equal(t.t, lnrpc.Payment_FAILED, update.Status)
	require.Len(t.t, update.Htlcs, 0, "expected no htlcs dispatched")

	ctxt, _ = context.WithTimeout(ctxb, channelCloseTimeout)
	closeChannelAndAssert(ctxt, t, net, net.Alice, chanPoint, true)
	cleanupForceClose(t, net, net.Alice, chanPoint)
}

// acceptHoldInvoice adds a hold invoice the the recipient node, pays it from
// the sender and asserts that we have reached the accepted state where htlcs
// are locked in for the payment.
func acceptHoldInvoice(t *testing.T, idx int, sender,
	receiver *lntest.HarnessNode) {

	ctxb := context.Background()
	ctxt, cancel := context.WithTimeout(ctxb, defaultTimeout)
	defer cancel()

	hash := [lntypes.HashSize]byte{byte(idx + 1)}

	invoice, err := receiver.AddHoldInvoice(
		ctxt, &invoicesrpc.AddHoldInvoiceRequest{
			ValueMsat: 10000,
			Hash:      hash[:],
		},
	)
	require.NoError(t, err, "couldn't add invoice")

	invStream, err := receiver.InvoicesClient.SubscribeSingleInvoice(
		ctxb, &invoicesrpc.SubscribeSingleInvoiceRequest{
			RHash: hash[:],
		},
	)
	require.NoError(t, err, "could not subscribe to invoice")

	inv, err := invStream.Recv()
	require.NoError(t, err, "invoice open stream failed")
	require.Equal(t, lnrpc.Invoice_OPEN, inv.State,
		"expected open")

	payStream, err := sender.RouterClient.SendPaymentV2(
		ctxb, &routerrpc.SendPaymentRequest{
			PaymentRequest: invoice.PaymentRequest,
			TimeoutSeconds: 60,
			FeeLimitSat:    1000000,
		},
	)
	require.NoError(t, err, "send payment failed")

	// Finally, assert that we progress to an accepted state.
	payment, err := payStream.Recv()
	require.NoError(t, err, "payment in flight stream failed")
	require.Equal(t, lnrpc.Payment_IN_FLIGHT, payment.Status)

	inv, err = invStream.Recv()
	require.NoError(t, err, "invoice accepted stream failed")
	require.Equal(t, lnrpc.Invoice_ACCEPTED, inv.State,
		"expected accepted invoice")
}
