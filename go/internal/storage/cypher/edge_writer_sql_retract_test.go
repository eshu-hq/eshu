// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// sqlSequentialRecordingExecutor records both single-statement Execute calls
// and ExecuteGroup calls. The SQL retract must run its per-label statements
// sequentially even when the executor CAN group (the SQL sibling of #5116: on
// NornicDB v1.1.11 multiple DELETE statements in one managed transaction do
// not all apply), so the retract tests need a double that would accept a
// grouped call in order to prove the writer never issues one.
type sqlSequentialRecordingExecutor struct {
	calls      []Statement
	groupCalls [][]Statement

	// readCalls, readCandidates, and readConnected mirror recordingExecutor's
	// Run scripting (writer_test.go) so shell-exec orphan-cleanup retract
	// tests can use this double while still proving ExecuteGroup is never
	// called.
	readCalls      []Statement
	readCandidates []string
	readConnected  map[string]bool
}

func (r *sqlSequentialRecordingExecutor) Execute(_ context.Context, stmt Statement) error {
	r.calls = append(r.calls, stmt)
	return nil
}

func (r *sqlSequentialRecordingExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	cloned := make([]Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}

// Run implements OrphanSweepReader; see recordingExecutor.Run for the S1/S2
// scripting contract this mirrors.
func (r *sqlSequentialRecordingExecutor) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.readCalls = append(r.readCalls, Statement{Cypher: cypher, Parameters: params})
	if keys, ok := params["keys"].([]string); ok {
		rows := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if r.readConnected[k] {
				rows = append(rows, map[string]any{"key": k})
			}
		}
		return rows, nil
	}
	rows := make([]map[string]any, 0, len(r.readCandidates))
	for _, k := range r.readCandidates {
		rows = append(rows, map[string]any{"key": k})
	}
	return rows, nil
}

// TestSQLRelationshipRetractCoversEveryWriteEndpointLabel links the retract's
// per-source-label set (sqlRelationshipRetractSourceLabels) to every label the
// WRITE path can create a SQL relationship edge from
// (sqlRelationshipEntityLabels, the independent source of truth). Without this
// link, a label added to the write set but not the retract set would let edges
// from that label be written yet never retracted — the replay-coverage
// dashboard would stay green while production kept stale edges. It mirrors
// TestInheritanceRetractCoversEveryWriteEndpointLabel (#5120 review).
func TestSQLRelationshipRetractCoversEveryWriteEndpointLabel(t *testing.T) {
	t.Parallel()

	retractSet := make(map[string]bool, len(sqlRelationshipRetractSourceLabels))
	for _, label := range sqlRelationshipRetractSourceLabels {
		retractSet[label] = true
	}
	for label := range sqlRelationshipEntityLabels {
		if !retractSet[label] {
			t.Errorf("write-path SQL endpoint label %q has no retract statement: add it to sqlRelationshipRetractSourceLabels, or its edges are written but never retracted", label)
		}
	}
}

// TestSQLRelationshipRetractCoversEveryWriteRelationshipType links the
// retract's relationship-type disjunction (sqlRelationshipRetractRelTypes) to
// every relationship type the WRITE path accepts
// (sqlRelationshipWriteReasons, the single source of truth both write
// templates gate on). Without this link, a relationship type added to the
// write set but not the retract disjunction would let its edges be written yet
// never retracted, and the shape tests would stay green because they derive
// their expectations from the shipped retract constant.
func TestSQLRelationshipRetractCoversEveryWriteRelationshipType(t *testing.T) {
	t.Parallel()

	retractSet := make(map[string]bool)
	for _, relType := range strings.Split(sqlRelationshipRetractRelTypes, "|") {
		retractSet[relType] = true
	}
	for relType := range sqlRelationshipWriteReasons {
		if !retractSet[relType] {
			t.Errorf("write-path SQL relationship type %q is not in the retract disjunction: add it to sqlRelationshipRetractRelTypes, or its edges are written but never retracted", relType)
		}
	}
}

// TestSQLRelationshipRetractStatementsUseSingleSourceLabel guards the SQL
// sibling of the #5116 fix: the retract must emit one statement per source
// label with a single-label source anchor. On NornicDB a node-label
// disjunction matches zero rows, and on v1.1.11 an unlabeled `(source)` scan
// silently drops some source labels, so a single combined statement
// under-deletes. The live proof is
// TestReducerSQLRelationshipRetractGraphTruth in internal/replay/offlinetier.
func TestSQLRelationshipRetractStatementsUseSingleSourceLabel(t *testing.T) {
	t.Parallel()

	for _, build := range []struct {
		name   string
		stmts  []Statement
		anchor string
	}{
		{"repo", BuildRetractSQLRelationshipEdgeStatements([]string{"r"}, "reducer/sql-relationships"), "{repo_id: repo_id}"},
		{"file", BuildRetractSQLRelationshipEdgeStatementsByFilePath([]string{"p"}, "reducer/sql-relationships"), "{path: file_path}"},
	} {
		if len(build.stmts) != len(sqlRelationshipRetractSourceLabels) {
			t.Fatalf("%s: %d statements, want %d (one per source label)", build.name, len(build.stmts), len(sqlRelationshipRetractSourceLabels))
		}
		for _, label := range sqlRelationshipRetractSourceLabels {
			want := "MATCH (source:" + label + " " + build.anchor + ")-[rel:" + sqlRelationshipRetractRelTypes + "]->()"
			found := false
			for _, stmt := range build.stmts {
				if strings.Contains(stmt.Cypher, want) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: no per-label retract statement for source label %q (want %q)", build.name, label, want)
			}
		}
		for _, stmt := range build.stmts {
			if strings.Contains(stmt.Cypher, "(source)-[rel:") {
				t.Errorf("%s: unlabeled source scan reintroduced (#5116 sibling): %q", build.name, stmt.Cypher)
			}
		}
	}
}

func TestEdgeWriterRetractEdgesSQLRelationshipRunsPerLabelStatementsSequentially(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	// The SQL sibling of #5116: on NornicDB v1.1.11 multiple DELETE statements
	// in one managed transaction under-apply, so the retract must never group
	// even when the executor supports it.
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 (grouped DELETEs under-apply on NornicDB v1.1.11)", got)
	}
	if got, want := len(executor.calls), len(sqlRelationshipRetractSourceLabels); got != want {
		t.Fatalf("Execute calls = %d, want %d (one per source label)", got, want)
	}
	for i, label := range sqlRelationshipRetractSourceLabels {
		assertSQLRetractStatement(t, executor.calls[i], label)
	}
}

func TestBuildRetractSQLRelationshipEdgeStatementsUsesSharedParameters(t *testing.T) {
	t.Parallel()

	stmts := BuildRetractSQLRelationshipEdgeStatements([]string{"repo-a", "repo-b"}, "reducer/sql-relationships")
	if got, want := len(stmts), len(sqlRelationshipRetractSourceLabels); got != want {
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

func TestEdgeWriterRetractEdgesSQLRelationshipDeltaScopesToFilePaths(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
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
		t.Fatalf("ExecuteGroup calls = %d, want 0 (grouped DELETEs under-apply on NornicDB v1.1.11)", got)
	}
	if got, want := len(executor.calls), len(sqlRelationshipRetractSourceLabels); got != want {
		t.Fatalf("Execute calls = %d, want %d (one per source label)", got, want)
	}
	for _, stmt := range executor.calls {
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

	executor := &sqlSequentialRecordingExecutor{}
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
	if got := len(executor.calls) + len(executor.groupCalls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 for malformed delta scope", got)
	}
}

func assertSQLRetractStatement(
	t *testing.T,
	stmt Statement,
	sourceLabel string,
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
	if !strings.Contains(stmt.Cypher, "MATCH (source:"+sourceLabel+" {repo_id: repo_id})-[rel:"+sqlRelationshipRetractRelTypes+"]->()") {
		t.Fatalf("cypher missing inline-anchored per-label match for %s: %s", sourceLabel, stmt.Cypher)
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
