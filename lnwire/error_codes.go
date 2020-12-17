package lnwire

// ErrorCode represents error codes as defined in (error code proposal).
type ErrorCode uint16

const (
	// ErrorCodeTemporary represents a generic temporary error.
	ErrorCodeTemporary ErrorCode = 0

	// ErrorCodePermanent represents a generic permanent error.
	ErrorCodePermanent ErrorCode = 1

	// ErrorCodeRemoteUnresponsive indicates that our peer took too long to
	// complete a commitment dance.
	ErrorCodeRemoteUnresponsive ErrorCode = 2

	// ErrorCodeRemoteDataLoss indicates that our peer's state is behind
	// ours, and they should recover the channel.
	ErrorCodeRemoteDataLoss ErrorCode = 3

	// ErrorCodeLocalDataLoss indicates that our state is behind our peer's,
	// and we need to recover the channel.
	ErrorCodeLocalDataLoss ErrorCode = 4

	// ErrorCodeInvalidCommitSig indicates that the remote peer sent us an
	// invalid commitment signature.
	ErrorCodeInvalidCommitSig ErrorCode = 5

	// ErrorCodeInvalidRevocation indicates that the remote peer send us an
	// invalid revocation message.
	ErrorCodeInvalidRevocation ErrorCode = 6

	// ErrorCodeInvalidHtlcSig indicates that we received an invalid htlc
	// sig.
	ErrorCodeInvalidHtlcSig ErrorCode = 7

	// ErrorCodeInvalidCommitSecret ?
	ErrorCodeInvalidCommitSecret ErrorCode = 8

	// ErrorCodeInvalidUnrevoked ???
	ErrorCodeInvalidUnrevoked ErrorCode = 9

	// ErrorCodeInvalidHtlcUpdate indicates that the remote node's htlc
	// update is invalid.
	ErrorCodeInvalidHtlcUpdate ErrorCode = 10

	// ErrorCodeInvalidSettle indicates that the remote node's htlc settle
	// is invalid.
	ErrorCodeInvalidSettle ErrorCode = 11

	// ErrorCodeInvalidFail indicates that the remote node's htlc fail is
	// invalid.
	ErrorCodeInvalidFail ErrorCode = 12

	// ErrorCodeInvalidFeeUpdate indicates that the remote node's fee update
	// is invalid.
	ErrorCodeInvalidFeeUpdate ErrorCode = 13

	// ErrorCodeReservation indicates that the parameters provided during
	// channel opening are unsuitable.
	ErrorCodeReservation ErrorCode = 14
)

// String returns the string representation of an error code.
func (e ErrorCode) String() string {
	switch e {
	case ErrorCodeTemporary:
		return "Temporary Error"

	case ErrorCodePermanent:
		return "Permanent Error"

	case ErrorCodeRemoteUnresponsive:
		return "Remote Unresponsive"

	case ErrorCodeRemoteDataLoss:
		return "Remote Data Loss"

	case ErrorCodeLocalDataLoss:
		return "Local Data Loss"

	case ErrorCodeInvalidCommitSig:
		return "Invalid Commit Sig"

	case ErrorCodeInvalidRevocation:
		return "Invalid Revocation"

	case ErrorCodeInvalidHtlcSig:
		return "Invalid Htlc Sig"

	case ErrorCodeInvalidCommitSecret:
		return "Invalid Commit Secret"

	case ErrorCodeInvalidUnrevoked:
		return "Invalid Unrevoked"

	case ErrorCodeInvalidHtlcUpdate:
		return "Invalid Htlc Update"

	case ErrorCodeInvalidSettle:
		return "Invalid Htlc Settle"

	case ErrorCodeInvalidFail:
		return "Invalid Htlc Failure"

	case ErrorCodeInvalidFeeUpdate:
		return "Invalid Fee Update"

	case ErrorCodeReservation:
		return "Reservation Error"

	default:
		return "Unknown"
	}
}
