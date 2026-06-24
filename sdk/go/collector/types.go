// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "time"

const (
	// ProtocolVersionV1Alpha1 is the first collector extension wire contract.
	ProtocolVersionV1Alpha1 = "collector-sdk/v1alpha1"
)

const (
	// SourceConfidenceObserved marks facts read directly from a source artifact.
	SourceConfidenceObserved SourceConfidence = "observed"
	// SourceConfidenceReported marks facts returned by an external system or API.
	SourceConfidenceReported SourceConfidence = "reported"
	// SourceConfidenceInferred marks facts concluded by correlating other evidence.
	SourceConfidenceInferred SourceConfidence = "inferred"
	// SourceConfidenceDerived marks facts materialized from existing Eshu facts.
	SourceConfidenceDerived SourceConfidence = "derived"
	// SourceConfidenceUnknown is compatibility-only and invalid for SDK output.
	SourceConfidenceUnknown SourceConfidence = "unknown"
)

const (
	// ResultComplete tells the host to validate facts and complete the claim.
	ResultComplete ResultState = "complete"
	// ResultUnchanged tells the host to complete the claim without fact writes.
	ResultUnchanged ResultState = "unchanged"
	// ResultPartial tells the host reachable evidence is useful but incomplete.
	ResultPartial ResultState = "partial"
	// ResultRetryable tells the host to release the claim for a bounded retry.
	ResultRetryable ResultState = "retryable"
	// ResultTerminal tells the host to terminal-fail the claim.
	ResultTerminal ResultState = "terminal"
)

const (
	// StatusProgress reports bounded in-flight progress.
	StatusProgress StatusClass = "progress"
	// StatusWarning reports a non-fatal partial or degraded outcome.
	StatusWarning StatusClass = "warning"
	// StatusFailure reports retryable or terminal failure metadata.
	StatusFailure StatusClass = "failure"
	// StatusComplete reports successful claim completion metadata.
	StatusComplete StatusClass = "complete"
)

// SourceConfidence describes how the extension learned a fact.
type SourceConfidence string

// ResultState describes the core claim action requested by an extension result.
type ResultState string

// StatusClass describes a bounded status record.
type StatusClass string

// Contract declares the fact families and wire protocol a host accepts.
type Contract struct {
	ProtocolVersion string            `json:"protocol_version"`
	Facts           []FactDeclaration `json:"facts"`
}

// FactDeclaration describes one manifest-declared fact family.
type FactDeclaration struct {
	Kind             string             `json:"kind"`
	SchemaVersions   []string           `json:"schema_versions"`
	SourceConfidence []SourceConfidence `json:"source_confidence"`
	TombstoneAllowed bool               `json:"tombstone_allowed,omitempty"`
}

// Result is the top-level collector-sdk/v1alpha1 extension response.
type Result struct {
	ProtocolVersion string      `json:"protocol_version"`
	State           ResultState `json:"state"`
	Claim           Claim       `json:"claim"`
	Generation      Generation  `json:"generation"`
	Facts           []Fact      `json:"facts,omitempty"`
	Statuses        []Status    `json:"statuses,omitempty"`
}

// Claim is the bounded core-owned work item passed to an extension.
type Claim struct {
	ComponentID   string    `json:"component_id"`
	InstanceID    string    `json:"instance_id"`
	CollectorKind string    `json:"collector_kind"`
	SourceSystem  string    `json:"source_system"`
	Scope         Scope     `json:"scope"`
	SourceRunID   string    `json:"source_run_id"`
	GenerationID  string    `json:"generation_id"`
	WorkItemID    string    `json:"work_item_id"`
	FencingToken  string    `json:"fencing_token"`
	Attempt       int       `json:"attempt"`
	Deadline      time.Time `json:"deadline"`
	ConfigHandle  string    `json:"config_handle"`
}

// Scope identifies the source shard that the extension is allowed to observe.
type Scope struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// Generation identifies one source observation for the claimed scope.
type Generation struct {
	ID            string    `json:"id"`
	ObservedAt    time.Time `json:"observed_at"`
	FreshnessHint string    `json:"freshness_hint,omitempty"`
}

// Fact is the public fact envelope emitted by an out-of-tree collector.
type Fact struct {
	Kind             string           `json:"kind"`
	SchemaVersion    string           `json:"schema_version"`
	StableKey        string           `json:"stable_key"`
	SourceConfidence SourceConfidence `json:"source_confidence"`
	ObservedAt       time.Time        `json:"observed_at"`
	Tombstone        bool             `json:"tombstone,omitempty"`
	SourceRef        SourceRef        `json:"source_ref"`
	Payload          map[string]any   `json:"payload"`
	Redactions       []Redaction      `json:"redactions,omitempty"`
}

// SourceRef identifies the source-local record that produced one fact.
type SourceRef struct {
	SourceSystem string `json:"source_system"`
	ScopeID      string `json:"scope_id"`
	GenerationID string `json:"generation_id"`
	FactKey      string `json:"fact_key"`
	URI          string `json:"uri"`
	RecordID     string `json:"record_id"`
}

// Redaction records a field that was intentionally removed before emission.
type Redaction struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Status reports bounded progress, partial, retry, or terminal metadata.
type Status struct {
	Class             StatusClass `json:"class"`
	FailureClass      string      `json:"failure_class,omitempty"`
	RetryAfterSeconds int         `json:"retry_after_seconds,omitempty"`
	Partial           bool        `json:"partial,omitempty"`
	WarningCount      int         `json:"warning_count,omitempty"`
	FactCount         int         `json:"fact_count,omitempty"`
	SourceLatencyMS   int         `json:"source_latency_ms,omitempty"`
}

// ValidationReport summarizes accepted facts and safe duplicate/redaction counts.
type ValidationReport struct {
	FactCount      int
	DuplicateCount int
	RedactionCount int
	TombstoneCount int
	StatusCount    int
}
