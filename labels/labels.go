// Package labels contains labels used to label transactions broadcast by lnd.
// These labels are used across packages, so they are declared in a separate
// package to avoid dependency issues.
package labels

const (
	// OpenChannel labels a transaction as a channel open.
	OpenChannel = "open channel"

	// CloseChannelLabel labels a transaction as a channel close.
	CloseChannel = "close channel"

	// JusticeTx labels a transaction as a justice penalty tx.
	JusticeTx = "justice"

	// SweepLabel labels a transaction as a sweep.
	Sweep = "sweep"

	// UserSend labels a transaction as user initiated via the api. This
	// label is only used when a custom user provided label is not given.
	UserSend = "user send"
)
