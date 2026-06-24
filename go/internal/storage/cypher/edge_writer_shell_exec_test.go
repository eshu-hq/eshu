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
	if !strings.Contains(executor.calls[0].Cypher, "source.path IN $file_paths") {
		t.Fatalf("delta retract did not scope by source.path: %s", executor.calls[0].Cypher)
	}
}
