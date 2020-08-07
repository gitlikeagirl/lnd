// Package mempool takes a mempool source and accepts requests for notifications
// of transactions matching a template that describes the structure of the
// witness we are looking for.
package mempool

import (
	"fmt"
    "github.com/lightningnetwork/lnd/subscribe"
    "sync"
	"sync/atomic"

	"github.com/btcsuite/btcd/wire"
	"github.com/prometheus/common/log"
)

// Config contains the external functionality we need to run our mempool
// watcher.
type Config struct {
	GetMempool func() (chan *wire.MsgTx, chan error, error)
}

// Watcher monitors the mempool and accepts request for subscriptions to
// transactions that match a specific template.
type Watcher struct {
	started int32 // to be used atomically
	stopped int32 // to be used atomically

	cfg *Config

	requests chan RegisterWitnessTemplate

	// subscriptions holds the current set of subscriptions that are
	// registered with our watcher.
	subscriptions map[int]templateSubscription

	// subscriptionCounter provides each new subscription with a unique
	// index in our subscriptions map.
	subscriptionCounter int

	wg   sync.WaitGroup
	quit chan struct{}
}

type templateSubscription struct {
	// WitnessTemplateRegistration contains the channels we need to communicate
	// with the client.
	WitnessTemplateRegistration

	// template is the template that the client registered to get notifications
	// for.
	template []witnessMatcher

	server subscribe.Server
}

// RegisterWitnessTemplate contains the details a client needs to register for
// notifications for a witness template.
type RegisterWitnessTemplate struct {
	Template []witnessMatcher

	// Response returns a subscription client to the caller which
	// transactions matching the template provided will be sent on.
	Response chan subscribe.Client
}

type WitnessSubscription struct {
    client subscribe.Client
}


// NewWatcher creates a new mempool watcher.
func NewWatcher(cfg *Config) *Watcher {
	return &Watcher{
		cfg:  cfg,
		quit: make(chan struct{}),
	}
}

// Start the watcher and all the goroutines that it requires.
func (w *Watcher) Start() error {
	if !atomic.CompareAndSwapInt32(&w.started, 0, 1) {
		return fmt.Errorf("watcher already started")
	}

	// Subscribe to our mempool source. If it is not available, we cannot
	// monitor the mempool so we fail on start.
	mempool, errChan, err := w.cfg.GetMempool()
	if err != nil {
		return err
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		if err := w.matchTemplates(mempool, errChan); err != nil {
			log.Errorf("error matching templates: %v", err)
		}
	}()

	return nil
}

// Stop shuts down the watcher and waits for all of its goroutines to exit.
func (w *Watcher) Stop() error {
	if !atomic.CompareAndSwapInt32(&w.stopped, 0, 1) {
		return fmt.Errorf("watcher already stopped")
	}

	close(w.quit)
	w.wg.Wait()

	return nil
}

// matchTemplates is the main event loop for our watcher. It is responsible for
// consuming transactions from our mempool source, matching them with any
// templates that have active subscriptions and dispatching notifications to
// subscribing clients when matches are found.
func (w *Watcher) matchTemplates(mempool chan *wire.MsgTx,
	mempoolErr chan error) error {

	for {
		select {
		// If a new mempool transaction arrives, we try to match it to
		// our set of registered witness templates.
		case tx := <-mempool:
			index, err := w.matchMempoolTx(tx)
			if err != nil {
				return err
			}

		case request := <-w.requests:
			index := w.subscriptionCounter


			server:= subscribe.NewServer()

			resp := WitnessTemplateRegistration{
				Transactions: make(chan *wire.MsgTx),
				Done:         w.quit,
				Cancel:
			}

			w.subscriptions[index] = templateSubscription{
				WitnessTemplateRegistration: resp,
				template:                    request.Template,
			}

			w.subscriptionCounter++

			request.Registration <- resp

		// If our mempool subscription fails, exit with this error
		// because we require our mempool subscription to operate.
		case err := <-mempoolErr:
			return err

		// If we get the instruction to shutdown, exit without error.
		case <-w.quit:
			return nil
		}
	}
}

func (w *Watcher) matchMempoolTx(tx *wire.MsgTx) (int, error) {
	// Run through each of our inputs and check whether the witness used to
	// spend this input matches any of our registered templates.
	for _, txin := range tx.TxIn {

		for _, subscription := range w.subscriptions {
			match, err := match(subscription.template, txin.Witness)
			if err != nil {
				return 0, err
			}

			// Send an update to this
		}
	}

	return nil
}
