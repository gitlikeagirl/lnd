package itest

import (
	"context"
	"fmt"
	"testing"

	"github.com/lightningnetwork/lnd/lntest"
	"github.com/stretchr/testify/require"
)

// testLocalCsvLimit tests rejection of channels that are above our maximum
// local csv delay, for locally and remote initiated channels.
func testLocalCsvLimit(net *lntest.NetworkHarness, ht *harnessTest) {
	ht.t.Run("csv delay too high", func(t *testing.T) {
		testLocalCsvDelayLimit(net, ht, 500, 600, true)
	})

	ht.t.Run("csv delay low enough", func(t *testing.T) {
		testLocalCsvDelayLimit(net, ht, 500, 400, false)
	})
}

// testLocalCsvDelayLimit creates a node Alice with a local csv limit and tests
// that incoming and outgoing channels adhere to this upper limit. The fail
// boolean passed in indicates whether we expect the channels to succeed or
// fail.
func testLocalCsvDelayLimit(net *lntest.NetworkHarness, t *harnessTest,
	localCsvLimit, remoteCsvDefault uint16, fail bool) {

	// First, we create a node Alice that will only accept channels if her
	// csv delay is <= localCsvLimit.
	args := []string{
		fmt.Sprintf("--bitcoin.maxlocaldelay=%v", localCsvLimit),
	}
	alice, err := net.NewNode("Alice", args)
	require.NoError(t.t, err)
	defer require.NoError(t.t, net.ShutdownNode(alice))

	// Create Bob, who sets remote csv delay to remoteCsvDefault by default.
	args = []string{
		fmt.Sprintf("--bitcoin.defaultremotedelay=%v",
			remoteCsvDefault),
	}
	bob, err := net.NewNode("Bob", args)
	require.NoError(t.t, err)
	defer require.NoError(t.t, net.ShutdownNode(bob))

	ctxb := context.Background()
	ctxt, _ := context.WithTimeout(ctxb, defaultTimeout)
	require.NoError(t.t, net.ConnectNodes(ctxt, alice, bob))

	// First, we open a channel from Alice to the other node. We
	// do not need to set any CSV values, because the recipient
	// node will use its default values.
	outgoingParams := lntest.OpenChannelParams{
		Amt: 50000,
	}

	openStream, err := net.OpenChannel(
		ctxt, alice, bob, outgoingParams,
	)
	openFailed := err != nil
	require.Equal(t.t, fail, openFailed)

	// If we do not expect the channel open to fail, we assert that
	// it is successfully opened.
	if !fail {
		mineBlocks(t, net, 6, 1)
		_, err := net.WaitForChannelOpen(ctxt, openStream)
		require.NoError(t.t, err)
	}

	// Now, we open a channel from Bob -> Alice. Again, we don't need to
	// set our csv value, because Bob's default should be set.
	incomingParams := lntest.OpenChannelParams{
		Amt: 50000,
	}

	openStream, err = net.OpenChannel(
		ctxt, bob, alice, incomingParams,
	)
	openFailed = err != nil
	require.Equal(t.t, fail, openFailed)

	// If we do not expect the channel open to fail, we assert that
	// it is successfully opened.
	if !fail {
		mineBlocks(t, net, 6, 1)
		_, err := net.WaitForChannelOpen(ctxt, openStream)
		require.NoError(t.t, err)
	}
}
