package itest

import (
	"context"
	"fmt"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntest/wait"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
)

// testHoldInvoiceForceClose demonstrates that recipients of hold invoices
// will not release active htlcs for their own invoices when they expire,
// resulting in a force close of their channel.
func testHoldInvoiceForceClose(net *lntest.NetworkHarness, t *harnessTest) {
	ctxb, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Open a channel between alice and bob.
	chanReq := lntest.OpenChannelParams{
		Amt: 300000,
	}

	ctxt, _ := context.WithTimeout(ctxb, channelOpenTimeout)
	chanPoint := openChannelAndAssert(ctxt, t, net, net.Alice, net.Bob, chanReq)

	// Create a non-dust hold invoice for bob.
	var (
		preimage = lntypes.Preimage{1, 2, 3}
		payHash  = preimage.Hash()
	)
	invoiceReq := &invoicesrpc.AddHoldInvoiceRequest{
		Value:      30000,
		CltvExpiry: 40,
		Hash:       payHash[:],
	}

	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	bobInvoice, err := net.Bob.AddHoldInvoice(ctxt, invoiceReq)
	require.NoError(t.t, err)

	// Pay this invoice from Alice -> Bob, we should achieve this with a
	// single htlc.
	_, err = net.Alice.RouterClient.SendPaymentV2(
		ctxb, &routerrpc.SendPaymentRequest{
			PaymentRequest: bobInvoice.PaymentRequest,
			TimeoutSeconds: 60,
			FeeLimitMsat:   noFeeLimitMsat,
		},
	)
	require.NoError(t.t, err)

	waitForInvoiceAccepted(t, net.Bob, payHash)

	// Once the HTLC has cleared, alice and bob should both have a single
	// htlc locked in.
	nodes := []*lntest.HarnessNode{net.Alice, net.Bob}
	err = wait.NoError(func() error {
		return assertActiveHtlcs(nodes, payHash[:])
	}, defaultTimeout)
	require.NoError(t.t, err)

	// Get our htlc expiry height and current block height so that we
	// can mine the exact number of blocks required to expire the htlc.
	chans, err := net.Alice.ListChannels(ctxb, &lnrpc.ListChannelsRequest{})
	require.NoError(t.t, err)
	require.Len(t.t, chans.Channels, 1)
	require.Len(t.t, chans.Channels[0].PendingHtlcs, 1)
	activeHtlc := chans.Channels[0].PendingHtlcs[0]

	info, err := net.Alice.GetInfo(ctxb, &lnrpc.GetInfoRequest{})
	require.NoError(t.t, err)

	// Now we will mine blocks until the htlc expires, and wait for each
	// node to sync to our latest height.
	blocksTillExpiry := activeHtlc.ExpirationHeight - info.BlockHeight
	require.Greater(t.t, blocksTillExpiry, uint32(0))

	mineBlocks(t, net, blocksTillExpiry, 0)
	require.NoError(t.t, err)

	require.NoError(t.t, net.Alice.WaitForBlockchainSync(ctxb))
	require.NoError(t.t, net.Bob.WaitForBlockchainSync(ctxb))

	// We should have our force close tx in the mempool.
	mineBlocks(t, net, 1, 1)

	// At this point, Bob should have a pending force close channel as
	// Alice has gone directly to chain.
	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	err = waitForNumChannelPendingForceClose(ctxt, net.Bob, 1,
		func(channel *lnrpcForceCloseChannel) error {
			numHtlcs := len(channel.PendingHtlcs)
			if numHtlcs != 1 {
				return fmt.Errorf("expected 1 htlc, got: "+
					"%v", numHtlcs)
			}

			return nil
		},
	)
	require.NoError(t.t, err)

	// Cleanup Alice's force close.
	cleanupForceClose(t, net, net.Alice, chanPoint)
}
