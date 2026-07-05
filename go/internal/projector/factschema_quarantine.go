// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// This file is the GENERIC, family-neutral projector-side counterpart to the
// reducer's factschema_decode.go quarantine apparatus (Contract System v1
// §3.2). It introduces the typed-decode seam into the projector's canonical
// extractors for the first time. It is deliberately NOT oci-specific: the
// terraform_state canonical extractor (and any future typed projector family)
// reuses partitionProjectorDecodeFailures / recordQuarantinedFacts / factschemaEnvelope
// verbatim, so the per-fact fault-isolation contract is defined once.
//
// Why per-fact quarantine, not a whole-work-item fail: the projector's
// buildCanonicalMaterialization builds one repository generation's entire graph
// (files, entities, terraform, packages, oci) and returns no error per fact. A
// malformed oci fact must NOT fail that whole build — that would drop every
// valid file/entity/package for the repo, an accuracy regression far worse than
// the one malformed fact. Instead the extractor SKIPS the one bad fact
// (recording it as a visible input_invalid dead-letter) and keeps projecting
// every valid fact, OCI and non-OCI. This mirrors the reducer's proven
// per-fact model.

// projectorDecodeError wraps a classified *factschema.DecodeError so the
// projector's quarantine path can read the missing field and classification.
// Unlike the reducer's factDecodeError it is not wired into a durable
// queue-failure interface: a quarantined projector fact is recorded as a
// per-fact metric + structured log and skipped, never surfaced as a whole
// work-item failure_class (the projector's canonical build is not per-fact on
// the queue path). The classification value is byte-equal to
// FailureClassInputInvalid ("input_invalid") by the by-value contract Contract
// System v1 mandates (the contracts module cannot import go/internal, so the
// projector maps the classification by value).
type projectorDecodeError struct {
	// factKind is the fact kind that failed to decode, for the error message.
	factKind string
	// err is the underlying classified *factschema.DecodeError.
	err *factschema.DecodeError
}

// Error implements the error interface, naming the fact kind and the underlying
// classified decode failure.
func (e *projectorDecodeError) Error() string {
	return fmt.Sprintf("decode %s payload: %s", e.factKind, e.err.Error())
}

// Unwrap exposes the underlying *factschema.DecodeError so errors.As/errors.Is
// can reach it (and its ErrUnsupportedSchemaMajor sentinel).
func (e *projectorDecodeError) Unwrap() error {
	return e.err
}

// newProjectorDecodeError wraps a decode error returned by a factschema Decode*
// function into the projector's classified decode failure. It expects a
// *factschema.DecodeError (the only error the Decode* seam returns); a
// different error is still wrapped with the input_invalid classification so the
// fact is quarantined rather than mistaken for a valid decode.
func newProjectorDecodeError(factKind string, err error) *projectorDecodeError {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return &projectorDecodeError{factKind: factKind, err: decodeErr}
	}
	return &projectorDecodeError{
		factKind: factKind,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Err:            err,
		},
	}
}

// quarantinedFact records one fact a projector canonical extractor could not
// decode because its payload was missing a required field (an input_invalid
// decode failure). The extractor skips it (still projecting every valid fact in
// the batch) and returns it so the caller can emit a visible, per-fact
// dead-letter — a metric increment plus a structured error log naming the fact
// and field — rather than silently dropping the fact as the pre-typing
// extractor did.
type quarantinedFact struct {
	// factID is the durable fact identifier of the malformed fact, so an
	// operator can locate the exact fact in fact_records.
	factID string
	// factKind is the malformed fact's kind.
	factKind string
	// field is the required payload key that was absent or null.
	field string
	// classification is the decode classification (always input_invalid for a
	// quarantined fact; a non-input_invalid error is returned fatally by
	// partitionProjectorDecodeFailures).
	classification string
}

// partitionProjectorDecodeFailures is the single classifier every projector canonical
// extractor routes a decode error through. It enforces the projector fault-
// isolation contract, mirroring the reducer's partitionDecodeFailures:
//
//   - A *projectorDecodeError with ClassificationInputInvalid (a missing/null
//     required field) is QUARANTINABLE: it returns a quarantinedFact and true,
//     so the extractor skips that one fact and keeps projecting the rest. The
//     fact is non-retryable — replaying it unchanged can never succeed — so
//     dropping it and recording it as a visible dead-letter is correct, not a
//     silent swallow.
//   - ANY OTHER error (including an unsupported schema major, which is version
//     skew rather than a malformed individual payload) is FATAL: it returns
//     (zero, false, err), so the caller propagates it. An unsupported schema
//     major must fail loudly rather than be quarantined per-fact, because it can
//     succeed once the projector supports the major.
//
// Routing every decode error through this ONE helper stops a future family
// migration from inline `if err != nil { skip }` swallowing a real error — the
// "swallow failures" sin the Life Motto forbids.
func partitionProjectorDecodeFailures(env facts.Envelope, err error) (quarantinedFact, bool, error) {
	var decodeErr *projectorDecodeError
	if errors.As(err, &decodeErr) &&
		decodeErr.err.Classification == factschema.ClassificationInputInvalid &&
		!errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		return quarantinedFact{
			factID:         env.FactID,
			factKind:       env.FactKind,
			field:          decodeErr.err.Field,
			classification: decodeErr.err.Classification,
		}, true, nil
	}
	return quarantinedFact{}, false, err
}

// recordProjectorQuarantinedFacts emits the visible, operator-diagnosable
// dead-letter for each fact a projector canonical extractor quarantined during
// decode: it increments the eshu_dp_projector_input_invalid_facts_total counter
// (labeled by stage and fact_kind) and logs one structured error per fact
// naming the fact id and the missing required field, then returns the count.
// This is the difference from the pre-typing silent skip — a quarantined fact
// is a first-class, dashboard-visible, log-searchable event.
//
// It is safe to call with a nil instruments pointer (the counter is skipped,
// the logs still emit) and with an empty slice (a no-op returning 0). stage is
// the bounded projector extractor label (for example "oci_registry_canonical").
func recordProjectorQuarantinedFacts(
	ctx context.Context,
	instruments *telemetry.Instruments,
	stage string,
	scopeID, generationID string,
	quarantined []quarantinedFact,
) int {
	for _, q := range quarantined {
		if instruments != nil && instruments.ProjectorInputInvalidFacts != nil {
			instruments.ProjectorInputInvalidFacts.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrStage(stage),
				telemetry.AttrFactKind(q.factKind),
			))
		}
		slog.ErrorContext(
			ctx, "projector input_invalid fact quarantined",
			slog.String("stage", stage),
			log.ScopeID(scopeID),
			log.GenerationID(generationID),
			slog.String("fact_id", q.factID),
			slog.String("fact_kind", q.factKind),
			slog.String("missing_field", q.field),
			slog.String("failure_class", q.classification),
		)
	}
	return len(quarantined)
}

// factschemaEnvelope adapts a go/internal/facts.Envelope to the contracts-module
// factschema.Envelope the Decode* seam accepts. Only the fields the decode seam
// reads (FactKind, SchemaVersion, Payload) are populated; envelope unification
// is documented follow-up work (design §3.1), so the adapter stays explicit and
// narrow rather than aliasing the two envelope types. An empty SchemaVersion is
// normalized to the current major-1 schema version, matching the reducer's
// factschemaEnvelope: every oci emitter stamps a concrete "1.0.0" version and
// the projector's schema-version admission gates on it upstream, so a
// version-less fact does not occur on the production path. A present but
// unsupported major still dead-letters through the Decode* seam's default
// branch.
func factschemaEnvelope(env facts.Envelope) factschema.Envelope {
	schemaVersion := env.SchemaVersion
	if schemaVersion == "" {
		schemaVersion = projectorDefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      env.FactKind,
		SchemaVersion: schemaVersion,
		Payload:       env.Payload,
	}
}

// projectorDefaultSchemaMajorVersion is the schema version the projector assumes
// when an envelope carries none. It is a major-1 version because every migrated
// fact kind is at schema major 1 today; the Decode seam dispatches on the major
// component only.
const projectorDefaultSchemaMajorVersion = "1.0.0"
