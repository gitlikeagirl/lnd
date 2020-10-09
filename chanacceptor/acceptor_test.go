package chanacceptor

import (
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/assert"
)

var timeout = time.Second

func randKey(t *testing.T) *btcec.PublicKey {
	t.Helper()

	priv, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		t.Fatalf("unable to generate new public key")
	}

	return priv.PubKey()
}

type channelAcceptorCtx struct {
	t *testing.T

	// extRequests is the channel that we send our channel accept requests
	// into, this channel mocks sending of a request to the rpc acceptor.
	// This channel should be buffered with the number of requests we want
	// to send so that it does not block (like a rpc stream).
	extRequests chan []byte

	// responses is a map of pending channel IDs to the response which we
	// wish to mock the remote channel acceptor sending.
	responses map[[32]byte]*lnrpc.ChannelAcceptResponse

	// acceptor is the channel acceptor we create for the test.
	acceptor *RPCAcceptor

	// errChan is a channel that the error the channel acceptor exits with
	// is sent into.
	errChan chan error

	// quit is a channel that can be used to shutdown the channel acceptor
	// and return errShuttingDown.
	quit chan struct{}
}

func newChanAcceptorCtx(t *testing.T, acceptCallCount int,
	responses map[[32]byte]*lnrpc.ChannelAcceptResponse) *channelAcceptorCtx {

	testCtx := &channelAcceptorCtx{
		t:           t,
		extRequests: make(chan []byte, acceptCallCount),
		responses:   responses,
		errChan:     make(chan error),
		quit:        make(chan struct{}),
	}

	testCtx.acceptor = NewRPCAcceptor(
		testCtx.receiveResponse, testCtx.sendRequest, timeout*5,
		testCtx.quit,
	)

	return testCtx
}

// sendRequest mocks sending a request to the channel acceptor.
func (c *channelAcceptorCtx) sendRequest(request *lnrpc.ChannelAcceptRequest) error {
	select {
	case c.extRequests <- request.PendingChanId:

	case <-time.After(timeout):
		c.t.Fatalf("timeout sending request: %v", request.PendingChanId)
	}

	return nil
}

// receiveResponse mocks sending of a response from the channel acceptor.
func (c *channelAcceptorCtx) receiveResponse() (*lnrpc.ChannelAcceptResponse,
	error) {

	select {
	case id := <-c.extRequests:
		scratch := [32]byte{}
		copy(scratch[:], id)

		resp, ok := c.responses[scratch]
		assert.True(c.t, ok)

		return resp, nil

	case <-time.After(timeout):
		c.t.Fatalf("timeout receiving request")
		return nil, fmt.Errorf("timeout")
	}
}

// start runs our channel acceptor in a goroutine which sends its exit error
// into our test error channel.
func (c *channelAcceptorCtx) start() {
	go func() {
		c.errChan <- c.acceptor.Run()
	}()
}

// stop shuts down the test's channel acceptor and asserts that it exits with
// our expected error.
func (c *channelAcceptorCtx) stop() {
	close(c.quit)
	c.assertFinished(errShuttingDown)
}

// assertFinished asserts that the acceptor exits with the error provided.
func (c *channelAcceptorCtx) assertFinished(expected error) {
	select {
	case actual := <-c.errChan:
		assert.Equal(c.t, expected, actual)

	case <-time.After(timeout):
		c.t.Fatal("timeout waiting for acceptor to exit")
	}
}

// queryAndAssert takes a map of channel ids which we want to call Accept for
// to the outcome we expect from the acceptor, dispatches each request in a
// goroutine and then asserts that we get the outcome we expect.
func (c *channelAcceptorCtx) queryAndAssert(queries map[[32]byte]bool) {
	var (
		node      = randKey(c.t)
		responses = make(chan struct{})
	)

	for request, expected := range queries {
		request := request
		expected := expected

		go func() {
			resp := c.acceptor.Accept(&ChannelAcceptRequest{
				Node: node,
				OpenChanMsg: &lnwire.OpenChannel{
					PendingChannelID: request,
				},
			})
			assert.Equal(c.t, expected, resp)
			responses <- struct{}{}
		}()
	}

	// Wait for each of our requests to return a response before we exit.
	for i := 0; i < len(queries); i++ {
		select {
		case <-responses:
		case <-time.After(timeout):
			c.t.Fatalf("did not receive response")
		}
	}
}

// TestMultipleAcceptClients tests that the RPC acceptor is capable of handling
// multiple requests to its Accept function and responding to them correctly.
func TestMultipleAcceptClients(t *testing.T) {
	var (
		chan1 = [32]byte{1}
		chan2 = [32]byte{2}
		chan3 = [32]byte{3}

		// Queries is a map of the channel IDs we will query Accept
		// with, and the set of outcomes we expect.
		queries = map[[32]byte]bool{
			chan1: true,
			chan2: false,
			chan3: false,
		}

		// Responses is a mocked set of responses from the remote
		// channel acceptor.
		responses = map[[32]byte]*lnrpc.ChannelAcceptResponse{
			chan1: {
				PendingChanId: chan1[:],
				Accept:        true,
			},
			chan2: {
				PendingChanId: chan2[:],
				Accept:        false,
			},
			chan3: {
				PendingChanId: chan3[:],
				Accept:        false,
			},
		}
	)

	// Create and start our channel acceptor.
	testCtx := newChanAcceptorCtx(t, len(queries), responses)
	testCtx.start()

	// Dispatch three queries and assert that we get our expected response.
	// for each.
	testCtx.queryAndAssert(queries)

	// Shutdown our acceptor.
	testCtx.stop()
}
