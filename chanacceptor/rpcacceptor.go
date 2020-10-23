package chanacceptor

import (
	"errors"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

var errShuttingDown = errors.New("server shutting down")

// chanAcceptInfo contains a request for a channel acceptor decision, and a
// channel that the response should be sent on.
type chanAcceptInfo struct {
	Request  *ChannelAcceptRequest
	Response chan bool
}

// RPCAcceptor represents the RPC-controlled variant of the ChannelAcceptor.
// One RPCAcceptor allows one RPC client.
type RPCAcceptor struct {
	// receive is a function from which we receive channel acceptance
	// decisions. Note that this function is expected to block.
	receive func() (*lnrpc.ChannelAcceptResponse, error)

	// send is a function which sends requests for channel acceptance
	// decisions into our rpc stream.
	send func(request *lnrpc.ChannelAcceptRequest) error

	// requests is a channel that we send requests for a acceptor response
	// into.
	requests chan *chanAcceptInfo

	// timeout is the amount of time we allow the channel acceptance
	// decision to take. This time includes the time to send a query to the
	// acceptor, and the time it takes to receive a response.
	timeout time.Duration

	// done is closed when the rpc client terminates.
	done chan struct{}

	// quit is closed when lnd is shutting down.
	quit chan struct{}
}

// Accept is a predicate on the ChannelAcceptRequest which is sent to the RPC
// client who will respond with the ultimate decision. This function passes the
// request into the acceptor's requests channel, and returns the response it
// receives, failing the request if the timeout elapses.
//
// NOTE: Part of the ChannelAcceptor interface.
func (r *RPCAcceptor) Accept(req *ChannelAcceptRequest) bool {
	respChan := make(chan bool, 1)

	newRequest := &chanAcceptInfo{
		Request:  req,
		Response: respChan,
	}

	// timeout is the time after which ChannelAcceptRequests expire.
	timeout := time.After(r.timeout)

	// Send the request to the newRequests channel.
	select {
	case r.requests <- newRequest:

	case <-timeout:
		log.Errorf("RPCAcceptor returned false - reached timeout of %d",
			r.timeout)
		return false

	case <-r.done:
		return false

	case <-r.quit:
		return false
	}

	// Receive the response and return it. If no response has been received
	// in AcceptorTimeout, then return false.
	select {
	case resp := <-respChan:
		return resp

	case <-timeout:
		log.Errorf("RPCAcceptor returned false - reached timeout of %d",
			r.timeout)
		return false

	case <-r.done:
		return false

	case <-r.quit:
		return false
	}
}

// NewRPCAcceptor creates and returns an instance of the RPCAcceptor.
func NewRPCAcceptor(receive func() (*lnrpc.ChannelAcceptResponse, error),
	send func(*lnrpc.ChannelAcceptRequest) error,
	timeout time.Duration, quit chan struct{}) *RPCAcceptor {

	return &RPCAcceptor{
		receive:  receive,
		send:     send,
		requests: make(chan *chanAcceptInfo),
		timeout:  timeout,
		done:     make(chan struct{}),
		quit:     quit,
	}
}

// Start runs the rpc acceptor
func (r *RPCAcceptor) Run() error {
	// Close the done channel to indicate that the acceptor is no longer
	// listening and any in-progress requests should be terminated.
	defer close(r.done)

	// Create a channel that responses
	responses := make(chan lnrpc.ChannelAcceptResponse)

	// errChan is used by the receive loop to signal any errors that occur
	// during reading from the stream. This is primarily used to shutdown
	// the send loop in the case of an RPC client disconnecting.
	errChan := make(chan error, 1)

	// Start a goroutine to receive responses from the channel acceptor.
	// We expect the receive function to block, so it must be run in a
	// goroutine (otherwise we could not send more than one channel accept
	// request to the client).
	go func() {
		for {
			resp, err := r.receive()
			if err != nil {
				errChan <- err
				return
			}

			var pendingID [32]byte
			copy(pendingID[:], resp.PendingChanId)

			openChanResp := lnrpc.ChannelAcceptResponse{
				Accept:        resp.Accept,
				PendingChanId: pendingID[:],
			}

			// We have received a decision for one of our channel
			// acceptor requests.
			select {
			case responses <- openChanResp:

			case <-r.done:
				return

			case <-r.quit:
				return
			}
		}
	}()

	acceptRequests := make(map[[32]byte]chan bool)

	for {
		select {
		// Consume requests passed to us from our Accept() function and
		// send them into our stream.
		case newRequest := <-r.requests:

			req := newRequest.Request
			pendingChanID := req.OpenChanMsg.PendingChannelID

			acceptRequests[pendingChanID] = newRequest.Response

			// A ChannelAcceptRequest has been received, send it to the client.
			chanAcceptReq := &lnrpc.ChannelAcceptRequest{
				NodePubkey:       req.Node.SerializeCompressed(),
				ChainHash:        req.OpenChanMsg.ChainHash[:],
				PendingChanId:    req.OpenChanMsg.PendingChannelID[:],
				FundingAmt:       uint64(req.OpenChanMsg.FundingAmount),
				PushAmt:          uint64(req.OpenChanMsg.PushAmount),
				DustLimit:        uint64(req.OpenChanMsg.DustLimit),
				MaxValueInFlight: uint64(req.OpenChanMsg.MaxValueInFlight),
				ChannelReserve:   uint64(req.OpenChanMsg.ChannelReserve),
				MinHtlc:          uint64(req.OpenChanMsg.HtlcMinimum),
				FeePerKw:         uint64(req.OpenChanMsg.FeePerKiloWeight),
				CsvDelay:         uint32(req.OpenChanMsg.CsvDelay),
				MaxAcceptedHtlcs: uint32(req.OpenChanMsg.MaxAcceptedHTLCs),
				ChannelFlags:     uint32(req.OpenChanMsg.ChannelFlags),
			}

			if err := r.send(chanAcceptReq); err != nil {
				return err
			}

		// Process newly received responses from our channel acceptor,
		// looking the original request up in our map of requests and
		// dispatching the response.
		case resp := <-responses:
			// Look up the appropriate channel to send on given the
			// pending ID. If a channel is found, send the response
			// over it.
			var pendingID [32]byte
			copy(pendingID[:], resp.PendingChanId)
			respChan, ok := acceptRequests[pendingID]
			if !ok {
				continue
			}

			// Send the response boolean over the buffered response
			// channel.
			respChan <- resp.Accept

			// Delete the channel from the acceptRequests map.
			delete(acceptRequests, pendingID)

		// If we failed to receive from our acceptor, we exit.
		case err := <-errChan:
			log.Errorf("Received an error: %v, shutting down", err)
			return err

		// Exit if we are shutting down.
		case <-r.quit:
			return errShuttingDown
		}
	}
}

// A compile-time constraint to ensure RPCAcceptor implements the ChannelAcceptor
// interface.
var _ ChannelAcceptor = (*RPCAcceptor)(nil)
