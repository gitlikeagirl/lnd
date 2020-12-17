package lnwire

import (
	"fmt"
	"io"
)

// StringError is metadata for a coded error which just contains an error
// string.
type StringError struct {
	// Value is an optional error string for the error.
	Value ErrorData
}

// Decode deserializes the error's metadata from the passed io.Reader.
//
// Note: part of the ErrorMetadata interface
func (c *StringError) Decode(r io.Reader) error {
	return ReadElements(r, &c.Value)
}

// Encode serializes the error's metadata into the passed io.Writer.
//
// Note: part of the ErrorMetadata interface
func (c *StringError) Encode(w io.Writer) error {
	return WriteElements(w, c.Value)
}

// Error returns the string representation of string error.
func (c *StringError) Error() string {
	errMsg := "non-ascii data"
	if isASCII(c.Value) {
		errMsg = string(c.Value)
	}

	return fmt.Sprintf("Message: %v", errMsg)
}

// InvalidSigMetadata contains the metadata reported for error code invalid
// signatures.
type InvalidSigMetadata struct {
	commitHeight uint64
}

// Decode deserializes the error's metadata from the passed io.Reader.
//
// Note: part of the ErrorMetadata interface
func (c *InvalidSigMetadata) Decode(r io.Reader) error {
	return ReadElements(r, &c.commitHeight)
}

// Encode serializes the error's metadata into the passed io.Writer.
//
// Note: part of the ErrorMetadata interface
func (c *InvalidSigMetadata) Encode(w io.Writer) error {
	return WriteElements(w, c.commitHeight)
}

// Error returns the string representation of a commit sig error.
func (c *InvalidSigMetadata) Error() string {
	return fmt.Sprintf("commit_height=%v", c.commitHeight)
}

// InvalidHtlcSigMetadata contains the metadata for an invalid htlc sig.
type InvalidHtlcSigMetadata struct {
	commitHeight uint64
	htlcIndex    uint64
}

// Decode deserializes the error's metadata from the passed io.Reader.
//
// Note: part of the ErrorMetadata interface
func (c *InvalidHtlcSigMetadata) Decode(r io.Reader) error {
	return ReadElements(r, &c.commitHeight, &c.htlcIndex)
}

// Encode serializes the error's metadata into the passed io.Writer.
//
// Note: part of the ErrorMetadata interface
func (c *InvalidHtlcSigMetadata) Encode(w io.Writer) error {
	return WriteElements(w, c.commitHeight, c.htlcIndex)
}

// Error returns the string representation of a commit sig error.
func (c *InvalidHtlcSigMetadata) Error() string {
	return fmt.Sprintf("commit_height=%v, hltc_index=%v", c.commitHeight,
		c.htlcIndex)
}
