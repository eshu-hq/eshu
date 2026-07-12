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
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (target:ShellCommand {path: file_path})") {
		t.Fatalf("delta retract did not anchor by target.path: %s", executor.calls[0].Cypher)
	}
	assertShellExecRetractScopesEvidenceSource(t, executor.calls[0], "file_paths")
	assertShellExecCleanupStatement(t, executor.calls[1], "file_paths", "MATCH (target:ShellCommand {path: file_path})")
}

func TestBuildRetractShellExecEdgesUsesRepoAnchoredShellCommandLookup(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractShellExecEdges([]string{"repo-a"}, "reducer/shell-exec")
	if !strings.Contains(stmt.Cypher, "UNWIND $repo_ids AS repo_id") {
		t.Fatalf("cypher = %q, want repo_id unwind", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (target:ShellCommand {repo_id: repo_id})") {
		t.Fatalf("cypher = %q, want indexed ShellCommand repo_id anchor", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH ()-[rel:EXECUTES_SHELL]->(target)") {
		t.Fatalf("cypher = %q, want target-bound EXECUTES_SHELL expansion", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MATCH (source:Function {repo_id: repo_id})") {
		t.Fatalf("cypher = %q, want ShellCommand anchor instead of Function fan-out", stmt.Cypher)
	}
	if strings.HasPrefix(strings.TrimSpace(stmt.Cypher), "MATCH ()-[rel:") {
		t.Fatalf("cypher starts from unbound relationship scan: %q", stmt.Cypher)
	}
	assertShellExecRetractScopesEvidenceSource(t, stmt, "repo_ids")
}

func TestBuildRetractShellExecEdgesByFilePathUsesPathAnchoredShellCommandLookup(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractShellExecEdgesByFilePath([]string{"/repo/cmd/archive/main.go"}, "reducer/shell-exec")
	if !strings.Contains(stmt.Cypher, "UNWIND $file_paths AS file_path") {
		t.Fatalf("cypher = %q, want file path unwind", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (target:ShellCommand {path: file_path})") {
		t.Fatalf("cypher = %q, want indexed ShellCommand path anchor", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH ()-[rel:EXECUTES_SHELL]->(target)") {
		t.Fatalf("cypher = %q, want target-bound EXECUTES_SHELL expansion", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "source.path IN $file_paths") {
		t.Fatalf("cypher = %q, want bound path lookup rather than post-match IN filter", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MATCH (source:Function {path: file_path})") {
		t.Fatalf("cypher = %q, want ShellCommand anchor instead of Function fan-out", stmt.Cypher)
	}
	if strings.HasPrefix(strings.TrimSpace(stmt.Cypher), "MATCH ()-[rel:") {
		t.Fatalf("cypher starts from unbound relationship scan: %q", stmt.Cypher)
	}
	assertShellExecRetractScopesEvidenceSource(t, stmt, "file_paths")
}

func TestBuildCleanupOrphanShellCommandsUsesRepoAnchor(t *testing.T) {
	t.Parallel()

	stmt := BuildCleanupOrphanShellCommands([]string{"repo-a"}, "reducer/shell-exec")
	assertShellExecCleanupStatement(t, stmt, "repo_ids", "MATCH (target:ShellCommand {repo_id: repo_id})")
}

func TestBuildCleanupOrphanShellCommandsByFilePathUsesPathAnchor(t *testing.T) {
	t.Parallel()

	stmt := BuildCleanupOrphanShellCommandsByFilePath(
		[]string{"/repo/cmd/archive/main.go"},
		"reducer/shell-exec",
	)
	assertShellExecCleanupStatement(t, stmt, "file_paths", "MATCH (target:ShellCommand {path: file_path})")
}

func TestEdgeWriterRetractEdgesShellExecRunsSequentialOrderedCleanup(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id": "repo-a",
			},
		},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainShellExec, rows, "reducer/shell-exec"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	// The shell-exec retract runs its two statements sequentially, edge retract
	// first so the orphan cleanup sees the detached ShellCommand nodes: grouped
	// DELETEs under-apply on NornicDB v1.1.11.
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 (grouped DELETEs under-apply on NornicDB v1.1.11)", got)
	}
	stmts := executor.calls
	if got, want := len(stmts), 2; got != want {
		t.Fatalf("sequential statement count = %d, want %d", got, want)
	}
	if !strings.Contains(stmts[0].Cypher, "MATCH ()-[rel:EXECUTES_SHELL]->(target)") {
		t.Fatalf("first statement should retract EXECUTES_SHELL relationships: %s", stmts[0].Cypher)
	}
	assertShellExecCleanupStatement(t, stmts[1], "repo_ids", "MATCH (target:ShellCommand {repo_id: repo_id})")
}

func assertShellExecRetractScopesEvidenceSource(t *testing.T, stmt Statement, scopeParam string) {
	t.Helper()

	if !strings.Contains(stmt.Cypher, "rel.evidence_source = $evidence_source") {
		t.Fatalf("cypher = %q, want rel.evidence_source predicate", stmt.Cypher)
	}
	if got, want := stmt.Parameters["evidence_source"], "reducer/shell-exec"; got != want {
		t.Fatalf("evidence_source = %#v, want %#v", got, want)
	}
	if _, ok := stmt.Parameters[scopeParam]; !ok {
		t.Fatalf("%s parameter missing: %#v", scopeParam, stmt.Parameters)
	}
}

func assertShellExecCleanupStatement(t *testing.T, stmt Statement, scopeParam string, anchor string) {
	t.Helper()

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	for _, want := range []string{
		anchor,
		"target.evidence_source = $evidence_source",
		"COUNT { (target)--() } = 0",
		"DELETE target",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("cleanup cypher = %q, want %q", stmt.Cypher, want)
		}
	}
	if strings.Contains(stmt.Cypher, "DETACH DELETE") {
		t.Fatalf("cleanup cypher = %q, want orphan-only DELETE not DETACH DELETE", stmt.Cypher)
	}
	if got, want := stmt.Parameters["evidence_source"], "reducer/shell-exec"; got != want {
		t.Fatalf("evidence_source = %#v, want %#v", got, want)
	}
	if _, ok := stmt.Parameters[scopeParam]; !ok {
		t.Fatalf("%s parameter missing: %#v", scopeParam, stmt.Parameters)
	}
}
