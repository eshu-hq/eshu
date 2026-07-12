// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestInheritanceRetractStatementsUseSingleChildLabel guards the #5116/#4367 fix:
// the inheritance retract must emit one statement per child label with a
// single-label `(child:Label)` anchor. On NornicDB a node-label disjunction
// matches zero rows, and on v1.1.11 an unlabeled `(child)` scan drops some
// labels, so a single combined statement under-deletes. The live proof is
// TestReducerInheritanceEdgeRetractGraphTruth in internal/replay/offlinetier.
func TestInheritanceRetractStatementsUseSingleChildLabel(t *testing.T) {
	t.Parallel()

	for _, build := range []struct {
		name  string
		stmts []Statement
	}{
		{"repo", BuildRetractInheritanceEdgeStatements([]string{"r"}, "reducer/inheritance")},
		{"file", BuildRetractInheritanceEdgeStatementsByFilePath([]string{"p"}, "reducer/inheritance")},
	} {
		if len(build.stmts) != len(inheritanceRetractChildLabels) {
			t.Fatalf("%s: %d statements, want %d (one per child label)", build.name, len(build.stmts), len(inheritanceRetractChildLabels))
		}
		for _, label := range inheritanceRetractChildLabels {
			want := "MATCH (child:" + label + ")-[rel:INHERITS|OVERRIDES|ALIASES|IMPLEMENTS]->()"
			found := false
			for _, stmt := range build.stmts {
				if strings.Contains(stmt.Cypher, want) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: no per-label retract statement for child label %q (want %q)", build.name, label, want)
			}
		}
		for _, stmt := range build.stmts {
			if strings.Contains(stmt.Cypher, "(child)-[rel:") {
				t.Errorf("%s: unlabeled child scan reintroduced (#5116): %q", build.name, stmt.Cypher)
			}
		}
	}
}

func TestBuildRetractInheritanceEdgeStatements(t *testing.T) {
	t.Parallel()

	stmts := BuildRetractInheritanceEdgeStatements([]string{"repo-1"}, "reducer/inheritance")
	if len(stmts) != len(inheritanceRetractChildLabels) {
		t.Fatalf("statements = %d, want %d", len(stmts), len(inheritanceRetractChildLabels))
	}
	for _, stmt := range stmts {
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
		}
		if !strings.Contains(stmt.Cypher, "child.repo_id IN $repo_ids") {
			t.Fatalf("cypher = %q, want repo_id filter", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "child.path IN $file_paths") {
			t.Fatalf("cypher = %q, want no file-path filter", stmt.Cypher)
		}
		gotRepos, ok := stmt.Parameters["repo_ids"].([]string)
		if !ok || !reflect.DeepEqual(gotRepos, []string{"repo-1"}) {
			t.Fatalf("repo_ids = %#v", stmt.Parameters["repo_ids"])
		}
	}
}

func TestBuildRetractInheritanceEdgeStatementsByFilePath(t *testing.T) {
	t.Parallel()

	stmts := BuildRetractInheritanceEdgeStatementsByFilePath([]string{"/repo/src/child.go"}, "reducer/inheritance")
	if len(stmts) != len(inheritanceRetractChildLabels) {
		t.Fatalf("statements = %d, want %d", len(stmts), len(inheritanceRetractChildLabels))
	}
	for _, stmt := range stmts {
		if !strings.Contains(stmt.Cypher, "child.path IN $file_paths") {
			t.Fatalf("cypher = %q, want child.path file-scope filter", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "child.repo_id IN $repo_ids") {
			t.Fatalf("cypher = %q, want no repo-wide child filter", stmt.Cypher)
		}
		if _, ok := stmt.Parameters["repo_ids"]; ok {
			t.Fatalf("repo_ids unexpectedly present: %#v", stmt.Parameters)
		}
		gotPaths, ok := stmt.Parameters["file_paths"].([]string)
		if !ok || !reflect.DeepEqual(gotPaths, []string{"/repo/src/child.go"}) {
			t.Fatalf("file_paths = %#v", stmt.Parameters["file_paths"])
		}
	}
}

func TestEdgeWriterRetractEdgesInheritanceDeltaUsesFileScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
				"delta_file_paths": []string{"/repo/src/child.go"},
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	// One statement per child label (#5116/#4367), each in its own call (the
	// recordingExecutor is not a GroupExecutor, so they run sequentially).
	if got, want := len(executor.calls), len(inheritanceRetractChildLabels); got != want {
		t.Fatalf("executor calls = %d, want %d (one per child label)", got, want)
	}
	for _, stmt := range executor.calls {
		if strings.Contains(stmt.Cypher, "child.repo_id IN $repo_ids") {
			t.Fatalf("delta retract cypher = %q, want no repo-wide child filter", stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "child.path IN $file_paths") {
			t.Fatalf("delta retract cypher = %q, want child.path file-scope filter", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "(child)-[rel:") {
			t.Fatalf("delta retract cypher uses unlabeled child scan: %q", stmt.Cypher)
		}
		if _, ok := stmt.Parameters["repo_ids"]; ok {
			t.Fatalf("repo_ids unexpectedly present in delta retract parameters: %#v", stmt.Parameters)
		}
		filePaths, ok := stmt.Parameters["file_paths"].([]string)
		if !ok || strings.Join(filePaths, ",") != "/repo/src/child.go" {
			t.Fatalf("file_paths = %#v", stmt.Parameters["file_paths"])
		}
	}
}

func TestEdgeWriterRetractEdgesInheritanceRepoScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), len(inheritanceRetractChildLabels); got != want {
		t.Fatalf("executor calls = %d, want %d (one per child label)", got, want)
	}
	for _, stmt := range executor.calls {
		if !strings.Contains(stmt.Cypher, "child.repo_id IN $repo_ids") {
			t.Fatalf("repo retract cypher = %q, want repo_id filter", stmt.Cypher)
		}
	}
}

func TestEdgeWriterRetractEdgesInheritanceRejectsDeltaWithoutFilePaths(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
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

	err := writer.RetractEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want malformed delta scope error")
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 for malformed delta scope", got)
	}
}
