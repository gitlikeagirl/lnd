package lnwire

import (
	"fmt"
	"io"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
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

// ReservationMetadata contains the parameters our node requires for channels.
type ReservationMetadata struct {
	// ChainHash is the hash of the genisis block of the chain that the node
	// supports.
	chainHash *chainhash.Hash

	// minSize is the minimum channel size we will accept.
	minSize btcutil.Amount

	// maxSize is the maximum channel size we will accept.
	maxSize btcutil.Amount

	// maxCSV is the maximum CSV we will accept for our funds.
	maxCSV uint16

	// minReserve is the minimum reserve we require.
	minReserve btcutil.Amount

	// maxReserve is the maximum reserve we require.
	maxReserve btcutil.Amount

	// allowPush indicates whether we accept psuh amounts.
	allowPush bool

	// maxInFlightMaximum is the maximum size we allow for in flight.
	maxInFlightMaximum btcutil.Amount

	//  'max HTLC value in flight' the remote required is too small to be accepted.
	maxInFlightMinimum btcutil.Amount

	// 'max HTLCs in flight' value the remote required is too large to be accepted.
	maxHtlcNum uint16

	// 'max HTLCs in light' value the remote required is too small to be accepted.
	minHtlcNum uint16

	// maxConfs is the maximum nubmer of confirmations we will allow.
	maxConfs uint32
}

// Decode deserializes the error's metadata from the passed io.Reader.
//
// Note: part of the ErrorMetadata interface
func (c *ReservationMetadata) Decode(r io.Reader) error {
	return ReadElements(r, &c.chainHash)
}

// Encode serializes the error's metadata into the passed io.Writer.
//
// Note: part of the ErrorMetadata interface
func (c *ReservationMetadata) Encode(w io.Writer) error {
	// TODO(carla): serialization code
	return WriteElements(w, c.chainHash)
}

// Error returns the string representation of funding metadata.
func (c *ReservationMetadata) Error() string {
	return fmt.Sprintf("chainhash=%v ", c.chainHash)
}
