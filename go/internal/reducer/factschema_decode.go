// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// factDecodeError wraps a classified *factschema.DecodeError so the reducer's
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
type factDecodeError struct {
	// factKind is the fact kind that failed to decode, for the error message.
	factKind string
	// err is the underlying classified *factschema.DecodeError.
	err *factschema.DecodeError
}

// Error implements the error interface, naming the fact kind and the underlying
// classified decode failure.
func (e *factDecodeError) Error() string {
	return fmt.Sprintf("decode %s payload: %s", e.factKind, e.err.Error())
}

// Unwrap exposes the underlying *factschema.DecodeError so errors.As can reach
// it (and its ErrUnsupportedSchemaMajor sentinel).
func (e *factDecodeError) Unwrap() error {
	return e.err
}

// Retryable reports that a decode failure is terminal: replaying a fact with a
// missing or malformed required field can never succeed, so it must not re-enter
// the durable queue.
func (e *factDecodeError) Retryable() bool {
	return false
}

// FailureClass returns the durable failure_class the dead-letter row carries.
// It is the DecodeError's own classification value ("input_invalid"), so the
// reducer maps the contracts-module classification onto the queue's triage class
// by value without importing go/internal/projector.
func (e *factDecodeError) FailureClass() string {
	return e.err.Classification
}

// newFactDecodeError wraps a decode error returned by a factschema Decode*
// function into the reducer's self-classifying terminal failure. It expects a
// *factschema.DecodeError (the only error the Decode* seam returns); if a caller
// ever passes a different error it is wrapped with the input_invalid
// classification so the fact still dead-letters rather than being mistaken for a
// retryable projection bug.
func newFactDecodeError(factKind string, err error) *factDecodeError {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return &factDecodeError{factKind: factKind, err: decodeErr}
	}
	return &factDecodeError{
		factKind: factKind,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Err:            err,
		},
	}
}

// decodeAWSResource decodes one aws_resource envelope into the typed
// awsv1.Resource struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field or otherwise
// malformed. It is the single decode site for the aws_resource kind on the
// reducer side: every handler and join-index builder that consumes aws_resource
// facts decodes through here (or decodeAWSResourceEnvelopes), so a missing
// required field dead-letters as input_invalid exactly once per fact rather than
// silently becoming an empty-string graph identity.
func decodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Resource{}, newFactDecodeError(factschema.FactKindAWSResource, err)
	}
	return resource, nil
}

// decodeAWSRelationship decodes one aws_relationship envelope into the typed
// awsv1.Relationship struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region, relationship_type, source_resource_id,
// target_resource_id) or is otherwise malformed. It is the single decode site
// for the aws_relationship kind on the reducer side.
func decodeAWSRelationship(env facts.Envelope) (awsv1.Relationship, error) {
	relationship, err := factschema.DecodeAWSRelationship(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Relationship{}, newFactDecodeError(factschema.FactKindAWSRelationship, err)
	}
	return relationship, nil
}

// factschemaEnvelope adapts a go/internal/facts.Envelope to the contracts-module
// factschema.Envelope the Decode* seam accepts. Only the fields the decode seam
// reads (FactKind, SchemaVersion, Payload) are populated; envelope unification
// is documented follow-up work (design §3.1), so the adapter stays explicit and
// narrow rather than aliasing the two envelope types.
//
// An empty SchemaVersion is normalized to the current major-1 schema version.
// Every AWS/IAM/security-group emitter stamps a concrete "1.0.0" version and the
// projector's schema-version admission already gates on it upstream, so a
// version-less fact does not occur on the production path; the normalization
// exists only so the decode seam's major dispatch matches the pre-typing
// behavior, where the reducer read the payload without inspecting the version.
// A present but unsupported major (for example "2.0.0") is NOT normalized and
// still dead-letters through the Decode* seam's default branch.
func factschemaEnvelope(env facts.Envelope) factschema.Envelope {
	schemaVersion := env.SchemaVersion
	if schemaVersion == "" {
		schemaVersion = defaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      env.FactKind,
		SchemaVersion: schemaVersion,
		Payload:       env.Payload,
	}
}

// defaultSchemaMajorVersion is the schema version the reducer assumes when an
// envelope carries none. It is a major-1 version because every migrated fact
// kind is at schema major 1 today; the value's minor/patch are irrelevant since
// the Decode seam dispatches on the major component only.
const defaultSchemaMajorVersion = "1.0.0"
