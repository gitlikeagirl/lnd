package lnwire

// ErrorCode represents error codes as defined in (error code proposal).
type ErrorCode uint16

const (
	// ErrorCodeTemporary represents a generic temporary error.
	ErrorCodeTemporary ErrorCode = 0

	// ErrorCodePermanent represents a generic permanent error.
	ErrorCodePermanent ErrorCode = 1
)

// String returns the string representation of an error code.
func (e ErrorCode) String() string {
	switch e {
	case ErrorCodeTemporary:
		return "Temporary Error"

	case ErrorCodePermanent:
		return "Permanent Error"

	default:
		return "Unknown"
	}
}
