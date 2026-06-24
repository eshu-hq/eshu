// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func codeTaintEvidenceRow() map[string]any {
	return map[string]any{
		"uid":           "taint-1",
		"function_uid":  "func-handle",
		"function_name": "handle",
		"relative_path": "src/handler.go",
		"language":      "go",
		"kind":          "TAINTED",
		"sink_kind":     "sql",
		"source_kind":   "http_request",
		"binding":       "q",
		"source_line":   4,
		"sink_line":     5,
		"confidence":    0.8,
		"guard_reason":  "allowed",
	}
}

func TestCodeTaintEvidenceWriterEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.WriteCodeTaintEvidence(context.Background(), nil, "scope-1", "gen-1", "reducer/code-taint"); err != nil {
		t.Fatalf("WriteCodeTaintEvidence returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty rows", len(executor.calls))
	}
}

// TestCodeTaintEvidenceWriterMatchesFunctionAndMergesEvidence proves the writer
// MATCHes the Function (never creates it), MERGEs the evidence node on uid, and
// MERGEs the HAS_TAINT_EVIDENCE edge with confidence + provenance.
func TestCodeTaintEvidenceWriterMatchesFunctionAndMergesEvidence(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.WriteCodeTaintEvidence(
		context.Background(),
		[]map[string]any{codeTaintEvidenceRow()},
		"scope-1", "gen-1", "reducer/code-taint",
	); err != nil {
		t.Fatalf("WriteCodeTaintEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MATCH (f:Function {uid: row.function_uid})",
		"MERGE (ev:CodeTaintEvidence {uid: row.uid})",
		"MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)",
		"ev.confidence = row.confidence",
		"ev.guard_reason = row.guard_reason",
		"rel.sink_kind = row.sink_kind",
		"ev.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	// The Function must be MATCHed, not MERGEd (taint evidence must not invent a
	// code node).
	if strings.Contains(cypher, "MERGE (f:Function") {
		t.Fatalf("cypher must MATCH the Function, not MERGE it:\n%s", cypher)
	}
	// Scope/generation/evidence-source are stamped onto the rows, not the caller's.
	rows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows parameter missing or wrong shape: %#v", executor.calls[0].Parameters["rows"])
	}
	if rows[0]["scope_id"] != "scope-1" || rows[0]["generation_id"] != "gen-1" || rows[0]["evidence_source"] != "reducer/code-taint" {
		t.Fatalf("row not stamped with scope/generation/evidence source: %+v", rows[0])
	}
}

// TestCodeTaintEvidenceWriterRetract proves retraction targets only the
// reducer-owned nodes for the given scopes and evidence source.
func TestCodeTaintEvidenceWriterRetract(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.RetractCodeTaintEvidence(context.Background(), []string{"scope-1"}, "gen-1", "reducer/code-taint"); err != nil {
		t.Fatalf("RetractCodeTaintEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{"MATCH (n:CodeTaintEvidence)", "n.scope_id IN $scope_ids", "n.evidence_source = $evidence_source", "DETACH DELETE n"} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract cypher missing %q:\n%s", want, cypher)
		}
	}
	if got := executor.calls[0].Parameters["evidence_source"]; got != "reducer/code-taint" {
		t.Fatalf("evidence_source param = %v, want reducer/code-taint", got)
	}
}

// TestCodeTaintEvidenceWriterEmptyRetractIsNoOp proves retracting no scopes does
// nothing.
func TestCodeTaintEvidenceWriterEmptyRetractIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.RetractCodeTaintEvidence(context.Background(), nil, "gen-1", "reducer/code-taint"); err != nil {
		t.Fatalf("RetractCodeTaintEvidence returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty scopes", len(executor.calls))
	}
}

// TestCodeTaintEvidenceWriterRetractStaleGeneration proves side cleanup deletes
// only stale generations for one scope/source pair and keeps the mutation
// bounded so a runner can call it repeatedly until no stale rows remain.
func TestCodeTaintEvidenceWriterRetractStaleGeneration(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.RetractStaleCodeTaintEvidence(context.Background(), "scope-1", "gen-current", "reducer/code-taint", 123); err != nil {
		t.Fatalf("RetractStaleCodeTaintEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (n:CodeTaintEvidence)",
		"n.scope_id = $scope_id",
		"n.evidence_source = $evidence_source",
		"n.generation_id <> $generation_id",
		"WITH n LIMIT $limit",
		"DETACH DELETE n",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("stale retract cypher missing %q:\n%s", want, cypher)
		}
	}
	for _, forbidden := range []string{"n.scope_id IN $scope_ids", "DETACH DELETE ev"} {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("stale retract cypher contains unsafe scope-wide pattern %q:\n%s", forbidden, cypher)
		}
	}
	if got := executor.calls[0].Parameters["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id param = %v, want scope-1", got)
	}
	if got := executor.calls[0].Parameters["generation_id"]; got != "gen-current" {
		t.Fatalf("generation_id param = %v, want gen-current", got)
	}
	if got := executor.calls[0].Parameters["evidence_source"]; got != "reducer/code-taint" {
		t.Fatalf("evidence_source param = %v, want reducer/code-taint", got)
	}
	if got := executor.calls[0].Parameters["limit"]; got != 123 {
		t.Fatalf("limit param = %v, want 123", got)
	}
}

// TestCodeTaintEvidenceWriterRetractStaleGenerationRejectsBlankInputs proves
// the side-cleanup primitive fails closed instead of broadening the deletion
// predicate when the runner lacks the current scope, generation, or source.
func TestCodeTaintEvidenceWriterRetractStaleGenerationRejectsBlankInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		scopeID        string
		generationID   string
		evidenceSource string
		wantErr        string
	}{
		{name: "scope", scopeID: "", generationID: "gen-current", evidenceSource: "reducer/code-taint", wantErr: "scope_id must not be blank"},
		{name: "generation", scopeID: "scope-1", generationID: "", evidenceSource: "reducer/code-taint", wantErr: "generation_id must not be blank"},
		{name: "source", scopeID: "scope-1", generationID: "gen-current", evidenceSource: "", wantErr: "evidence_source must not be blank"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			executor := &recordingExecutor{}
			writer := NewCodeTaintEvidenceWriter(executor, 0)
			err := writer.RetractStaleCodeTaintEvidence(context.Background(), tt.scopeID, tt.generationID, tt.evidenceSource, 123)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
			if len(executor.calls) != 0 {
				t.Fatalf("len(calls) = %d, want 0 after validation failure", len(executor.calls))
			}
		})
	}
}
