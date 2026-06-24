// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		"why_trail_json":       `[{"role":"source","function_uid":"func-source"},{"role":"sink","function_uid":"func-sink"}]`,
		"why_trail_truncated":  true,
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
		"rel.why_trail_json = row.why_trail_json",
		"rel.why_trail_truncated = row.why_trail_truncated",
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

// TestCodeInterprocEvidenceWriterRetractEvidenceSource proves global fixpoint
// retraction deletes by evidence source without depending on the last writer's
// stamped scope_id.
func TestCodeInterprocEvidenceWriterRetractEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.RetractCodeInterprocEvidenceSource(context.Background(), "reducer/code-interproc-fixpoint"); err != nil {
		t.Fatalf("RetractCodeInterprocEvidenceSource returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (:Function)-[rel:TAINT_FLOWS_TO]->(:Function)",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("source retract cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "scope_id") {
		t.Fatalf("source retract must not filter by scope_id:\n%s", cypher)
	}
	if got := executor.calls[0].Parameters["evidence_source"]; got != "reducer/code-interproc-fixpoint" {
		t.Fatalf("evidence_source param = %v, want fixpoint source", got)
	}
}

// TestCodeInterprocEvidenceWriterRetractStaleGeneration proves side cleanup
// deletes only stale generations for one scope/source pair and keeps the
// mutation bounded so a runner can call it repeatedly until no stale edges
// remain.
func TestCodeInterprocEvidenceWriterRetractStaleGeneration(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.RetractStaleCodeInterprocEvidence(context.Background(), "scope-1", "gen-current", "reducer/code-interproc", 123); err != nil {
		t.Fatalf("RetractStaleCodeInterprocEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (:Function)-[rel:TAINT_FLOWS_TO]->(:Function)",
		"rel.scope_id = $scope_id",
		"rel.evidence_source = $evidence_source",
		"rel.generation_id <> $generation_id",
		"WITH rel LIMIT $limit",
		"DELETE rel",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("stale retract cypher missing %q:\n%s", want, cypher)
		}
	}
	for _, forbidden := range []string{"rel.scope_id IN $scope_ids", "DETACH DELETE"} {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("stale retract cypher contains unsafe pattern %q:\n%s", forbidden, cypher)
		}
	}
	if got := executor.calls[0].Parameters["scope_id"]; got != "scope-1" {
		t.Fatalf("scope_id param = %v, want scope-1", got)
	}
	if got := executor.calls[0].Parameters["generation_id"]; got != "gen-current" {
		t.Fatalf("generation_id param = %v, want gen-current", got)
	}
	if got := executor.calls[0].Parameters["evidence_source"]; got != "reducer/code-interproc" {
		t.Fatalf("evidence_source param = %v, want reducer/code-interproc", got)
	}
	if got := executor.calls[0].Parameters["limit"]; got != 123 {
		t.Fatalf("limit param = %v, want 123", got)
	}
}

// TestCodeInterprocEvidenceWriterRetractStaleGenerationRejectsBlankInputs
// proves the side-cleanup primitive fails closed instead of broadening the
// deletion predicate when the runner lacks the current scope, generation, or
// source.
func TestCodeInterprocEvidenceWriterRetractStaleGenerationRejectsBlankInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		scopeID        string
		generationID   string
		evidenceSource string
		wantErr        string
	}{
		{name: "scope", scopeID: "", generationID: "gen-current", evidenceSource: "reducer/code-interproc", wantErr: "scope_id must not be blank"},
		{name: "generation", scopeID: "scope-1", generationID: "", evidenceSource: "reducer/code-interproc", wantErr: "generation_id must not be blank"},
		{name: "source", scopeID: "scope-1", generationID: "gen-current", evidenceSource: "", wantErr: "evidence_source must not be blank"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			executor := &recordingExecutor{}
			writer := NewCodeInterprocEvidenceWriter(executor, 0)
			err := writer.RetractStaleCodeInterprocEvidence(context.Background(), tt.scopeID, tt.generationID, tt.evidenceSource, 123)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
			if len(executor.calls) != 0 {
				t.Fatalf("len(calls) = %d, want 0 after validation failure", len(executor.calls))
			}
		})
	}
}
