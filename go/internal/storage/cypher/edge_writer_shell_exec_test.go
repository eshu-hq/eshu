// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesShellExec(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"source_entity_id":   "function:archive",
				"target_entity_id":   "shell-command:abc123",
				"source_entity_type": "Function",
				"target_entity_type": "ShellCommand",
				"repo_id":            "repo-a",
				"source_path":        "/repo/cmd/archive/main.go",
				"line_number":        8,
				"api":                "os/exec.CommandContext",
				"language":           "go",
				"relationship_type":  "EXECUTES_SHELL",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainShellExec, rows, "reducer/shell-exec"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (source:Function {uid: row.source_entity_id})",
		"MERGE (target:ShellCommand {uid: row.target_entity_id})",
		"MERGE (source)-[rel:EXECUTES_SHELL]->(target)",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q: %s", want, cypher)
		}
	}
	params := executor.calls[0].Parameters["rows"].([]map[string]any)[0]
	if _, ok := params["command"]; ok {
		t.Fatalf("shell exec row persisted raw command text: %#v", params)
	}
}

func TestEdgeWriterRetractEdgesShellExecDeltaUsesFileScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":            "repo-a",
				"delta_projection":   true,
				"delta_file_paths":   []any{"/repo/cmd/archive/main.go"},
				"target_entity_id":   "shell-command:abc123",
				"source_entity_id":   "function:archive",
				"relationship_type":  "EXECUTES_SHELL",
				"source_entity_type": "Function",
				"target_entity_type": "ShellCommand",
			},
		},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainShellExec, rows, "reducer/shell-exec"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (source:Function {path: file_path})") {
		t.Fatalf("delta retract did not anchor by source.path: %s", executor.calls[0].Cypher)
	}
}

func TestBuildRetractShellExecEdgesUsesRepoAnchoredFunctionLookup(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractShellExecEdges([]string{"repo-a"}, "reducer/shell-exec")
	if !strings.Contains(stmt.Cypher, "UNWIND $repo_ids AS repo_id") {
		t.Fatalf("cypher = %q, want repo_id unwind", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (source:Function {repo_id: repo_id})") {
		t.Fatalf("cypher = %q, want indexed Function repo_id anchor", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (source)-[rel:EXECUTES_SHELL]->()") {
		t.Fatalf("cypher = %q, want source-bound EXECUTES_SHELL expansion", stmt.Cypher)
	}
	if strings.HasPrefix(strings.TrimSpace(stmt.Cypher), "MATCH (source)-[rel:") {
		t.Fatalf("cypher starts from unbound relationship scan: %q", stmt.Cypher)
	}
}

func TestBuildRetractShellExecEdgesByFilePathUsesPathAnchoredFunctionLookup(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractShellExecEdgesByFilePath([]string{"/repo/cmd/archive/main.go"}, "reducer/shell-exec")
	if !strings.Contains(stmt.Cypher, "UNWIND $file_paths AS file_path") {
		t.Fatalf("cypher = %q, want file path unwind", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (source:Function {path: file_path})") {
		t.Fatalf("cypher = %q, want indexed Function path anchor", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (source)-[rel:EXECUTES_SHELL]->()") {
		t.Fatalf("cypher = %q, want source-bound EXECUTES_SHELL expansion", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "source.path IN $file_paths") {
		t.Fatalf("cypher = %q, want bound path lookup rather than post-match IN filter", stmt.Cypher)
	}
	if strings.HasPrefix(strings.TrimSpace(stmt.Cypher), "MATCH (source)-[rel:") {
		t.Fatalf("cypher starts from unbound relationship scan: %q", stmt.Cypher)
	}
}
