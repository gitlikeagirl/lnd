package itest

import (
	"context"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
)

// testMaxHtlcPathfind
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
	subCtx, cancel := context.WithTimeout(ctxb, defaultTimeout)
	defer cancel()

	payStreams := make([]routerrpc.Router_SendPaymentV2Client, maxHtlcs)
	invStreams := make(
		[]invoicesrpc.Invoices_SubscribeSingleInvoiceClient, maxHtlcs,
	)
	preimages := make([]lntypes.Preimage, maxHtlcs)

	for i := 0; i < maxHtlcs; i++ {
		ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)

		preimages[i] = [lntypes.PreimageSize]byte{byte(i + 1)}
		hash := preimages[i].Hash()

		invoice, err := net.Alice.AddHoldInvoice(
			ctxt, &invoicesrpc.AddHoldInvoiceRequest{
				ValueMsat: 10000,
				Hash:      hash[:],
			},
		)
		require.NoError(t.t, err, "couldn't add invoice")

		invStreams[i], err = net.Alice.InvoicesClient.SubscribeSingleInvoice(
			subCtx, &invoicesrpc.SubscribeSingleInvoiceRequest{
				RHash: hash[:],
			},
		)
		require.NoError(t.t, err, "could not subscribe to invoice")

		inv, err := invStreams[i].Recv()
		require.NoError(t.t, err, "invoice open stream failed")
		require.Equal(t.t, lnrpc.Invoice_OPEN, inv.State,
			"expected open")

		payStreams[i], err = net.Bob.RouterClient.SendPaymentV2(
			subCtx, &routerrpc.SendPaymentRequest{
				PaymentRequest: invoice.PaymentRequest,
				TimeoutSeconds: 60,
				FeeLimitSat:    1000000,
			},
		)
		require.NoError(t.t, err, "send payment failed")
	}

	// Assert that our invoices are updated to an accepted state, and all
	// payments are in flight.
	for _, stream := range payStreams {
		payment, err := stream.Recv()
		require.NoError(t.t, err, "payment in flight stream failed")

		require.Equal(t.t, lnrpc.Payment_IN_FLIGHT, payment.Status)
	}

	for _, stream := range invStreams {
		inv, err := stream.Recv()
		require.NoError(t.t, err, "invoice accepted stream failed")
		require.Equal(t.t, lnrpc.Invoice_ACCEPTED, inv.State,
			"expected accepted invoice")
	}

	err = assertNumActiveHtlcs([]*lntest.HarnessNode{}, maxHtlcs)
	require.NoError(t.t, err, "htlcs not active")

	// Now, we're going to try to send another payment from Bob -> Alice.
	payment, err := net.Bob.RouterClient.SendPaymentV2(
		subCtx, &routerrpc.SendPaymentRequest{
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
