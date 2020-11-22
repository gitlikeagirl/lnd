// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Heavily inspired by https://github.com/btcsuite/btcd/blob/master/signal.go
// Copyright (C) 2015-2017 The Lightning Network Developers

package signal

import (
	"errors"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
)

var (
	// started indicates whether we have started our main interrupt handler.
	// This field should be used atomically.
	started int32
)

type Interceptor struct {
	// InterruptChannel is used to receive SIGINT (Ctrl+C) signals.
	InterruptChannel chan os.Signal

	// ShutdownChannel is closed once the main interrupt handler exits.
	ShutdownChannel chan struct{}

	// ShutdownRequestChannel is used to request the daemon to shutdown
	// gracefully, similar to when receiving SIGINT.
	ShutdownRequestChannel chan struct{}

	// Quit is closed when instructing the main interrupt handler to exit.
	Quit chan struct{}
}

// Intercept starts the interception of interrupt signals. Note that this
// function can only be called once.
func Intercept() (Interceptor, error) {
	if !atomic.CompareAndSwapInt32(&started, 0, 1) {
		return Interceptor{}, errors.New("intercept already started")
	}

	channels := Interceptor{
		InterruptChannel:       make(chan os.Signal, 1),
		ShutdownChannel:        make(chan struct{}),
		ShutdownRequestChannel: make(chan struct{}),
		Quit:                   make(chan struct{}),
	}

	// shutdownRequestChannel = make(chan struct{})
	// quit = make(chan struct{})

	signalsToCatch := []os.Signal{
		os.Interrupt,
		os.Kill,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	}
	signal.Notify(channels.InterruptChannel, signalsToCatch...)
	go mainInterruptHandler(channels)

	return channels, nil
}

// mainInterruptHandler listens for SIGINT (Ctrl+C) signals on the
// interruptChannel and shutdown requests on the shutdownRequestChannel, and
// invokes the registered interruptCallbacks accordingly. It also listens for
// callback registration.
// It must be run as a goroutine.
func mainInterruptHandler(channels Interceptor) {
	defer atomic.StoreInt32(&started, 0)
	// isShutdown is a flag which is used to indicate whether or not
	// the shutdown signal has already been received and hence any future
	// attempts to add a new interrupt handler should invoke them
	// immediately.
	var isShutdown bool

	// shutdown invokes the registered interrupt handlers, then signals the
	// shutdownChannel.
	shutdown := func() {
		// Ignore more than one shutdown signal.
		if isShutdown {
			log.Infof("Already shutting down...")
			return
		}
		isShutdown = true
		log.Infof("Shutting down...")

		// Signal the main interrupt handler to exit, and stop accept
		// post-facto requests.
		close(channels.Quit)
	}

	for {
		select {
		case signal := <-channels.InterruptChannel:
			log.Infof("Received %v", signal)
			shutdown()

		case <-channels.ShutdownRequestChannel:
			log.Infof("Received shutdown request.")
			shutdown()

		case <-channels.Quit:
			log.Infof("Gracefully shutting down.")
			close(channels.ShutdownChannel)
			signal.Stop(channels.InterruptChannel)
			return
		}
	}
}

// Listening returns true if the main interrupt handler has been started, and
// has not been killed.
func (channels Interceptor) Listening() bool {
	// If our started field is not set, we are not yet listening for
	// interrupts.
	if atomic.LoadInt32(&started) != 1 {
		return false
	}

	// If we have started our main goroutine, we check whether we have
	// stopped it yet.
	return channels.Alive()
}

// Alive returns true if the main interrupt handler has not been killed.
func (channels Interceptor) Alive() bool {
	select {
	case <-channels.Quit:
		return false
	default:
		return true
	}
}

// RequestShutdown initiates a graceful shutdown from the application.
func (channels Interceptor) RequestShutdown() {
	select {
	case channels.ShutdownRequestChannel <- struct{}{}:
	case <-channels.Quit:
	}
}
