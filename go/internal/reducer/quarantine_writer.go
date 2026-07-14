// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

// QuarantinedFactRecord is one durable row describing a fact that
// recordQuarantinedFacts (factschema_decode.go) quarantined as input_invalid
// during typed-payload decode. It carries exactly the fields the
// reducer_input_invalid_facts table persists (issue #4630): the malformed
// fact's identity, the reducer domain that observed it, the scope/generation
// it belongs to, and when the quarantine decision was made. This is the
// read-surface counterpart to the existing eshu_dp_reducer_input_invalid_facts_total
// counter and structured error log — a durable row an operator can query by
// scope/generation instead of only seeing an aggregate rate or a single log
// line.
type QuarantinedFactRecord struct {
	// FactID is the durable fact identifier of the malformed fact.
	FactID string
	// FactKind is the malformed fact's kind.
	FactKind string
	// MissingField is the required payload key that was absent or null.
	MissingField string
	// FailureClass is the decode classification (always input_invalid today;
	// carried as a string so a future non-input_invalid quarantine class does
	// not require a schema change).
	FailureClass string
	// Domain is the reducer domain that observed the quarantine.
	Domain string
	// ScopeID is the ingestion scope the malformed fact belongs to.
	ScopeID string
	// GenerationID is the scope generation the malformed fact belongs to.
	GenerationID string
	// DecidedAt is when the reducer quarantined the fact.
	DecidedAt time.Time
}

// QuarantinedFactWriter persists durable per-fact input_invalid quarantine
// rows (issue #4630). Implementations MUST be idempotent under reduction
// replay: a work item that quarantines the same fact/field/domain twice (a
// retry after a later failure in the same intent, or a replayed generation)
// must converge on one durable row rather than duplicating it, so a
// natural-key ON CONFLICT DO NOTHING upsert is the expected shape (mirrors
// the admission_decisions write pattern's idempotent-upsert requirement).
// The natural key includes Domain: two different reducer domains that
// independently quarantine the same fact/field (for example aws_resource
// decoded by both the AWS resource materialization domain and the
// relationship/IAM/security-group join-path domains) MUST produce two
// distinct durable rows, not one collapsed row, so a domain-filtered read
// preserves per-domain quarantine truth (codex review on PR #5252).
//
// A WriteQuarantinedFacts failure is NEVER allowed to fail the owning
// intent: this is a best-effort, operator-facing read-surface write, not a
// correctness-critical one. The single caller, persistQuarantinedFacts in
// factschema_decode.go, enforces this by logging and counting a write error
// without propagating it.
type QuarantinedFactWriter interface {
	WriteQuarantinedFacts(ctx context.Context, records []QuarantinedFactRecord) error
}

// quarantineWriterContextKey is the unexported context key carrying the
// optional QuarantinedFactWriter through Executor.Execute -> Handler.Handle
// -> recordQuarantinedFacts.
//
// Why context instead of a field on every one of the ~40 domain handler
// structs that call recordQuarantinedFacts: recordQuarantinedFacts is a
// single shared helper called from nearly every materialization/correlation
// handler in this package, each with its own struct and its own
// DefaultHandlers-sourced field set (mirroring the FactLoader/Instruments
// pattern). Threading a new optional dependency through every one of those
// structs and their defaults_additive_domains_*.go wiring sites would bloat
// ~40 files for what is a single, optional, best-effort side-write with no
// bearing on handler correctness or control flow. Service already carries
// exactly this kind of cross-cutting, non-control-flow infrastructure through
// context for the active trace span (s.Tracer.Start(ctx, ...) in
// executeWithTelemetry, service.go); WithQuarantineWriter follows the same,
// already-reviewed precedent. A nil writer (every test, and any deployment
// that has not wired Service.QuarantineWriter) makes both helpers a no-op —
// recordQuarantinedFacts' counter and structured log continue unchanged.
type quarantineWriterContextKey struct{}

// WithQuarantineWriter returns a context carrying writer for
// recordQuarantinedFacts to read via quarantineWriterFromContext. Service's
// executeWithTelemetry calls this once per claimed intent, before invoking
// Executor.Execute, so every handler's quarantined facts flow to the same
// durable writer without any handler-level wiring. Passing a nil writer
// returns ctx unchanged.
func WithQuarantineWriter(ctx context.Context, writer QuarantinedFactWriter) context.Context {
	if writer == nil {
		return ctx
	}
	return context.WithValue(ctx, quarantineWriterContextKey{}, writer)
}

// quarantineWriterFromContext returns the QuarantinedFactWriter stashed by
// WithQuarantineWriter, or nil when none was set (the default: persistence is
// a no-op, the counter and structured log still emit).
func quarantineWriterFromContext(ctx context.Context) QuarantinedFactWriter {
	writer, _ := ctx.Value(quarantineWriterContextKey{}).(QuarantinedFactWriter)
	return writer
}
