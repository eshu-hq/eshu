// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterRetractEdgesSQLRelationshipUsesLabelScopedGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 1; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
	stmts := executor.groupCalls[0]
	if got, want := len(stmts), 6; got != want {
		t.Fatalf("group statement count = %d, want %d", got, want)
	}

	assertSQLRetractStatement(t, stmts[0], "Function", "QUERIES_TABLE")
	assertSQLRetractStatement(t, stmts[1], "SqlView", "REFERENCES_TABLE")
	assertSQLRetractStatement(t, stmts[2], "SqlFunction", "REFERENCES_TABLE")
	assertSQLRetractStatement(t, stmts[3], "SqlTable", "HAS_COLUMN")
	assertSQLRetractStatement(t, stmts[4], "SqlTrigger", "TRIGGERS")
	assertSQLRetractStatement(t, stmts[5], "SqlTrigger", "EXECUTES")
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "QUERIES_TABLE|REFERENCES_TABLE|HAS_COLUMN|TRIGGERS|EXECUTES") {
			t.Fatalf("cypher uses broad relationship alternation: %s", stmt.Cypher)
		}
	}
}

func TestBuildRetractSQLRelationshipEdgeStatementsUsesSharedParameters(t *testing.T) {
	t.Parallel()

	stmts := BuildRetractSQLRelationshipEdgeStatements([]string{"repo-a", "repo-b"}, "reducer/sql-relationships")
	if got, want := len(stmts), 6; got != want {
		t.Fatalf("statement count = %d, want %d", got, want)
	}

	for _, stmt := range stmts {
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
		}
		repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
		if !ok {
			t.Fatalf("repo_ids parameter type = %T, want []string", stmt.Parameters["repo_ids"])
		}
		if got, want := strings.Join(repoIDs, ","), "repo-a,repo-b"; got != want {
			t.Fatalf("repo_ids = %q, want %q", got, want)
		}
		if got, want := stmt.Parameters["evidence_source"], "reducer/sql-relationships"; got != want {
			t.Fatalf("evidence_source = %v, want %v", got, want)
		}
	}
}

func TestEdgeWriterRetractEdgesSQLRelationshipFallbackIncludesQueriesTable(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("Execute calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "QUERIES_TABLE") {
		t.Fatalf("fallback retract cypher missing QUERIES_TABLE: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "source.repo_id IN $repo_ids") {
		t.Fatalf("fallback retract cypher missing repo_id predicate: %s", stmt.Cypher)
	}
	if got, want := stmt.Parameters["evidence_source"], "reducer/sql-relationships"; got != want {
		t.Fatalf("evidence_source = %v, want %v", got, want)
	}
}

func TestEdgeWriterRetractEdgesSQLRelationshipDeltaUsesFileScopedGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
				"delta_file_paths": []string{"/repo/db/schema.sql"},
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 1; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
	stmts := executor.groupCalls[0]
	if got, want := len(stmts), 6; got != want {
		t.Fatalf("group statement count = %d, want %d", got, want)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "source.repo_id IN $repo_ids") {
			t.Fatalf("delta retract cypher = %q, want no repo-wide source filter", stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "source.path IN $file_paths") {
			t.Fatalf("delta retract cypher = %q, want source.path file-scope filter", stmt.Cypher)
		}
		if _, ok := stmt.Parameters["repo_ids"]; ok {
			t.Fatalf("repo_ids unexpectedly present in delta retract parameters: %#v", stmt.Parameters)
		}
		filePaths, ok := stmt.Parameters["file_paths"].([]string)
		if !ok {
			t.Fatalf("file_paths parameter type = %T, want []string", stmt.Parameters["file_paths"])
		}
		if got, want := strings.Join(filePaths, ","), "/repo/db/schema.sql"; got != want {
			t.Fatalf("file_paths = %q, want %q", got, want)
		}
	}
}

func TestEdgeWriterRetractEdgesSQLRelationshipRejectsDeltaWithoutFilePaths(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want malformed delta scope error")
	}
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 for malformed delta scope", got)
	}
}

func assertSQLRetractStatement(
	t *testing.T,
	stmt Statement,
	sourceLabel string,
	relationshipType string,
) {
	t.Helper()

	if !strings.Contains(stmt.Cypher, "MATCH (source:"+sourceLabel+")-[rel:"+relationshipType+"]->()") {
		t.Fatalf("cypher missing scoped match for %s/%s: %s", sourceLabel, relationshipType, stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "source.repo_id IN $repo_ids") {
		t.Fatalf("cypher missing repo_id predicate: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("cypher missing evidence_source predicate: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE: %s", stmt.Cypher)
	}
}
