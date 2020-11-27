package lnwire

import "math"

// ErrorCode represents the error code set
type ErrorCode uint16

const (
	ErrorCodeGeneric           ErrorCode = 0
	ErrorCodeChannelParameters ErrorCode = 1
	ErrorCodeUnknownHtlcIndex  ErrorCode = 2

	ErrorCodeUnknown ErrorCode = math.MaxUint16
)

// String returns the string representation of an error code.
func (e ErrorCode) String() string {
	switch e {
	case ErrorCodeGeneric:
		return "Generic Error"

	case ErrorCodeChannelParameters:
		return "Channel Parameters"

	case ErrorCodeUnknownHtlcIndex:
		return "Unknown HTLC index"

	default:
		return "Unknown"
	}
}
