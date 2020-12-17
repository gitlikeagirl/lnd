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
