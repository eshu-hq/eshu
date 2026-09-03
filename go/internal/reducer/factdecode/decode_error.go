// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factdecode

import (
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// FactDecodeError wraps a classified *factschema.DecodeError so the reducer's
// durable-queue failure path treats a malformed payload as a terminal,
// operator-facing dead letter rather than a retry or a silent zero value.
//
// It self-classifies through the same interface the Postgres queue reads
// (queueFailureMetadata via errors.As): FailureClass returns the DecodeError's
// classification string — "input_invalid" for a missing/null required field —
// which is byte-equal to projector.TriageClassInputInvalid and
// factschema.ClassificationInputInvalid by the by-value contract Contract System
// v1 mandates (the contracts module cannot import go/internal, so the reducer
// maps the classification by value). Retryable returns false because a missing
// required field can never succeed on replay unchanged; the intent must
// dead-letter, not loop.
type FactDecodeError struct {
	// factKind is the fact kind that failed to decode, for the error message.
	factKind string
	// err is the underlying classified *factschema.DecodeError.
	err *factschema.DecodeError
}

// Error implements the error interface, naming the fact kind and the underlying
// classified decode failure.
func (e *FactDecodeError) Error() string {
	return fmt.Sprintf("decode %s payload: %s", e.factKind, e.err.Error())
}

// Unwrap exposes the underlying *factschema.DecodeError so errors.As can reach
// it (and its ErrUnsupportedSchemaMajor sentinel).
func (e *FactDecodeError) Unwrap() error {
	return e.err
}

// Retryable reports that a decode failure is terminal: replaying a fact with a
// missing or malformed required field can never succeed, so it must not re-enter
// the durable queue.
func (e *FactDecodeError) Retryable() bool {
	return false
}

// FailureClass returns the durable failure_class the dead-letter row carries.
// It is the DecodeError's own classification value ("input_invalid"), so the
// reducer maps the contracts-module classification onto the queue's triage class
// by value without importing go/internal/projector.
func (e *FactDecodeError) FailureClass() string {
	return e.err.Classification
}

// NewFactDecodeError wraps a decode error returned by a factschema Decode*
// function into the reducer's self-classifying terminal failure. It expects a
// *factschema.DecodeError (the only error the Decode* seam returns); if a caller
// ever passes a different error it is wrapped with the input_invalid
// classification so the fact still dead-letters rather than being mistaken for a
// retryable projection bug.
func NewFactDecodeError(factKind string, err error) *FactDecodeError {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return &FactDecodeError{factKind: factKind, err: decodeErr}
	}
	return &FactDecodeError{
		factKind: factKind,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Err:            err,
		},
	}
}

// QuarantinedFact records one fact a batch extractor could not decode because
// its payload was missing a required field (an input_invalid decode failure).
// The extractor skips it (still projecting every valid fact in the batch) and
// returns it so the handler can emit a visible, per-fact dead-letter — a metric
// increment plus a structured error log naming the fact and field — rather than
// failing the whole intent or silently dropping the fact.
type QuarantinedFact struct {
	// FactID is the durable fact identifier of the malformed fact, so an
	// operator can locate the exact fact in fact_records.
	FactID string
	// FactKind is the malformed fact's kind.
	FactKind string
	// Field is the required payload key that was absent or null.
	Field string
	// Classification is the decode classification (always input_invalid for a
	// quarantined fact; a non-input_invalid error is never quarantined — it is
	// returned fatally by PartitionDecodeFailures).
	Classification string
}

// PartitionDecodeFailures is the single classifier every batch extractor routes
// a decode error through. It enforces the reducer fault-isolation contract:
//
//   - A *FactDecodeError with ClassificationInputInvalid (a missing/null
//     required field) is QUARANTINABLE: it returns a QuarantinedFact and true,
//     so the extractor skips that one fact and keeps projecting the rest. The
//     fact is non-retryable — replaying it unchanged can never succeed — so
//     dropping it from the batch and recording it as a visible dead-letter is
//     correct, not a silent swallow.
//   - ANY OTHER error (a transient fact-load EOF, a graph-write failure, an
//     unsupported schema major, a projection bug) is FATAL: it returns
//     (zero, false, err), so the extractor propagates it and the handler fails
//     the whole intent through WorkSink.Fail, which triages it correctly
//     (retry_exhausted / dependency_unavailable / projection_bug / …).
//
// Routing every decode error through this ONE helper is what stops a future
// family-migration copy from inline `if err != nil { skip }` swallowing a
// transient loader or graph error — the "swallow failures" sin the Life Motto
// forbids. TestPartitionDecodeFailures locks the boundary.
func PartitionDecodeFailures(env facts.Envelope, err error) (QuarantinedFact, bool, error) {
	var decodeErr *FactDecodeError
	// An unsupported schema major is version skew, not a malformed individual
	// payload: the contracts module currently labels it input_invalid, but it
	// must fail the whole work item for durable triage (it can succeed once the
	// reducer supports the major), never be quarantined and skipped per-fact.
	// Excluding the sentinel keeps this function matching its documented contract
	// above, where an unsupported schema major is listed as fatal.
	if errors.As(err, &decodeErr) &&
		decodeErr.err.Classification == factschema.ClassificationInputInvalid &&
		!errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		return QuarantinedFact{
			FactID:         env.FactID,
			FactKind:       env.FactKind,
			Field:          decodeErr.err.Field,
			Classification: decodeErr.err.Classification,
		}, true, nil
	}
	return QuarantinedFact{}, false, err
}

// attributeShapeError is the family-agnostic contract behind an
// attribute-shape failure (issue #6358). Any family's typed-attribute Decode*
// error earns field extraction by reporting its failing attribute path; the
// adapters below match it with errors.As, so this package never names a
// single family's concrete error type and carries no per-family import.
type attributeShapeError interface {
	error
	AttributeShapeField() string
}

// attributeShapeField extracts the failing attribute path from err: the
// contract's AttributeShapeField when err (or anything it wraps) implements
// it, or err.Error() as a fallback so the field text is preserved even if a
// future caller passes a non-contract error value.
func attributeShapeField(err error) string {
	field := err.Error()
	var shapeErr attributeShapeError
	if errors.As(err, &shapeErr) {
		field = shapeErr.AttributeShapeField()
	}
	return field
}

// QuarantinedAttributeShapeFact builds a QuarantinedFact for a service-specific
// attribute field that failed one of a family's bounded typed-attribute Decode*
// functions (issue #4631) — for example
// awsv1.DecodeResourceEC2VolumeAttributes rejecting a present-but-non-bool
// "encrypted" value. Any error behind the attributeShapeError contract is
// accepted, not only the AWS shape. This is distinct from
// PartitionDecodeFailures, which
// classifies a whole-envelope *FactDecodeError: an attribute-shape failure
// happens AFTER the envelope's identity fields already decoded successfully,
// so the envelope itself is not malformed, only one service-specific field
// inside it. Routing it through the same QuarantinedFact dead-letter surface
// keeps the failure visible (counted + logged) instead of silently
// substituting a zero/empty derived value, matching the accuracy contract a
// missing required field already gets.
func QuarantinedAttributeShapeFact(env facts.Envelope, err error) QuarantinedFact {
	return QuarantinedFact{
		FactID:         env.FactID,
		FactKind:       env.FactKind,
		Field:          attributeShapeField(err),
		Classification: factschema.ClassificationInputInvalid,
	}
}

// AttributeShapeAsFactDecodeError adapts a service-specific attribute decode
// error (any error behind the attributeShapeError contract, e.g. an
// *awsv1.AttributeShapeError from one of the bounded typed-attribute Decode*
// functions, issue #4631) into a *FactDecodeError so a caller that
// already routes its envelope decode errors through PartitionDecodeFailures
// (for example cloudResourceNodeRow) can propagate an attribute-shape failure
// through that same, unmodified quarantine path. The resulting
// *FactDecodeError classifies as input_invalid and names the specific
// attribute field (not just the envelope) in its Field, so the quarantine's
// operator-facing "missing_field" log carries the precise failing path.
func AttributeShapeAsFactDecodeError(factKind string, err error) error {
	field := attributeShapeField(err)
	return &FactDecodeError{
		factKind: factKind,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          field,
			Err:            err,
		},
	}
}

// InputInvalidSubSignals returns the Result.SubSignals map carrying the count of
// facts quarantined as input_invalid during this intent, or nil when none were.
// Returning nil for the zero case keeps the service log line from emitting a
// noise "input_invalid_facts=0" signal on the overwhelming majority of intents
// that decode cleanly; a non-zero count is the operator's per-intent flag that
// this intent skipped malformed facts (each one also on the counter + error log).
func InputInvalidSubSignals(count int) map[string]float64 {
	if count == 0 {
		return nil
	}
	return map[string]float64{"input_invalid_facts": float64(count)}
}
