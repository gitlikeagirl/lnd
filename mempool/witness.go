package mempool

import (
	"bytes"
	"io"

	"github.com/btcsuite/btcd/wire"
)

// witnessMatcher is an interface implemented by objects that can be used to
// match a single element in a witness stack.
type witnessMatcher interface {
	match([]byte) (bool, error)
}

// witnessLengthMatcher matches an element in the witness by length, but does
// not attempt to match its value.
type witnessLengthMatcher struct {
	len      int
	callback func([]byte)
}

// match compares the witness element provided to our target length. If the
// length is a match, the length matcher provides its callback with the witness
// value so that it can be used.
func (w *witnessLengthMatcher) match(witness []byte) (bool, error) {
	if len(witness) != w.len {
		return false, nil
	}

	// Provide our callback with the value we have matched by length and
	// return true.
	w.callback(witness)
	return true, nil
}

// witnessScriptMatcher matches an element to a target bitcoin script.
type witnessScriptMatcher struct {
	// scriptElements is a set of templates that we match our script to.
	scriptElements []scriptMatcher
}

// match compares the witness provided with a set of elements that describe a
// bitcoin script template.
func (w *witnessScriptMatcher) match(witness []byte) (bool, error) {
	r := bytes.NewReader(witness)

	// Run through our set of script matchers, consuming the bytes in our
	// reader. If we do not get a match, we fail the match.
	for _, m := range w.scriptElements {
		ok, err := m.match(r)
		switch err {
		// If we get an EOF error, we ran out of bytes in our script so
		// we can't possibly match it.
		case io.EOF:
			return false, nil

		// If we have no error, fallthrough to check our match.
		case nil:

		// If our error is a non-nil error that is not an EOF, we return
		// it.
		default:
			return false, err
		}

		// If we did not successfully match this item, return false.
		if !ok {
			return false, nil
		}
	}

	// Return true if there are no bytes left in our script and we have
	// matched everything.
	return r.Len() == 0, nil
}

// match attempts to match a set of template matchers with the witness provided.
// The index of a matcher in the template slice provided indicates the index
// in the witness that it will be matched against. This function allows nil
// elements in the template slice; these values are interpreted as not needing
// to validate that element in the witness at all.
func match(template []witnessMatcher, witness wire.TxWitness) (bool, error) {
	// If the witness provided does not have the same number of elements
	// as our template, it cannot match.
	if len(witness) != len(template) {
		return false, nil
	}

	// Run through each matcher and test that our witness element at the
	// same index is a match. If any element does not match, we can fail
	// the match.
	for i, tmpl := range template {
		// If we have a nil entry in our set of templates, we do not
		// need to match this index of the witness.
		if tmpl == nil {
			continue
		}

		// Attempt to match the corresponding index in the witness.
		ok, err := tmpl.match(witness[i])
		if err != nil {
			return false, err
		}

		if !ok {
			return false, nil
		}
	}

	return true, nil
}
