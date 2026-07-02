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

func TestBuildRetractInheritanceEdgesByFilePath(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractInheritanceEdgesByFilePath([]string{"/repo/src/child.go"}, "reducer/inheritance")
	if !strings.Contains(stmt.Cypher, "UNWIND $file_paths AS file_path") {
		t.Fatalf("cypher = %q, want file path unwind", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {path: file_path})") {
		t.Fatalf("cypher = %q, want label-scoped path child anchor", stmt.Cypher)
	}
	if strings.HasPrefix(strings.TrimSpace(stmt.Cypher), "MATCH (child)-[rel:") {
		t.Fatalf("cypher starts from unbound relationship scan: %q", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "child.repo_id IN $repo_ids") {
		t.Fatalf("cypher = %q, want no repo-wide child filter", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "IMPLEMENTS") {
		t.Fatalf("cypher = %q, want IMPLEMENTS cleanup", stmt.Cypher)
	}
	if got, want := stmt.Parameters["evidence_source"], "reducer/inheritance"; got != want {
		t.Fatalf("evidence_source = %#v, want %#v", got, want)
	}
	gotPaths, ok := stmt.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths parameter type = %T, want []string", stmt.Parameters["file_paths"])
	}
	wantPaths := []string{"/repo/src/child.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("file_paths = %#v, want %#v", gotPaths, wantPaths)
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
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if strings.Contains(stmt.Cypher, "child.repo_id IN $repo_ids") {
		t.Fatalf("delta retract cypher = %q, want no repo-wide child filter", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "UNWIND $file_paths AS file_path") {
		t.Fatalf("delta retract cypher = %q, want file path unwind", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {path: file_path})") {
		t.Fatalf("delta retract cypher = %q, want label-scoped path child anchor", stmt.Cypher)
	}
	if strings.HasPrefix(strings.TrimSpace(stmt.Cypher), "MATCH (child)-[rel:") {
		t.Fatalf("delta retract cypher starts from unbound relationship scan: %q", stmt.Cypher)
	}
	if _, ok := stmt.Parameters["repo_ids"]; ok {
		t.Fatalf("repo_ids unexpectedly present in delta retract parameters: %#v", stmt.Parameters)
	}
	filePaths, ok := stmt.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths parameter type = %T, want []string", stmt.Parameters["file_paths"])
	}
	if got, want := strings.Join(filePaths, ","), "/repo/src/child.go"; got != want {
		t.Fatalf("file_paths = %q, want %q", got, want)
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
