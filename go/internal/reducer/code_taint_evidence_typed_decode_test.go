// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDecodeCodeTaintEvidenceInputMapsAllFields proves the fact payload maps
// to a reducer input through the typed contracts seam, including the JSONB
// float64-to-int coercion for line numbers (handled by the typed decode's
// *int fields rather than an ad hoc numeric-type switch). This moved from
// go/internal/storage/postgres/code_taint_evidence_loader_test.go when the
// field mapping moved into this package (Contract System v1 Wave 4f S2,
// issue #4754).
func TestDecodeCodeTaintEvidenceInputMapsAllFields(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeTaintEvidenceFactKind,
		Payload: map[string]any{
			"function_uid":  "func-handle",
			"function_name": "handle",
			"relative_path": "src/handler.go",
			"language":      "go",
			"kind":          "TAINTED",
			"sink_kind":     "sql",
			"source_kind":   "http_request",
			"binding":       "q",
			// JSONB numeric scan yields float64.
			"source_line":   float64(4),
			"sink_line":     float64(5),
			"confidence":    0.8,
			"class_context": "Repo",
			"sink_label":    "Query",
			"guard_reason":  "allowed",
		},
	}

	got, err := decodeCodeTaintEvidenceInput(envelope)
	if err != nil {
		t.Fatalf("decodeCodeTaintEvidenceInput error = %v, want nil", err)
	}
	if got.FunctionUID != "func-handle" || got.FunctionName != "handle" || got.RelativePath != "src/handler.go" {
		t.Fatalf("identity fields not mapped: %+v", got)
	}
	if got.Kind != "TAINTED" || got.SinkKind != "sql" || got.SourceKind != "http_request" || got.Binding != "q" {
		t.Fatalf("finding fields not mapped: %+v", got)
	}
	if got.SourceLine != 4 || got.SinkLine != 5 {
		t.Fatalf("line numbers not coerced from float64: %+v", got)
	}
	if got.Confidence != 0.8 || got.ClassContext != "Repo" || got.SinkLabel != "Query" || got.GuardReason != "allowed" {
		t.Fatalf("provenance fields not mapped: %+v", got)
	}
}

// TestDecodeCodeTaintEvidenceInputMissingFunctionUIDReturnsError proves the
// core deliverable of epic #4566 §1: a code_taint_evidence fact missing its
// required function_uid RETURNS a decode error (which the handler routes to an
// input_invalid dead-letter), rather than swallowing it into a zero-value
// input. The end-to-end production-path proof that the counter fires lives in
// TestCodeTaintEvidenceHandlerQuarantinesMalformedFact.
func TestDecodeCodeTaintEvidenceInputMissingFunctionUIDReturnsError(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeTaintEvidenceFactKind,
		Payload: map[string]any{
			"relative_path": "src/handler.go",
		},
	}
	if _, err := decodeCodeTaintEvidenceInput(envelope); err == nil {
		t.Fatal("decodeCodeTaintEvidenceInput(missing function_uid) error = nil, want a classified input_invalid error")
	}
}

// TestDecodeCodeTaintEvidenceInputTrimsWhitespace proves the padded-value
// regression this migration must not introduce: the pre-Contract-System
// payloadString helper TrimSpace'd every string field it read, including
// function_uid, so a padded value never reached the graph node attachment key.
func TestDecodeCodeTaintEvidenceInputTrimsWhitespace(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeTaintEvidenceFactKind,
		Payload: map[string]any{
			"function_uid":  "  uid:padded  ",
			"function_name": "  handle  ",
			"relative_path": "  src/handler.go  ",
		},
	}

	got, err := decodeCodeTaintEvidenceInput(envelope)
	if err != nil {
		t.Fatalf("decodeCodeTaintEvidenceInput error = %v, want nil", err)
	}
	if got.FunctionUID != "uid:padded" {
		t.Fatalf("FunctionUID = %q, want trimmed uid:padded", got.FunctionUID)
	}
	if got.FunctionName != "handle" || got.RelativePath != "src/handler.go" {
		t.Fatalf("optional string fields not trimmed: FunctionName=%q RelativePath=%q", got.FunctionName, got.RelativePath)
	}
}

// TestDecodeCodeInterprocEvidenceInputTrimsWhitespace mirrors
// TestDecodeCodeTaintEvidenceInputTrimsWhitespace for the interproc family's
// two edge-endpoint uids.
func TestDecodeCodeInterprocEvidenceInputTrimsWhitespace(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeInterprocEvidenceFactKind,
		Payload: map[string]any{
			"source_function_uid": "  uid:source-padded  ",
			"sink_function_uid":   "  uid:sink-padded  ",
		},
	}

	got, err := decodeCodeInterprocEvidenceInput(envelope)
	if err != nil {
		t.Fatalf("decodeCodeInterprocEvidenceInput error = %v, want nil", err)
	}
	if got.SourceFunctionUID != "uid:source-padded" || got.SinkFunctionUID != "uid:sink-padded" {
		t.Fatalf("endpoint uids not trimmed: SourceFunctionUID=%q SinkFunctionUID=%q", got.SourceFunctionUID, got.SinkFunctionUID)
	}
}

// TestDecodeCodeInterprocEvidenceInputMapsAllFields proves the cross-function
// fact payload maps to a reducer input, including the JSONB float64
// confidence and the bool cloud flag.
func TestDecodeCodeInterprocEvidenceInputMapsAllFields(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeInterprocEvidenceFactKind,
		Payload: map[string]any{
			"source_function_uid":  "func-source",
			"sink_function_uid":    "func-sink",
			"relative_path":        "src/handler.go",
			"source_function_name": "readRequest",
			"sink_function_name":   "execQuery",
			"language":             "go",
			"sink_kind":            "sql",
			"source_kind":          "http_request",
			"confidence":           0.7,
			"cloud":                true,
			"why_trail": []map[string]any{
				{"role": "source", "function_uid": "func-source"},
				{"role": "sink", "function_uid": "func-sink"},
			},
			"why_trail_truncated": true,
		},
	}

	got, err := decodeCodeInterprocEvidenceInput(envelope)
	if err != nil {
		t.Fatalf("decodeCodeInterprocEvidenceInput error = %v, want nil", err)
	}
	if got.SourceFunctionUID != "func-source" || got.SinkFunctionUID != "func-sink" || got.RelativePath != "src/handler.go" {
		t.Fatalf("identity fields not mapped: %+v", got)
	}
	if got.SourceFunctionName != "readRequest" || got.SinkFunctionName != "execQuery" || got.Language != "go" {
		t.Fatalf("name/language fields not mapped: %+v", got)
	}
	if got.SinkKind != "sql" || got.SourceKind != "http_request" {
		t.Fatalf("kind fields not mapped: %+v", got)
	}
	if got.Confidence != 0.7 || got.Cloud != true {
		t.Fatalf("confidence/cloud not mapped: %+v", got)
	}
	if len(got.WhyTrail) != 2 || got.WhyTrail[0]["function_uid"] != "func-source" {
		t.Fatalf("why trail not mapped: %+v", got.WhyTrail)
	}
	if !got.WhyTrailTruncated {
		t.Fatalf("WhyTrailTruncated = false, want true")
	}
}

// TestDecodeCodeInterprocEvidenceInputDefaultsCloudAbsent proves the cloud
// flag defaults to false when the payload omits it (the collector only sets
// it when true).
func TestDecodeCodeInterprocEvidenceInputDefaultsCloudAbsent(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeInterprocEvidenceFactKind,
		Payload: map[string]any{
			"source_function_uid": "func-source",
			"sink_function_uid":   "func-sink",
		},
	}
	got, err := decodeCodeInterprocEvidenceInput(envelope)
	if err != nil {
		t.Fatalf("decodeCodeInterprocEvidenceInput error = %v, want nil", err)
	}
	if got.Cloud {
		t.Fatalf("cloud must default to false when absent, got %+v", got)
	}
}
