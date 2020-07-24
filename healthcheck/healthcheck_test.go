package healthcheck

import (
	"errors"
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/ticker"
	"github.com/stretchr/testify/require"
)

var (
	errNonNil = errors.New("non-nil test error")
	timeout   = time.Second
	testTime  = time.Unix(1, 2)
)

// TestMonitor tests creation and triggering of a monitor with a health check.
func TestMonitor(t *testing.T) {
	intervalTicker := ticker.NewForce(time.Hour)

	errChan := make(chan error)
	shutdown := make(chan struct{})

	// Create our config for monitoring. We will use a 0 back off so that
	// out test does not need to wait.
	cfg := &Config{
		Checks: []*Observation{
			{
				Check: func() error {
					return <-errChan
				},
				Interval: intervalTicker,
				Attempts: 2,
				Backoff:  0,
				Timeout:  time.Hour,
			},
		},
		Shutdown: func(string, ...interface{}) {
			shutdown <- struct{}{}
		},
	}
	monitor := NewMonitor(cfg)

	require.NoError(t, monitor.Start(), "could not start monitor")

	// Tick is a helper we will use to tick our interval.
	tick := func() {
		select {
		case intervalTicker.Force <- testTime:
		case <-time.After(timeout):
			t.Fatal("could not tick timer")
		}
	}

	// errResponse is a helper which sends an error into our error channel.
	// This function should be used every time we want our health check to
	// fail.
	errResponse := func(err error) {
		select {
		case errChan <- err:
		case <-time.After(timeout):
			t.Fatal("could not send error response")
		}
	}

	// Tick our timer and provide our error channel with a nil error. This
	// mocks our check function succeeding on the first call.
	tick()
	errResponse(nil)

	// Now we tick our timer again. This time send a non-nil error, followed
	// by a nil error. This tests our retry logic, because we allow 2
	// retries, so should recover without needing to shutdown.
	tick()
	errResponse(errNonNil)
	errResponse(nil)

	// Finally, we tick our timer once more, and send two non-nil errors
	// into our error channel. This mocks our check function failing twice.
	tick()
	errResponse(errNonNil)
	errResponse(errNonNil)

	// Since we have failed within our allowed number of retries, we now
	// expect a call to our shutdown function.
	select {
	case <-shutdown:
	case <-time.After(timeout):
		t.Fatal("expected shutdown")
	}

	require.NoError(t, monitor.Stop(), "could not stop monitor")
}

// callCounter allows tracking of calls to a function. It is created with a list
// of responses, where the index of a response in the list indicates the call
// count at which it will be returned - index 0 is returned on our first call,
// index 1 on our second, etc.
type callCounter struct {
	count     int
	responses []callResponse
}

func newCallCounter(responses []callResponse) *callCounter {
	return &callCounter{
		responses: responses,
	}
}

// callResponse describes a desired delay and error response for a call.
type callResponse struct {
	delay time.Duration
	err   error
}

func newCallResponse(err error, delay time.Duration) callResponse {
	return callResponse{
		delay: delay,
		err:   err,
	}
}

func (c *callCounter) call() error {
	// Get our intended response at this call count. We allow our test to
	// panic if we are out of range, because this indicates that the call
	// counter has been incorrectly setup.
	resp := c.responses[c.count]

	// Increment our counter once we have retrieved our error for this
	// round.
	c.count++

	// Sleep for the delay set by this response.
	time.Sleep(resp.delay)

	return resp.err
}

// TestRetryCheck tests our retry logic. It does not include a test for exiting
// during the back off period.
func TestRetryCheck(t *testing.T) {
	tests := []struct {
		name string

		// responses provides a list of call responses that we want our
		// check to return.
		responses []callResponse

		// attempts is the number of times we call a check before
		// failing.
		attempts int

		// timeout is the time we allow our check to take before we
		// fail them.
		timeout time.Duration

		// expected calls is the number of times we expect our function
		// to be called.
		expectedCalls int

		// expectedShutdown is true if we expect a shutdown to be
		// triggered because all of our calls failed.
		expectedShutdown bool
	}{
		{
			name: "first call succeeds",
			responses: []callResponse{
				newCallResponse(nil, 0),
			},
			attempts:         2,
			timeout:          time.Hour,
			expectedCalls:    1,
			expectedShutdown: false,
		},
		{
			name: "first call fails",
			responses: []callResponse{
				newCallResponse(errNonNil, 0),
			},
			attempts:         1,
			timeout:          time.Hour,
			expectedCalls:    1,
			expectedShutdown: true,
		},
		{
			name: "fail then recover",
			responses: []callResponse{
				newCallResponse(errNonNil, 0),
				newCallResponse(nil, 0),
			},
			attempts:         2,
			timeout:          time.Hour,
			expectedCalls:    2,
			expectedShutdown: false,
		},
		{
			name: "always fail",
			responses: []callResponse{
				newCallResponse(errNonNil, 0),
				newCallResponse(errNonNil, 0),
			},
			attempts:         2,
			timeout:          time.Hour,
			expectedCalls:    2,
			expectedShutdown: true,
		},
		{
			name:             "no calls",
			responses:        nil,
			attempts:         0,
			timeout:          time.Hour,
			expectedCalls:    0,
			expectedShutdown: false,
		},
		{
			name: "call times out",
			responses: []callResponse{
				newCallResponse(nil, 2),
			},
			attempts:         1,
			timeout:          1,
			expectedCalls:    1,
			expectedShutdown: true,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			var shutdown bool
			shutdownFunc := func(string, ...interface{}) {
				shutdown = true
			}

			caller := newCallCounter(test.responses)

			// Create an observation that calls our call counting
			// function. We set a zero back off so that the test
			// will not wait.
			observation := &Observation{
				Check:    caller.call,
				Attempts: test.attempts,
				Timeout:  test.timeout,
				Backoff:  0,
			}
			quit := make(chan struct{})

			observation.retryCheck(quit, shutdownFunc)

			require.Equal(t, test.expectedCalls, caller.count,
				"call count wrong")

			require.Equal(t, test.expectedShutdown, shutdown,
				"unexpected shutdown state")
		})
	}
}
