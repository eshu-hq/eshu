// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// TestDecodeCodeTaintEvidenceQuarantinesMissingFunctionUID is the flagship
// regression test for Wave 4f S2 of Contract System v1 (issue #4754): the
// dataflow family's typed-decode migration. It proves the accuracy guarantee
// the migration exists to protect: a "code_taint_evidence" fact missing its
// required function_uid key is rejected by the typed decode seam as a
// classified input_invalid error, rather than silently producing an evidence
// row keyed on an empty-string function uid (the pre-migration
// payloadString(payload, "function_uid") behavior, which returns "" for an
// absent key with no operator signal).
func TestDecodeCodeTaintEvidenceQuarantinesMissingFunctionUID(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-taint-missing-function-uid",
		FactKind: "code_taint_evidence",
		Payload: map[string]any{
			// "function_uid" intentionally absent.
			"repo_id":       "repo-taint",
			"relative_path": "app.py",
			"function_name": "handle",
			"kind":          "sql_injection",
			"sink_kind":     "sql_exec",
			"source_kind":   "http_request",
			"source_line":   float64(3),
			"sink_line":     float64(9),
			"confidence":    0.75,
		},
	}

	_, err := schemadecode.DecodeCodeTaintEvidence(malformed)
	if err == nil {
		t.Fatalf("schemadecode.DecodeCodeTaintEvidence(missing function_uid) error = nil, want an input_invalid *factdecode.FactDecodeError")
	}
	q, quarantinable, fatal := factdecode.PartitionDecodeFailures(malformed, err)
	if !quarantinable {
		t.Fatalf("factdecode.PartitionDecodeFailures classified the missing-function_uid decode error as fatal (%v), want a quarantinable input_invalid", fatal)
	}
	if q.Field != "function_uid" || q.Classification != "input_invalid" {
		t.Fatalf("factdecode.QuarantinedFact = {field:%q classification:%q}, want {function_uid input_invalid}", q.Field, q.Classification)
	}

	// A valid sibling fact in the same batch must still decode and be usable
	// to project its evidence row (per-fact isolation).
	valid := facts.Envelope{
		FactID:   "valid-taint",
		FactKind: "code_taint_evidence",
		Payload: map[string]any{
			"function_uid":  "uid:handle-fn",
			"repo_id":       "repo-taint",
			"relative_path": "app.py",
			"function_name": "handle",
			"kind":          "sql_injection",
			"sink_kind":     "sql_exec",
			"source_kind":   "http_request",
			"source_line":   float64(3),
			"sink_line":     float64(9),
			"confidence":    0.75,
		},
	}
	evidence, err := schemadecode.DecodeCodeTaintEvidence(valid)
	if err != nil {
		t.Fatalf("schemadecode.DecodeCodeTaintEvidence(valid sibling) error = %v, want nil", err)
	}
	if evidence.FunctionUID != "uid:handle-fn" {
		t.Fatalf("schemadecode.DecodeCodeTaintEvidence FunctionUID = %q, want uid:handle-fn", evidence.FunctionUID)
	}
	rows, _, err := taint.ExtractEvidenceRowsWithQuarantine([]facts.Envelope{valid})
	if err != nil {
		t.Fatalf("taint.ExtractEvidenceRowsWithQuarantine(valid sibling) error = %v, want nil", err)
	}
	if len(rows) != 1 || rows[0]["function_uid"] != "uid:handle-fn" {
		t.Fatalf("taint.ExtractEvidenceRowsWithQuarantine(valid sibling) rows = %#v, want one row keyed on uid:handle-fn", rows)
	}
}

// TestDecodeCodeInterprocEvidenceQuarantinesMissingEndpoint mirrors the taint
// case above for the interproc family's two edge endpoints: a
// "code_interproc_evidence" fact missing either source_function_uid or
// sink_function_uid must dead-letter as input_invalid, never silently produce
// a TAINT_FLOWS_TO edge under an empty-string endpoint (the pre-migration
// ExtractInterprocEvidenceRows guard already dropped these rows, but
// silently — with no operator-visible dead-letter signal).
func TestDecodeCodeInterprocEvidenceQuarantinesMissingEndpoint(t *testing.T) {
	t.Parallel()

	missingSource := facts.Envelope{
		FactID:   "malformed-interproc-missing-source",
		FactKind: "code_interproc_evidence",
		Payload: map[string]any{
			// "source_function_uid" intentionally absent.
			"sink_function_uid": "uid:sink-fn",
			"repo_id":           "repo-interproc",
			"relative_path":     "app.py",
			"sink_kind":         "sql_exec",
			"source_kind":       "http_request",
			"confidence":        0.6,
		},
	}
	if _, err := schemadecode.DecodeCodeInterprocEvidence(missingSource); err == nil {
		t.Fatalf("schemadecode.DecodeCodeInterprocEvidence(missing source_function_uid) error = nil, want an input_invalid *factdecode.FactDecodeError")
	} else {
		q, quarantinable, fatal := factdecode.PartitionDecodeFailures(missingSource, err)
		if !quarantinable {
			t.Fatalf("factdecode.PartitionDecodeFailures classified the missing-source_function_uid decode error as fatal (%v), want a quarantinable input_invalid", fatal)
		}
		if q.Field != "source_function_uid" || q.Classification != "input_invalid" {
			t.Fatalf("factdecode.QuarantinedFact = {field:%q classification:%q}, want {source_function_uid input_invalid}", q.Field, q.Classification)
		}
	}

	valid := facts.Envelope{
		FactID:   "valid-interproc",
		FactKind: "code_interproc_evidence",
		Payload: map[string]any{
			"source_function_uid": "uid:source-fn",
			"sink_function_uid":   "uid:sink-fn",
			"repo_id":             "repo-interproc",
			"relative_path":       "app.py",
			"sink_kind":           "sql_exec",
			"source_kind":         "http_request",
			"confidence":          0.6,
		},
	}
	evidence, err := schemadecode.DecodeCodeInterprocEvidence(valid)
	if err != nil {
		t.Fatalf("schemadecode.DecodeCodeInterprocEvidence(valid sibling) error = %v, want nil", err)
	}
	rows, _, extractErr := taint.ExtractInterprocEvidenceRowsWithQuarantine([]facts.Envelope{valid})
	if extractErr != nil {
		t.Fatalf("taint.ExtractInterprocEvidenceRowsWithQuarantine(valid sibling) error = %v, want nil", extractErr)
	}
	if len(rows) != 1 || rows[0]["source_function_uid"] != evidence.SourceFunctionUID {
		t.Fatalf("taint.ExtractInterprocEvidenceRowsWithQuarantine(valid sibling) rows = %#v, want one edge row for the valid sibling", rows)
	}
}

// TestDecodeCodeFunctionSummaryQuarantinesMissingFunctionID proves the
// function-summary family's durable identity requirement: a
// "code_function_summary" fact missing its required function_id key must
// dead-letter as input_invalid rather than being silently skipped by
// the summary handler's ExtractEffects
// path (the pre-migration postgres loader silently skipped it via an
// `if id == "" { continue }` guard with no operator-visible signal).
func TestDecodeCodeFunctionSummaryQuarantinesMissingFunctionID(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-summary-missing-function-id",
		FactKind: "code_function_summary",
		Payload: map[string]any{
			// "function_id" intentionally absent.
			"repo_id":  "repo-summary",
			"language": "go",
		},
	}
	if _, err := schemadecode.DecodeCodeFunctionSummary(malformed); err == nil {
		t.Fatalf("schemadecode.DecodeCodeFunctionSummary(missing function_id) error = nil, want an input_invalid *factdecode.FactDecodeError")
	} else {
		q, quarantinable, fatal := factdecode.PartitionDecodeFailures(malformed, err)
		if !quarantinable {
			t.Fatalf("factdecode.PartitionDecodeFailures classified the missing-function_id decode error as fatal (%v), want a quarantinable input_invalid", fatal)
		}
		if q.Field != "function_id" || q.Classification != "input_invalid" {
			t.Fatalf("factdecode.QuarantinedFact = {field:%q classification:%q}, want {function_id input_invalid}", q.Field, q.Classification)
		}
	}

	valid := facts.Envelope{
		FactID:   "valid-summary",
		FactKind: "code_function_summary",
		Payload: map[string]any{
			"function_id": "repo-summary:pkg:Handle",
			"repo_id":     "repo-summary",
			"language":    "go",
		},
	}
	summary, err := schemadecode.DecodeCodeFunctionSummary(valid)
	if err != nil {
		t.Fatalf("schemadecode.DecodeCodeFunctionSummary(valid sibling) error = %v, want nil", err)
	}
	if summary.FunctionID != "repo-summary:pkg:Handle" {
		t.Fatalf("schemadecode.DecodeCodeFunctionSummary FunctionID = %q, want repo-summary:pkg:Handle", summary.FunctionID)
	}
}

// TestDecodeCodeFunctionSourceQuarantinesMissingRequiredFields proves the
// function-source family's two required fields: a "code_function_source" fact
// missing either function_id or kind must dead-letter as input_invalid rather
// than being silently skipped (the pre-migration postgres loader dropped it
// via an `if id == "" || kind == "" { continue }` guard).
func TestDecodeCodeFunctionSourceQuarantinesMissingRequiredFields(t *testing.T) {
	t.Parallel()

	missingKind := facts.Envelope{
		FactID:   "malformed-source-missing-kind",
		FactKind: "code_function_source",
		Payload: map[string]any{
			"function_id": "repo-source:pkg:Handle",
			"param_index": float64(0),
			// "kind" intentionally absent.
			"repo_id": "repo-source",
		},
	}
	if _, err := schemadecode.DecodeCodeFunctionSource(missingKind); err == nil {
		t.Fatalf("schemadecode.DecodeCodeFunctionSource(missing kind) error = nil, want an input_invalid *factdecode.FactDecodeError")
	} else {
		q, quarantinable, _ := factdecode.PartitionDecodeFailures(missingKind, err)
		if !quarantinable {
			t.Fatalf("factdecode.PartitionDecodeFailures did not classify the missing-kind decode error as quarantinable input_invalid")
		}
		if q.Field != "kind" {
			t.Fatalf("factdecode.QuarantinedFact.Field = %q, want kind", q.Field)
		}
	}

	valid := facts.Envelope{
		FactID:   "valid-source",
		FactKind: "code_function_source",
		Payload: map[string]any{
			"function_id": "repo-source:pkg:Handle",
			"param_index": float64(0),
			"kind":        "http_request",
			"repo_id":     "repo-source",
		},
	}
	source, err := schemadecode.DecodeCodeFunctionSource(valid)
	if err != nil {
		t.Fatalf("schemadecode.DecodeCodeFunctionSource(valid sibling) error = %v, want nil", err)
	}
	if source.FunctionID != "repo-source:pkg:Handle" || source.Kind != "http_request" {
		t.Fatalf("schemadecode.DecodeCodeFunctionSource = %+v, want FunctionID=repo-source:pkg:Handle Kind=http_request", source)
	}
}

// TestDecodeCodeDataflowFamilyTreatsPersistedZeroVersionAsLatestMajor is the
// CRITICAL regression test issue #4754 calls out by name: the dataflow
// family's six fact kinds are version-less on the wire (never registered in
// specs/fact-kind-registry.v1.yaml), so the Postgres persist layer stamps
// SchemaVersion="0.0.0" for every one of them
// (go/internal/storage/postgres/facts_streaming.go, calling
// payloadstore.EmptyToDefault(SchemaVersion, "0.0.0")). A fact LOADED BACK from Postgres
// for reduction therefore carries the literal string "0.0.0", NOT an absent/
// empty SchemaVersion.
//
// Wave 4f S1 (#4753) shipped exactly this regression: its unit test used an
// ABSENT version ("") and passed, while the real corpus — which persists
// "0.0.0" — silently zeroed out the whole code-graph. This test decodes a
// fact carrying the REAL persisted "0.0.0" value (not "") for every one of
// the six dataflow kinds and asserts each decodes successfully and produces
// its identity/edges, so this migration cannot repeat that regression.
func TestDecodeCodeDataflowFamilyTreatsPersistedZeroVersionAsLatestMajor(t *testing.T) {
	t.Parallel()

	const persistedZeroVersion = "0.0.0"

	t.Run("code_dataflow_scanned", func(t *testing.T) {
		t.Parallel()
		env := facts.Envelope{
			FactID:        "persisted-scanned",
			FactKind:      "code_dataflow_scanned",
			SchemaVersion: persistedZeroVersion,
			Payload: map[string]any{
				"repo_id": "repo-persisted",
				"reason":  "value-flow gate scanned the repository snapshot",
			},
		}
		scanned, err := schemadecode.DecodeCodeDataflowScanned(env)
		if err != nil {
			t.Fatalf("schemadecode.DecodeCodeDataflowScanned(SchemaVersion=%q) error = %v, want nil", persistedZeroVersion, err)
		}
		if scanned.RepoID == nil || *scanned.RepoID != "repo-persisted" {
			t.Fatalf("schemadecode.DecodeCodeDataflowScanned RepoID = %v, want repo-persisted", scanned.RepoID)
		}
	})

	t.Run("code_dataflow_function", func(t *testing.T) {
		t.Parallel()
		env := facts.Envelope{
			FactID:        "persisted-dataflow-function",
			FactKind:      "code_dataflow_function",
			SchemaVersion: persistedZeroVersion,
			Payload: map[string]any{
				"repo_id":       "repo-persisted",
				"relative_path": "app.go",
				"function_name": "Handle",
			},
		}
		function, err := schemadecode.DecodeCodeDataflowFunction(env)
		if err != nil {
			t.Fatalf("schemadecode.DecodeCodeDataflowFunction(SchemaVersion=%q) error = %v, want nil", persistedZeroVersion, err)
		}
		if function.RepoID != "repo-persisted" || function.RelativePath != "app.go" || function.FunctionName != "Handle" {
			t.Fatalf("schemadecode.DecodeCodeDataflowFunction = %+v, want identity repo-persisted/app.go/Handle", function)
		}
	})

	t.Run("code_function_summary", func(t *testing.T) {
		t.Parallel()
		env := facts.Envelope{
			FactID:        "persisted-summary",
			FactKind:      "code_function_summary",
			SchemaVersion: persistedZeroVersion,
			Payload: map[string]any{
				"function_id": "repo-persisted:pkg:Handle",
				"repo_id":     "repo-persisted",
			},
		}
		summary, err := schemadecode.DecodeCodeFunctionSummary(env)
		if err != nil {
			t.Fatalf("schemadecode.DecodeCodeFunctionSummary(SchemaVersion=%q) error = %v, want nil", persistedZeroVersion, err)
		}
		if summary.FunctionID != "repo-persisted:pkg:Handle" {
			t.Fatalf("schemadecode.DecodeCodeFunctionSummary FunctionID = %q, want repo-persisted:pkg:Handle", summary.FunctionID)
		}
	})

	t.Run("code_function_source", func(t *testing.T) {
		t.Parallel()
		env := facts.Envelope{
			FactID:        "persisted-source",
			FactKind:      "code_function_source",
			SchemaVersion: persistedZeroVersion,
			Payload: map[string]any{
				"function_id": "repo-persisted:pkg:Handle",
				"kind":        "http_request",
				"param_index": float64(0),
				"repo_id":     "repo-persisted",
			},
		}
		source, err := schemadecode.DecodeCodeFunctionSource(env)
		if err != nil {
			t.Fatalf("schemadecode.DecodeCodeFunctionSource(SchemaVersion=%q) error = %v, want nil", persistedZeroVersion, err)
		}
		if source.FunctionID != "repo-persisted:pkg:Handle" || source.Kind != "http_request" {
			t.Fatalf("schemadecode.DecodeCodeFunctionSource = %+v, want identity repo-persisted:pkg:Handle/http_request", source)
		}
	})

	t.Run("code_taint_evidence", func(t *testing.T) {
		t.Parallel()
		env := facts.Envelope{
			FactID:        "persisted-taint",
			FactKind:      "code_taint_evidence",
			SchemaVersion: persistedZeroVersion,
			Payload: map[string]any{
				"function_uid": "uid:persisted-fn",
				"repo_id":      "repo-persisted",
			},
		}
		evidence, err := schemadecode.DecodeCodeTaintEvidence(env)
		if err != nil {
			t.Fatalf("schemadecode.DecodeCodeTaintEvidence(SchemaVersion=%q) error = %v, want nil", persistedZeroVersion, err)
		}
		if evidence.FunctionUID != "uid:persisted-fn" {
			t.Fatalf("schemadecode.DecodeCodeTaintEvidence FunctionUID = %q, want uid:persisted-fn", evidence.FunctionUID)
		}
		rows, _, extractErr := taint.ExtractEvidenceRowsWithQuarantine([]facts.Envelope{env})
		if extractErr != nil {
			t.Fatalf("taint.ExtractEvidenceRowsWithQuarantine(persisted 0.0.0 version) error = %v, want nil", extractErr)
		}
		if len(rows) != 1 {
			t.Fatalf("taint.ExtractEvidenceRowsWithQuarantine(persisted 0.0.0 version) rows = %#v, want one row, not a dropped/empty graph", rows)
		}
	})

	t.Run("code_interproc_evidence", func(t *testing.T) {
		t.Parallel()
		env := facts.Envelope{
			FactID:        "persisted-interproc",
			FactKind:      "code_interproc_evidence",
			SchemaVersion: persistedZeroVersion,
			Payload: map[string]any{
				"source_function_uid": "uid:persisted-source-fn",
				"sink_function_uid":   "uid:persisted-sink-fn",
				"repo_id":             "repo-persisted",
			},
		}
		evidence, err := schemadecode.DecodeCodeInterprocEvidence(env)
		if err != nil {
			t.Fatalf("schemadecode.DecodeCodeInterprocEvidence(SchemaVersion=%q) error = %v, want nil", persistedZeroVersion, err)
		}
		rows, _, extractErr := taint.ExtractInterprocEvidenceRowsWithQuarantine([]facts.Envelope{env})
		if extractErr != nil {
			t.Fatalf("taint.ExtractInterprocEvidenceRowsWithQuarantine(persisted 0.0.0 version) error = %v, want nil", extractErr)
		}
		if len(rows) != 1 || rows[0]["source_function_uid"] != evidence.SourceFunctionUID {
			t.Fatalf("taint.ExtractInterprocEvidenceRowsWithQuarantine(persisted 0.0.0 version) rows = %#v, want one edge row, not a dropped/empty graph", rows)
		}
	})
}
