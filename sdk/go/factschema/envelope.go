// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import "time"

// Envelope is the canonical contracts-module fact envelope described in
// Contract System v1 §3.1 (docs/internal/design/contract-system-v1.md). It
// carries the frozen envelope fields every fact kind shares, plus the raw,
// not-yet-decoded Payload that a kind-specific typed struct is decoded from
// via the seam in decode.go.
//
// Envelope unification is explicitly out of scope for this scaffold. Eshu
// today has three separate envelope definitions — this one,
// go/internal/facts.Envelope, and sdk/go/collector.Fact — that describe the
// same wire concept with different field names and Go representations.
// Generating or aliasing all three from one source is documented follow-up
// work; see the "Envelope unification" section of this package's README.md
// and design §3.1.
type Envelope struct {
	// FactKind is the namespaced fact kind identifier (for example
	// "aws_resource"). It selects which decode function and JSON Schema
	// apply to Payload.
	FactKind string `json:"fact_kind"`

	// SchemaVersion is the semver payload schema version for FactKind. The
	// major component selects which typed struct and decode path in
	// decode.go handles Payload; a major this contracts module does not
	// recognize is a classified decode error, not a best-effort decode.
	SchemaVersion string `json:"schema_version"`

	// StableFactKey is the durable idempotency key for this fact within one
	// (ScopeID, GenerationID). Repeated delivery of the same StableFactKey
	// is expected under the platform's at-least-once delivery guarantee.
	StableFactKey string `json:"stable_fact_key"`

	// ScopeID identifies the source shard (for example a repository or
	// cloud account) this fact was observed in.
	ScopeID string `json:"scope_id"`

	// GenerationID identifies the source observation run that produced this
	// fact. Facts from a superseded generation stop being read by the
	// reducer; they are not necessarily deleted immediately.
	GenerationID string `json:"generation_id"`

	// CollectorKind identifies which collector implementation emitted this
	// fact.
	CollectorKind string `json:"collector_kind"`

	// SourceConfidence describes how the collector learned this fact
	// (observed, reported, inferred, or derived).
	SourceConfidence string `json:"source_confidence"`

	// ObservedAt is the source-reported observation timestamp for this
	// fact.
	ObservedAt time.Time `json:"observed_at"`

	// IsTombstone marks this fact as a deletion of a previously observed
	// StableFactKey rather than a new or updated observation.
	IsTombstone bool `json:"is_tombstone"`

	// SourceRef identifies the source-local record that produced this fact,
	// for provenance and debugging.
	SourceRef string `json:"source_ref"`

	// Payload is the raw, not-yet-decoded fact body. Callers must not read
	// Payload keys directly for a fact kind that has a typed struct in this
	// module; decode it via the kind-keyed seam in decode.go instead, which
	// validates required fields and returns a classified error rather than
	// a zero-value struct when the payload is malformed.
	Payload map[string]any `json:"payload"`
}
