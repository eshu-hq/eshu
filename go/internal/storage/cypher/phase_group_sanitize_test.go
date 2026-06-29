// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"testing"
)

func TestSanitizeStatementParametersStripsDiagnosticKeys(t *testing.T) {
	params := map[string]any{
		"repo_id":                   "r1",
		"generation_id":             "g2",
		"directory_paths":           []string{"/a", "/b"},
		StatementMetadataPhaseKey:   "retract",
		StatementMetadataSummaryKey: "phase=retract",
	}
	got := SanitizeStatementParameters(params)

	for _, key := range []string{StatementMetadataPhaseKey, StatementMetadataSummaryKey} {
		if _, ok := got[key]; ok {
			t.Errorf("diagnostic key %q must be stripped, but it survived", key)
		}
	}
	for _, key := range []string{"repo_id", "generation_id", "directory_paths"} {
		if _, ok := got[key]; !ok {
			t.Errorf("real Cypher parameter %q must be preserved, but it was dropped", key)
		}
	}
	// The input map must not be mutated when diagnostics are present.
	if _, ok := params[StatementMetadataPhaseKey]; !ok {
		t.Error("input map was mutated: SanitizeStatementParameters must copy, not edit in place")
	}
}

func TestSanitizeStatementParametersNoDiagnosticsReturnsInput(t *testing.T) {
	params := map[string]any{"repo_id": "r1"}
	got := SanitizeStatementParameters(params)
	// No diagnostics → same map returned (documented no-alloc fast path).
	if len(got) != 1 || got["repo_id"] != "r1" {
		t.Fatalf("expected unchanged params, got %v", got)
	}
	// Assert the documented no-alloc contract: the identical backing map is
	// returned (not a copy) when there is nothing to strip. A future change that
	// started copying here would be caught by this guard.
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(params).Pointer() {
		t.Error("no-diagnostics path must return the identical map (no-alloc fast path), got a copy")
	}

	if SanitizeStatementParameters(nil) != nil {
		t.Error("nil params must return nil")
	}
	if got := SanitizeStatementParameters(map[string]any{}); len(got) != 0 {
		t.Errorf("empty params must return empty, got %v", got)
	}
}

func TestSanitizeStatementAndStatements(t *testing.T) {
	stmt := Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    "MATCH (n) DETACH DELETE n",
		Parameters: map[string]any{
			"repo_id":                 "r1",
			StatementMetadataPhaseKey: "retract",
		},
	}
	got := SanitizeStatement(stmt)
	if got.Cypher != stmt.Cypher || got.Operation != stmt.Operation {
		t.Error("SanitizeStatement must preserve Cypher and Operation")
	}
	if _, ok := got.Parameters[StatementMetadataPhaseKey]; ok {
		t.Error("SanitizeStatement must strip diagnostic params")
	}

	batch := SanitizeStatements([]Statement{stmt, stmt})
	if len(batch) != 2 {
		t.Fatalf("SanitizeStatements must preserve length, got %d", len(batch))
	}
	for i, s := range batch {
		if _, ok := s.Parameters[StatementMetadataPhaseKey]; ok {
			t.Errorf("SanitizeStatements[%d] left a diagnostic param", i)
		}
	}
}

func TestStatementsAllUseOperation(t *testing.T) {
	retract := Statement{Operation: OperationCanonicalRetract}
	upsert := Statement{Operation: OperationCanonicalUpsert}

	if !StatementsAllUseOperation([]Statement{retract, retract}, OperationCanonicalRetract) {
		t.Error("all-retract slice must report true")
	}
	if StatementsAllUseOperation([]Statement{retract, upsert}, OperationCanonicalRetract) {
		t.Error("mixed slice must report false")
	}
	if StatementsAllUseOperation(nil, OperationCanonicalRetract) {
		t.Error("empty slice must report false")
	}
}
