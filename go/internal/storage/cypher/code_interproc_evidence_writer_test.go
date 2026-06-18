package cypher

import (
	"context"
	"strings"
	"testing"
)

func codeInterprocEvidenceRow() map[string]any {
	return map[string]any{
		"uid":                  "interproc-1",
		"source_function_uid":  "func-source",
		"sink_function_uid":    "func-sink",
		"source_function_name": "readRequest",
		"sink_function_name":   "execQuery",
		"relative_path":        "src/handler.go",
		"language":             "go",
		"sink_kind":            "sql",
		"source_kind":          "http_request",
		"confidence":           0.7,
		"cloud":                true,
	}
}

func TestCodeInterprocEvidenceWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.WriteCodeInterprocEvidence(context.Background(), nil, "scope-1", "gen-1", "reducer/code-interproc"); err != nil {
		t.Fatalf("WriteCodeInterprocEvidence returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

// TestCodeInterprocEvidenceWriterMatchesFunctionsAndMergesEdge proves the writer
// MATCHes both Functions (never creates them) and MERGEs the TAINT_FLOWS_TO edge
// keyed on the evidence uid with its kinds and provenance.
func TestCodeInterprocEvidenceWriterMatchesFunctionsAndMergesEdge(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.WriteCodeInterprocEvidence(
		context.Background(),
		[]map[string]any{codeInterprocEvidenceRow()},
		"scope-1", "gen-1", "reducer/code-interproc",
	); err != nil {
		t.Fatalf("WriteCodeInterprocEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MATCH (s:Function {uid: row.source_function_uid})",
		"MATCH (t:Function {uid: row.sink_function_uid})",
		"MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t)",
		"rel.sink_kind = row.sink_kind",
		"rel.source_kind = row.source_kind",
		"rel.confidence = row.confidence",
		"rel.cloud = row.cloud",
		"rel.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	// Both Functions must be MATCHed, not MERGEd (interproc evidence must not
	// invent a code node).
	if strings.Contains(cypher, "MERGE (s:Function") || strings.Contains(cypher, "MERGE (t:Function") {
		t.Fatalf("cypher must MATCH both Functions, not MERGE them:\n%s", cypher)
	}
	// Scope/generation/evidence-source are stamped onto the rows, not the caller's.
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows parameter missing or wrong shape: %#v", executor.calls[0].Parameters["rows"])
	}
	if rows[0]["scope_id"] != "scope-1" || rows[0]["generation_id"] != "gen-1" || rows[0]["evidence_source"] != "reducer/code-interproc" {
		t.Fatalf("row not stamped with scope/generation/evidence source: %+v", rows[0])
	}
}

// TestCodeInterprocEvidenceWriterRetract proves retraction targets only the
// reducer-owned edges for the given scopes and evidence source.
func TestCodeInterprocEvidenceWriterRetract(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.RetractCodeInterprocEvidence(context.Background(), []string{"scope-1"}, "gen-1", "reducer/code-interproc"); err != nil {
		t.Fatalf("RetractCodeInterprocEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (:Function)-[rel:TAINT_FLOWS_TO]->(:Function)",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract cypher missing %q:\n%s", want, cypher)
		}
	}
	if got := executor.calls[0].Parameters["evidence_source"]; got != "reducer/code-interproc" {
		t.Fatalf("evidence_source param = %v, want reducer/code-interproc", got)
	}
}

// TestCodeInterprocEvidenceWriterEmptyRetractIsNoOp proves retracting no scopes
// does nothing.
func TestCodeInterprocEvidenceWriterEmptyRetractIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.RetractCodeInterprocEvidence(context.Background(), nil, "gen-1", "reducer/code-interproc"); err != nil {
		t.Fatalf("RetractCodeInterprocEvidence returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scopes", len(executor.calls))
	}
}
