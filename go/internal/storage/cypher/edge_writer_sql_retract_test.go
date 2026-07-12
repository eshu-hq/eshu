// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterRetractEdgesSQLRelationshipUsesSequentialLabelScopedStatements(t *testing.T) {
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
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0", got)
	}
	stmts := executor.calls
	if got, want := len(stmts), 6; got != want {
		t.Fatalf("sequential statement count = %d, want %d", got, want)
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

func TestEdgeWriterRetractEdgesSQLRelationshipDeltaUsesSequentialFileScopedStatements(t *testing.T) {
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
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0", got)
	}
	stmts := executor.calls
	if got, want := len(stmts), 6; got != want {
		t.Fatalf("sequential statement count = %d, want %d", got, want)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "source.repo_id IN $repo_ids") {
			t.Fatalf("delta retract cypher = %q, want no repo-wide source filter", stmt.Cypher)
		}
		// #4708: file-scope delta retract UNWINDs $file_paths and anchors the
		// source via an inline {path: file_path} property (an index seek where the
		// source label has a path index; a cheap scan over the small SQL entity
		// labels otherwise). This asserts the query shape, not planner behavior.
		if !strings.Contains(stmt.Cypher, "UNWIND $file_paths AS file_path") {
			t.Fatalf("delta retract cypher = %q, want UNWIND of $file_paths", stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "{path: file_path})") {
			t.Fatalf("delta retract cypher = %q, want inline {path: file_path} anchor", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "source.path IN") {
			t.Fatalf("delta retract cypher = %q, still uses the slow source.path IN predicate", stmt.Cypher)
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

	// #4708: the whole-scope retract UNWINDs $repo_ids and anchors the source via
	// an inline {repo_id: repo_id} property (enabling an index seek where the
	// source label has a repo_id index, e.g. :Function) instead of a
	// `WHERE source.repo_id IN $repo_ids` predicate, which — being compound with
	// the rel predicate — defeats NornicDB's start-node index seek and full-scans
	// the label. This asserts the query shape, not planner behavior.
	if !strings.Contains(stmt.Cypher, "UNWIND $repo_ids AS repo_id") {
		t.Fatalf("cypher missing UNWIND of $repo_ids: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (source:"+sourceLabel+" {repo_id: repo_id})-[rel:"+relationshipType+"]->()") {
		t.Fatalf("cypher missing inline-anchored scoped match for %s/%s: %s", sourceLabel, relationshipType, stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "source.repo_id IN") {
		t.Fatalf("cypher still uses the slow source.repo_id IN predicate: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("cypher missing evidence_source predicate: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE: %s", stmt.Cypher)
	}
}
