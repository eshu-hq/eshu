// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querydecode

import (
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// Error wraps a classified *factschema.DecodeError so a query-layer
// caller can read the missing field and classification without importing the
// reducer/projector's dead-letter triage types. The classification value is
// byte-equal to "input_invalid" by the by-value contract Contract System v1
// mandates (the contracts module cannot import go/internal, and this package
// does not import the reducer/projector's triage classes either).
type Error struct {
	// FactKind is the fact kind that failed to decode.
	FactKind string
	// FactID is the durable fact identifier of the malformed fact, so an
	// operator can locate the exact row in fact_records.
	FactID string
	// Field is the required payload key that was absent or null. Empty when
	// the failure is not attributable to a single field (for example an
	// unsupported schema major).
	Field string
	// Classification is the decode classification, always
	// factschema.ClassificationInputInvalid for a field-attributable failure.
	Classification string
	// err is the underlying classified *factschema.DecodeError.
	err *factschema.DecodeError
}

// Error implements the error interface, naming the fact id, fact kind, and the
// underlying classified decode failure.
func (e *Error) Error() string {
	// err is unexported, so an importer building this from the exported fields
	// alone leaves it nil. New always sets it, but the type is exported for
	// root's alias, and a panic here would surface as a 500 on a read path
	// whose whole job is to degrade one malformed fact gracefully.
	if e.err == nil {
		return fmt.Sprintf("decode %s fact %s: no underlying decode error", e.FactKind, e.FactID)
	}
	return fmt.Sprintf("decode %s fact %s: %s", e.FactKind, e.FactID, e.err.Error())
}

// Unwrap exposes the underlying *factschema.DecodeError so errors.As/errors.Is
// can reach it (and its ErrUnsupportedSchemaMajor sentinel).
func (e *Error) Unwrap() error {
	// Return an untyped nil rather than a nil *factschema.DecodeError. A typed
	// nil is non-nil in an interface, so errors.Is would keep walking into it
	// and callers checking Unwrap() != nil would get the wrong answer.
	if e.err == nil {
		return nil
	}
	return e.err
}

// New wraps a decode error returned by a factschema Decode*
// function into the query layer's classified decode failure, attributing it to
// factID for operator diagnosis. It expects a *factschema.DecodeError (the
// only error the Decode* seam returns); a different error is still wrapped
// with the input_invalid classification so the caller treats it as
// non-retryable rather than mistaking it for a successful decode.
func New(factKind, factID string, err error) *Error {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return &Error{
			FactKind:       factKind,
			FactID:         factID,
			Field:          decodeErr.Field,
			Classification: decodeErr.Classification,
			err:            decodeErr,
		}
	}
	return &Error{
		FactKind:       factKind,
		FactID:         factID,
		Classification: factschema.ClassificationInputInvalid,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Err:            err,
		},
	}
}
