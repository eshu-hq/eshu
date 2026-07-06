// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// TaintEvidence is the schema-version-1 typed payload for the
// "code_taint_evidence" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A code_taint_evidence fact is the git collector's per-finding emission
// (go/internal/collector/git_snapshot_taint_evidence.go
// taintEvidenceFactEnvelope) for one resolved intraprocedural value-flow
// finding: the collector has already resolved the finding's function to its
// graph Function entity uid before emission, dropping any finding whose
// function did not materialize as an entity. The reducer's postgres loader
// (go/internal/storage/postgres/code_taint_evidence_loader.go) projects this
// struct into a CodeTaintEvidenceInput row
// (go/internal/reducer/code_taint_evidence_materialization.go), which the
// materialization handler writes as a graph evidence node attached to that
// Function node — evidence, never canonical truth.
//
// FunctionUID is REQUIRED: it is the only field this migration promotes from
// the pre-Contract-System optional read, because it is the join key the graph
// write attaches the evidence node to (a taint finding with no function to
// attach to is not a projectable fact). Every other field is optional: the
// collector always emits FunctionName/RelativePath/Language/Kind/SinkKind/
// SourceKind/Binding/SourceLine/SinkLine/Confidence, but none of them are a
// join or attachment identity — a present-but-empty/zero value is a
// legitimate finding shape, not a malformed one.
type TaintEvidence struct {
	// FunctionUID is the graph Function entity uid this finding attaches to.
	// Required: taintEvidenceFactEnvelope always resolves and writes this
	// before emission (a finding whose function did not resolve to an entity
	// is dropped collector-side, never emitted with an empty uid), and the
	// reducer's graph write keys the evidence node's attachment on it.
	FunctionUID string `json:"function_uid"`

	// RelativePath is the file path (relative to the repository root) the
	// finding was observed in. Optional: always emitted, but read only for
	// evidence-row display, not join identity.
	RelativePath *string `json:"relative_path,omitempty"`

	// FunctionName is the finding's function name. Optional: always emitted,
	// read only for evidence-row display.
	FunctionName *string `json:"function_name,omitempty"`

	// Language is the parser-detected source language. Optional: always
	// emitted (taintEvidenceFactEnvelope writes it unconditionally, empty
	// string when the parser reported none).
	Language *string `json:"language,omitempty"`

	// Kind is the finding's taint-rule kind (for example "sql_injection").
	// Optional: always emitted.
	Kind *string `json:"kind,omitempty"`

	// SinkKind is the finding's sink classification. Optional: always
	// emitted.
	SinkKind *string `json:"sink_kind,omitempty"`

	// SourceKind is the finding's source classification. Optional: always
	// emitted.
	SourceKind *string `json:"source_kind,omitempty"`

	// Binding is the finding's variable/parameter binding description.
	// Optional: always emitted.
	Binding *string `json:"binding,omitempty"`

	// SourceLine is the 1-based line number of the finding's taint source.
	// Optional: always emitted (zero when the parser reported none).
	SourceLine *int `json:"source_line,omitempty"`

	// SinkLine is the 1-based line number of the finding's taint sink.
	// Optional: always emitted.
	SinkLine *int `json:"sink_line,omitempty"`

	// Confidence is the parser's confidence score for this finding (0.0-1.0).
	// Optional: always emitted (zero is a legitimate low-confidence
	// observation, not an absent value).
	Confidence *float64 `json:"confidence,omitempty"`

	// ClassContext is the receiver/class context of the finding's function,
	// when the function is a method. Optional: written only when
	// non-empty (taintEvidenceFactEnvelope's `if evidence.ClassContext != ""`
	// guard).
	ClassContext *string `json:"class_context,omitempty"`

	// SinkLabel is a human-readable label for the sink, when the parser
	// supplied one. Optional: written only when non-empty.
	SinkLabel *string `json:"sink_label,omitempty"`

	// SourceLabel is a human-readable label for the source, when the parser
	// supplied one. Optional: written only when non-empty.
	SourceLabel *string `json:"source_label,omitempty"`

	// GuardReason explains why a finding was reported despite an apparent
	// guard/sanitizer, when the parser supplied one. Optional: written only
	// when non-empty.
	GuardReason *string `json:"guard_reason,omitempty"`
}
