// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// routeRecordingReducerExecutor is group-capable and records which dispatch
// route — autocommit Execute vs grouped ExecuteGroup — each statement took, so
// a factory-level test can assert the production wiring (not a manually-enabled
// writer option) routes retract DELETEs correctly per backend.
type routeRecordingReducerExecutor struct {
	executeCyphers []string
	groupCyphers   []string
}

func (e *routeRecordingReducerExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.executeCyphers = append(e.executeCyphers, stmt.Cypher)
	return nil
}

func (e *routeRecordingReducerExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	for _, s := range stmts {
		e.groupCyphers = append(e.groupCyphers, s.Cypher)
	}
	return nil
}

func anyReducerCypherContains(cyphers []string, sub string) bool {
	for _, c := range cyphers {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

// TestSemanticEntityWriterForGraphBackendNornicDBRoutesRetractThroughExecute
// proves the FACTORY (semanticEntityWriterForGraphBackend), not a manually
// enabled writer option, wires sequential retract for NornicDB: driven by a
// group-capable executor, the Module DETACH DELETE retract routes through
// autocommit Execute while upserts batch through ExecuteGroup. If the factory
// dropped the WithSequentialRetract() call, the retract would route through
// ExecuteGroup (grouped DELETEs under-apply on NornicDB v1.1.11) and this test
// would fail — closing the gap left by the writer-level dispatch tests, which
// enable the option directly.
func TestSemanticEntityWriterForGraphBackendNornicDBRoutesRetractThroughExecute(t *testing.T) {
	t.Parallel()

	exec := &routeRecordingReducerExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(exec, 100, runtimecfg.GraphBackendNornicDB, func(string) string { return "" })
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}
	if _, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    []reducer.SemanticEntityRow{semanticModuleRow("module-ts-1")},
	}); err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if !anyReducerCypherContains(exec.executeCyphers, "DETACH DELETE") {
		t.Fatal("NornicDB factory: the Module DETACH DELETE retract did not route through autocommit Execute — the WithSequentialRetract() factory wiring is missing, so grouped-writes would send DELETEs through ExecuteGroup and under-apply")
	}
	if anyReducerCypherContains(exec.groupCyphers, "DETACH DELETE") {
		t.Fatal("NornicDB factory: a DETACH DELETE routed through ExecuteGroup — grouped DELETEs under-apply on v1.1.11")
	}
	if !anyReducerCypherContains(exec.groupCyphers, "UNWIND $rows AS row") {
		t.Fatal("NornicDB factory: upserts must still batch through ExecuteGroup")
	}
}

// TestSemanticEntityWriterForGraphBackendNeo4jRoutesRetractGrouped proves the
// factory does NOT enable sequential retract for Neo4j: retract and upsert both
// route through ExecuteGroup (one atomic transaction), with zero statements on
// autocommit Execute. This guards the #5320 regression — unconditionally
// splitting retracts onto Execute removed Neo4j's atomic retract+upsert
// rollback.
func TestSemanticEntityWriterForGraphBackendNeo4jRoutesRetractGrouped(t *testing.T) {
	t.Parallel()

	exec := &routeRecordingReducerExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(exec, 100, runtimecfg.GraphBackendNeo4j, func(string) string { return "" })
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}
	if _, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    []reducer.SemanticEntityRow{semanticModuleRow("module-ts-1")},
	}); err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if len(exec.executeCyphers) != 0 {
		t.Fatalf("Neo4j factory: %d statement(s) routed through autocommit Execute, want 0 — retract+upsert must commit in one atomic ExecuteGroup", len(exec.executeCyphers))
	}
	// The Neo4j broad pipe-label retract must be inside the grouped transaction.
	if !anyReducerCypherContains(exec.groupCyphers, ":Annotation|Typedef|TypeAlias") {
		t.Fatal("Neo4j factory: the broad retract must be inside the grouped (atomic) transaction with the upserts")
	}
}
