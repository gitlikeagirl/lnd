package lnwire

import (
	"fmt"
	"io"

	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// The following TLV types are used for the ChannelParameters error
	// type.
	chanParamMaxSizeType    tlv.Type = 0
	chanParamMinSizeType    tlv.Type = 1
	chanParamMinReserveType tlv.Type = 2
	chanParamMaxReserveType tlv.Type = 3
	chanParamMaxCSV         tlv.Type = 4
	chanParamNoPush         tlv.Type = 5
)

// CodedError represents an error with an error code and optional
// metadata.
type CodedError struct {
	ChannelID ChannelID
	ErrorCode
	ErrorMetadata
}

func (c *CodedError) Error() string {
	// TODO(carla): remove channel ID, it is separate in the message
	errStr := fmt.Sprintf("Channel: %v, ErrorCode: %v", c.ChannelID,
		c.ErrorCode)

	if c.ErrorMetadata == nil {
		return errStr
	}

	return fmt.Sprintf("%v, Metadata: %v", errStr, c.ErrorMetadata.Error())
}

// Encode serializes the target CodedError into the passed io.Writer observing the
// protocol version specified.
//
// This is part of the lnwire.Message interface.
func (c *CodedError) Encode(w io.Writer, pver uint32) error {
	if err := WriteElements(w, c.ChannelID, uint16(c.ErrorCode)); err != nil {
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

	switch c.ErrorCode {
	case ErrorCodeGeneric:

	case ErrorCodeChannelParameters:
		c.ErrorMetadata = &ChannelParametersError{}

	case ErrorCodeUnknownHtlcIndex:
		c.ErrorMetadata = &UnknownHtlcIndex{}

	default:
		// If we have an unknown even error code, we should fail.
		if c.ErrorCode%2 == 0 {
			return fmt.Errorf("unknown even error code: %v",
				c.ErrorCode)
		}

		// If we have an unknown odd error, it's ok.
		// TODO(Carla):read all bytes into a string
		c.ErrorCode = ErrorCodeUnknown
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
	// TODO(carla)
	// 32 + 2 + 65501
	return 65535
}

// ErrorMetadata is an interface that is implemented by the different types
// of errors.
type ErrorMetadata interface {
	// Encode serializes the error's metadata into the passed io.Writer.
	Encode(w io.Writer) error

	// Encode deserializes the error's metadata from the passed io.Reader.
	Decode(r io.Reader) error

	// Error returns an error string for the metadata.
	Error() string
}

func NewGenericError(id ChannelID, value string) *CodedError {
	return &CodedError{
		ChannelID: id,
		ErrorCode: ErrorCodeGeneric,
		ErrorMetadata: &StringError{
			Value: ErrorData(value),
		},
	}
}

type StringError struct {
	Value ErrorData
}

func (c *StringError) Decode(r io.Reader) error {
	return ReadElements(r, &c.Value)
}

func (c *StringError) Encode(w io.Writer) error {
	return WriteElements(w, c.Value)
}

// Error returns the string representation of string error.
func (c *StringError) Error() string {
	return string(c.Value)
}

type UnknownHtlcIndex struct {
	Index uint64
}

func (c *UnknownHtlcIndex) Decode(r io.Reader) error {
	return ReadElements(r, &c.Index)
}

func (c *UnknownHtlcIndex) Encode(w io.Writer) error {
	return WriteElements(w, c.Index)
}

// Error returns the string representation of a unknown htlc index error.
func (c *UnknownHtlcIndex) Error() string {
	return fmt.Sprintf("index=%v", c.Index)
}

func NewUnknownHtlcIndexError(id ChannelID, index uint64) *CodedError {
	return &CodedError{
		ChannelID: id,
		ErrorCode: ErrorCodeUnknownHtlcIndex,
		ErrorMetadata: &UnknownHtlcIndex{
			Index: index,
		},
	}
}

// ChannelParametersError represents an error sent by a node that has
// rejected a
type ChannelParametersError struct {
	MaxChannelSize btcutil.Amount
	MinChannelSize btcutil.Amount
	MinimumReserve btcutil.Amount
	MaximumReserve btcutil.Amount
	MaxCSV         uint32
	NoPush         bool
}

func (c *ChannelParametersError) Decode(r io.Reader) error {
	tlvStream, err := tlv.NewStream(
		tlv.MakePrimitiveRecord(chanParamMaxSizeType, &c.MaxChannelSize),
		tlv.MakePrimitiveRecord(chanParamMinSizeType, &c.MinChannelSize),
		tlv.MakePrimitiveRecord(chanParamMinReserveType, &c.MinimumReserve),
		tlv.MakePrimitiveRecord(chanParamMaxCSV, &c.MaxCSV),
		tlv.MakePrimitiveRecord(chanParamNoPush, &c.NoPush),
	)
	if err != nil {
		return err
	}

	return tlvStream.Decode(r)
}

func (c *ChannelParametersError) Encode(w io.Writer) error {
	tlvStream, err := tlv.NewStream(
		tlv.MakePrimitiveRecord(chanParamMaxSizeType, &c.MaxChannelSize),
		tlv.MakePrimitiveRecord(chanParamMinSizeType, &c.MinChannelSize),
		tlv.MakePrimitiveRecord(chanParamMinReserveType, &c.MinimumReserve),
		tlv.MakePrimitiveRecord(chanParamMaxReserveType, &c.MaximumReserve),
		tlv.MakePrimitiveRecord(chanParamMaxCSV, &c.MaxCSV),
		tlv.MakePrimitiveRecord(chanParamNoPush, &c.NoPush),
	)
	if err != nil {
		return err
	}

	return tlvStream.Encode(w)
}

// Error returns the string representation of a channel params error.
func (c *ChannelParametersError) Error() string {
	return fmt.Sprintf("max channel: %v, min channel: %v, min "+
		"reserve: %v, max reserve: %v,  max csv: %v, no push: %v",
		c.MaxChannelSize, c.MinChannelSize, c.MinimumReserve,
		c.MaximumReserve, c.MaxCSV, c.NoPush)
}

func NewChannelParametersError(channelID ChannelID, maxChannelSize,
	minChannelSize, minReserve, maxReserve btcutil.Amount, maxCSV uint32,
	noPush bool) *CodedError {

	return &CodedError{
		ChannelID: channelID,
		ErrorCode: ErrorCodeChannelParameters,
		ErrorMetadata: &ChannelParametersError{
			MaxChannelSize: maxChannelSize,
			MinChannelSize: minChannelSize,
			MinimumReserve: minReserve,
			MaxCSV:         maxCSV,
			NoPush:         noPush,
		},
	}
}
