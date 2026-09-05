// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// routeRecordingReducerExecutor is group-capable and records which dispatch
// route — autocommit Execute vs grouped ExecuteGroup — each statement took, so
// a factory-level test can assert the production wiring routes retract DELETEs
// correctly per backend.
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

// TestSemanticEntityWriterForGraphBackendNornicDBGroupsRetractWithUpserts
// proves the FACTORY (semanticEntityWriterForGraphBackend) leaves the NornicDB
// semantic retract inside the grouped, atomic transaction: driven by a
// group-capable executor, the Module DETACH DELETE retract and the UNWIND
// upserts both route through ExecuteGroup, with nothing on autocommit Execute.
//
// This inverts the #4367 assertion it replaces, deliberately. That one required
// the retract on autocommit Execute because grouped DETACH DELETEs under-applied
// on NornicDB v1.1.11 and silently left semantic nodes behind. #5323 measured
// the grouped retract correct on 1.2.1 and 1.2.2, #6176 re-measured it on the
// deployed 1.2.2 build, and the supported floor now sits above v1.1.11 — so
// re-splitting the retract onto Execute would give up the atomic retract+upsert
// rollback for a defect no supported backend has (#6176).
func TestSemanticEntityWriterForGraphBackendNornicDBGroupsRetractWithUpserts(t *testing.T) {
	t.Parallel()

	exec := &routeRecordingReducerExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(exec, 100, runtimecfg.GraphBackendNornicDB, func(string) string { return "" })
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}
	if _, err := writer.WriteSemanticEntities(context.Background(), semanticentity.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    []semanticentity.SemanticEntityRow{semanticModuleRow("module-ts-1")},
	}); err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if len(exec.executeCyphers) != 0 {
		t.Fatalf("NornicDB factory: %d statement(s) routed through autocommit Execute, want 0 — retract and upsert must commit in one atomic ExecuteGroup", len(exec.executeCyphers))
	}
	if !anyReducerCypherContains(exec.groupCyphers, "DETACH DELETE") {
		t.Fatal("NornicDB factory: the Module DETACH DELETE retract must be inside the grouped transaction with the upserts")
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
	if _, err := writer.WriteSemanticEntities(context.Background(), semanticentity.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows:    []semanticentity.SemanticEntityRow{semanticModuleRow("module-ts-1")},
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
