package lnwire

import (
	"errors"
	"fmt"
	"io"
)

// ErrUnknownEvenErrorCode is returned if we try to decode an even feature bit
// that we do not know.
var ErrUnknownEvenErrorCode = errors.New("unknown even feature bit")

// CodedError represents an error with an error code and optional metadata.
type CodedError struct {
	// ChannelID is the channel that the error is associated with. If this
	// value is empty, the error applies to all channels.
	ChannelID

	// ErrorCode is the spec-defined error code for the error.
	ErrorCode

	// ErrorMetadata contains additional information for the error aside
	// from its code and channel id.
	ErrorMetadata
}

// Error returns a string representing a coded error, including its metadata if
// it is non-nil.
func (c *CodedError) Error() string {
	errStr := fmt.Sprintf("ErrorCode: %v", c.ErrorCode)

	if c.ErrorMetadata == nil {
		return errStr
	}

	return fmt.Sprintf("%v, Metadata: %v", errStr, c.ErrorMetadata.Error())
}

// Encode serializes the target CodedError into the passed io.Writer observing
// the protocol version specified.
//
// This is part of the lnwire.Message interface.
func (c *CodedError) Encode(w io.Writer, pver uint32) error {
	err := WriteElements(w, c.ChannelID, uint16(c.ErrorCode))
	if err != nil {
		return err
	}

	// If we have no metadata, we do not need to encode it.
	if c.ErrorMetadata == nil {
		return nil
	}

	return c.ErrorMetadata.Encode(w)
}

// Decode deserializes a serialized CodedError message stored in the passed
// io.Reader observing the specified protocol version.
//
// This is part of the lnwire.Message interface.
func (c *CodedError) Decode(r io.Reader, pver uint32) error {
	var errorCode uint16
	if err := ReadElements(r, &c.ChannelID, &errorCode); err != nil {
		return err
	}

	c.ErrorCode = ErrorCode(errorCode)

	// Decode metadata according to the error code that is set.
	switch c.ErrorCode {
	case ErrorCodeTemporary:
		c.ErrorMetadata = &StringError{}

	case ErrorCodePermanent:
		c.ErrorMetadata = &StringError{}

	default:
		// If we have an unknown even error code, we should fail.
		if c.ErrorCode%2 == 0 {
			return ErrUnknownEvenErrorCode
		}

		// If we have an unknown odd error, it's ok, we just ignore it
		// and do not decode any metadata.
		return nil
	}

	return c.ErrorMetadata.Decode(r)
}

// MsgType returns the integer uniquely identifying an CodedError message on the
// wire.
//
// This is part of the lnwire.Message interface.
func (c *CodedError) MsgType() MessageType {
	return MsgCodedError
}

// MaxPayloadLength returns the maximum allowed payload size for an Error
// complete message observing the specified protocol version.
//
// This is part of the lnwire.Message interface.
func (c *CodedError) MaxPayloadLength(uint32) uint32 {
	// 32 + 2 + 65501
	return 65535
}

// ErrorMetadata is an interface that is implemented by the different types
// of errors.
type ErrorMetadata interface {
	// Encode serializes the error's metadata into the passed io.Writer.
	Encode(w io.Writer) error

	// Decode deserializes the error's metadata from the passed io.Reader.
	Decode(r io.Reader) error

	error
}

// NewGenericError creates a generic error with the channel and value provided.
// It takes a temporary boolean which indicates whether to use a temporary or
// permanent error code.
func NewGenericError(id ChannelID, value ErrorData,
	temporary bool) *CodedError {

	code := ErrorCodePermanent
	if temporary {
		code = ErrorCodeTemporary
	}

	return &CodedError{
		ChannelID: id,
		ErrorCode: code,
		ErrorMetadata: &StringError{
			Value: value,
		},
	}
}
