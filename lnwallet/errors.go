package lnwallet

// ReservationError wraps certain errors returned during channel reservation
// that can be sent across the wire to the remote peer. Errors not being
// ReservationErrors will not be sent to the remote in case of a failed channel
// reservation, as they may contain private information.
type ReservationError struct {
	error
}

// A compile tim
