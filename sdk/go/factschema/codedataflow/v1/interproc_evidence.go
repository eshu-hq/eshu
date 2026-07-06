// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// InterprocEvidence is the schema-version-1 typed payload for the
// "code_interproc_evidence" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A code_interproc_evidence fact is the git collector's per-finding emission
// (go/internal/collector/git_snapshot_interproc_evidence.go
// interprocEvidenceFactEnvelope) for one resolved cross-function value-flow
// finding: the collector has already resolved BOTH the source and sink
// functions to their graph Function entity uids before emission, dropping any
// finding whose either endpoint did not resolve. The reducer's postgres
// loader (go/internal/storage/postgres/code_interproc_evidence_loader.go)
// projects this struct into a CodeInterprocEvidenceInput row
// (go/internal/reducer/code_interproc_evidence_materialization.go,
// code_interproc_evidence_rows.go), which the materialization handler writes
// as a TAINT_FLOWS_TO edge between the two Function nodes — evidence, never
// canonical truth.
//
// SourceFunctionUID and SinkFunctionUID are REQUIRED: they are the edge's two
// endpoints, and ExtractCodeInterprocEvidenceRows already drops any input
// missing either uid before drawing an edge — promoting them to required here
// makes that drop an explicit, operator-visible input_invalid dead-letter
// instead of a silent skip. Every other field is optional: always emitted but
// read only for edge-row display/provenance, never join identity.
type InterprocEvidence struct {
	// SourceFunctionUID is the graph Function entity uid of the finding's
	// taint source function. Required: interprocEvidenceFactEnvelope always
	// resolves and writes this before emission (an unresolved source endpoint
	// is dropped collector-side), and it is one of the TAINT_FLOWS_TO edge's
	// two endpoints.
	SourceFunctionUID string `json:"source_function_uid"`

	// SinkFunctionUID is the graph Function entity uid of the finding's taint
	// sink function. Required, same rationale as SourceFunctionUID.
	SinkFunctionUID string `json:"sink_function_uid"`

	// RelativePath is the file path (relative to the repository root) the
	// finding was observed in. Optional: always emitted, read only for
	// evidence-row display.
	RelativePath *string `json:"relative_path,omitempty"`

	// SourceFunctionName is the source function's name. Optional: always
	// emitted, read only for evidence-row display.
	SourceFunctionName *string `json:"source_function_name,omitempty"`

	// SinkFunctionName is the sink function's name. Optional: always emitted.
	SinkFunctionName *string `json:"sink_function_name,omitempty"`

	// Language is the parser-detected source language. Optional: always
	// emitted.
	Language *string `json:"language,omitempty"`

	// SinkKind is the finding's sink classification. Optional: always
	// emitted.
	SinkKind *string `json:"sink_kind,omitempty"`

	// SourceKind is the finding's source classification. Optional: always
	// emitted.
	SourceKind *string `json:"source_kind,omitempty"`

	// Confidence is the parser's confidence score for this finding (0.0-1.0).
	// Optional: always emitted (zero is a legitimate low-confidence
	// observation).
	Confidence *float64 `json:"confidence,omitempty"`

	// Cloud reports whether the finding's flow crosses a cloud/runtime
	// boundary. Optional: interprocEvidenceFactEnvelope writes this key only
	// when true (`if evidence.Cloud { payload["cloud"] = true }`), so absent
	// means false, not unknown.
	Cloud *bool `json:"cloud,omitempty"`

	// WhyTrail is the parser's step-by-step explanation of the flow from
	// source to sink. Optional and left UNTYPED (a slice of open maps, each
	// carrying role/function_id/function_name/function_uid/slot_kind/
	// slot_index/slot_name keys per interprocWhyTrailFromFinding): this is a
	// read-and-forward query-layer payload with no reducer field-level
	// consumer, matching codegraphv1.File.ParsedFileData's opacity
	// precedent. Written only when the parser supplied a non-empty trail.
	WhyTrail []map[string]any `json:"why_trail,omitempty"`

	// WhyTrailTruncated reports whether WhyTrail was truncated by the
	// parser's step-count limit. Optional: written only when true.
	WhyTrailTruncated *bool `json:"why_trail_truncated,omitempty"`
}
