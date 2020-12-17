package htlcswitch

import "github.com/go-errors/errors"

var (
	// ErrLinkShuttingDown signals that the link is shutting down.
	ErrLinkShuttingDown = errors.New("link shutting down")
)
