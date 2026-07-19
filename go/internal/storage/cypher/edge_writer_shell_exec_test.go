// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
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

	executor := &recordingExecutor{
		readCandidates: []string{"shell-command:abc123"},
		readConnected:  map[string]bool{},
	}
	writer := NewEdgeWriter(executor, 0)
	writer.Reader = executor

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

	// Edge delete, then the anti-join's S3 delete-by-uid write (the orphan
	// candidate has no connected keys, so it is deleted).
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (target:ShellCommand {path: file_path})") {
		t.Fatalf("delta retract did not anchor by target.path: %s", executor.calls[0].Cypher)
	}
	assertShellExecRetractScopesEvidenceSource(t, executor.calls[0], "file_paths")
	assertShellExecDeleteByUIDStatement(t, executor.calls[1], []string{"shell-command:abc123"})

	// S1 candidate read, then S2 connected-keys read.
	if got, want := len(executor.readCalls), 2; got != want {
		t.Fatalf("reader calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.readCalls[0].Cypher, "MATCH (target:ShellCommand {path: file_path})") {
		t.Fatalf("S1 candidate read did not anchor by target.path: %s", executor.readCalls[0].Cypher)
	}
	assertShellExecRetractScopesEvidenceSource(t, executor.readCalls[0], "file_paths")
	if !strings.Contains(executor.readCalls[1].Cypher, "-[r]-(m)") {
		t.Fatalf("S2 connected read missing concrete relationship variable: %s", executor.readCalls[1].Cypher)
	}
}

func TestEdgeWriterRetractEdgesShellExecCleanupPreservesConnectedCandidate(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{
		readCandidates: []string{"shell-command:connected", "shell-command:orphan"},
		readConnected:  map[string]bool{"shell-command:connected": true},
	}
	writer := NewEdgeWriter(executor, 0)
	writer.Reader = executor

	rows := []reducer.SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload:      map[string]any{"repo_id": "repo-a"},
		},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainShellExec, rows, "reducer/shell-exec"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}

	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	assertShellExecDeleteByUIDStatement(t, executor.calls[1], []string{"shell-command:orphan"})
}

func TestEdgeWriterRetractEdgesShellExecCleanupSkipsWriteWhenAllConnected(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{
		readCandidates: []string{"shell-command:connected"},
		readConnected:  map[string]bool{"shell-command:connected": true},
	}
	writer := NewEdgeWriter(executor, 0)
	writer.Reader = executor

	rows := []reducer.SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload:      map[string]any{"repo_id": "repo-a"},
		},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainShellExec, rows, "reducer/shell-exec"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}

	// Only the edge delete: every candidate is connected, so the anti-join
	// issues no delete-by-uid write.
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d (no write should fire when every candidate is connected)", got, want)
	}
}

func TestEdgeWriterRetractEdgesShellExecCleanupSkipsReadsWhenNoCandidates(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	writer.Reader = executor

	rows := []reducer.SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload:      map[string]any{"repo_id": "repo-a"},
		},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainShellExec, rows, "reducer/shell-exec"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}

	if got, want := len(executor.readCalls), 1; got != want {
		t.Fatalf("reader calls = %d, want %d (S1 empty should skip S2)", got, want)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d (S1 empty should skip the delete write)", got, want)
	}
}

func TestEdgeWriterRetractEdgesShellExecRequiresReader(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	// writer.Reader intentionally left nil.

	rows := []reducer.SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload:      map[string]any{"repo_id": "repo-a"},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainShellExec, rows, "reducer/shell-exec")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want reader-required error")
	}
	if !strings.Contains(err.Error(), "reader is required") {
		t.Fatalf("error = %v, want reader-required error", err)
	}
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

// TestShellCommandConnectedKeysQueryUsesConcreteRelationshipVariable pins the
// S2 anti-join read shape: a concrete relationship variable anchored on
// caller-supplied keys, never a relationship-existence predicate (#5310; see
// docs/public/reference/nornicdb-pitfalls.md).
func TestShellCommandConnectedKeysQueryUsesConcreteRelationshipVariable(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"UNWIND $keys AS candidate_key",
		"MATCH (target:ShellCommand {uid: candidate_key})-[r]-(m)",
		"RETURN DISTINCT target.uid AS key",
	} {
		if !strings.Contains(shellCommandConnectedKeysCypher, want) {
			t.Fatalf("shellCommandConnectedKeysCypher = %q, want %q", shellCommandConnectedKeysCypher, want)
		}
	}
	for _, unwanted := range []string{"NOT (", "COUNT {", "DETACH DELETE"} {
		if strings.Contains(shellCommandConnectedKeysCypher, unwanted) {
			t.Fatalf("shellCommandConnectedKeysCypher = %q, must not contain %q", shellCommandConnectedKeysCypher, unwanted)
		}
	}
}

// TestShellCommandCandidateKeysQueriesCarryNoRelationshipPredicate pins the S1
// reads: every in-scope ShellCommand uid, no relationship clause at all.
func TestShellCommandCandidateKeysQueriesCarryNoRelationshipPredicate(t *testing.T) {
	t.Parallel()

	for _, cypher := range []string{shellCommandCandidateKeysByRepoCypher, shellCommandCandidateKeysByFileCypher} {
		if !strings.Contains(cypher, "target.evidence_source = $evidence_source") {
			t.Fatalf("cypher = %q, want evidence_source predicate", cypher)
		}
		if !strings.Contains(cypher, "RETURN DISTINCT target.uid AS key") {
			t.Fatalf("cypher = %q, want target.uid key projection", cypher)
		}
		for _, unwanted := range []string{"--()", "NOT (", "COUNT {", "-[r]-"} {
			if strings.Contains(cypher, unwanted) {
				t.Fatalf("cypher = %q, must not contain relationship clause %q", cypher, unwanted)
			}
		}
	}
}

// TestShellCommandDeleteByUIDStatementIsKeyAnchoredNoDetach pins the S3 write:
// key-anchored DELETE, never DETACH DELETE.
func TestShellCommandDeleteByUIDStatementIsKeyAnchoredNoDetach(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"UNWIND $keys AS candidate_key",
		"MATCH (target:ShellCommand {uid: candidate_key})",
		"DELETE target",
	} {
		if !strings.Contains(deleteShellCommandsByUIDCypher, want) {
			t.Fatalf("deleteShellCommandsByUIDCypher = %q, want %q", deleteShellCommandsByUIDCypher, want)
		}
	}
	if strings.Contains(deleteShellCommandsByUIDCypher, "DETACH DELETE") {
		t.Fatalf("deleteShellCommandsByUIDCypher = %q, want orphan-only DELETE not DETACH DELETE", deleteShellCommandsByUIDCypher)
	}
}

func TestEdgeWriterRetractEdgesShellExecRunsSequentialOrderedCleanup(t *testing.T) {
	t.Parallel()

	executor := &sqlSequentialRecordingExecutor{
		readCandidates: []string{"shell-command:abc123"},
		readConnected:  map[string]bool{},
	}
	writer := NewEdgeWriter(executor, 0)
	writer.Reader = executor

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
	// The shell-exec retract runs its edge delete and its anti-join's S3
	// delete-by-uid write sequentially: edge retract first so the orphan
	// cleanup's S1/S2 reads see the detached ShellCommand nodes; grouped
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
	assertShellExecDeleteByUIDStatement(t, stmts[1], []string{"shell-command:abc123"})
}

func assertShellExecRetractScopesEvidenceSource(t *testing.T, stmt Statement, scopeParam string) {
	t.Helper()

	if !strings.Contains(stmt.Cypher, "evidence_source = $evidence_source") {
		t.Fatalf("cypher = %q, want evidence_source predicate", stmt.Cypher)
	}
	if got, want := stmt.Parameters["evidence_source"], "reducer/shell-exec"; got != want {
		t.Fatalf("evidence_source = %#v, want %#v", got, want)
	}
	if _, ok := stmt.Parameters[scopeParam]; !ok {
		t.Fatalf("%s parameter missing: %#v", scopeParam, stmt.Parameters)
	}
}

func assertShellExecDeleteByUIDStatement(t *testing.T, stmt Statement, wantKeys []string) {
	t.Helper()

	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	if !strings.Contains(stmt.Cypher, "DELETE target") {
		t.Fatalf("cleanup cypher = %q, want DELETE target", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "DETACH DELETE") {
		t.Fatalf("cleanup cypher = %q, want orphan-only DELETE not DETACH DELETE", stmt.Cypher)
	}
	keys, ok := stmt.Parameters["keys"].([]string)
	if !ok {
		t.Fatalf("keys parameter missing or wrong type: %#v", stmt.Parameters)
	}
	if got := keys; !equalStringSlices(got, wantKeys) {
		t.Fatalf("keys = %#v, want %#v", got, wantKeys)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCleanupOrphanShellCommandsChunksConnectedRead proves the S2 connected-keys
// read is chunked at shellCommandConnectedKeysChunkSize, not sent as one
// round trip -- the anchored read scales super-linearly on NornicDB (#5147).
func TestCleanupOrphanShellCommandsChunksConnectedRead(t *testing.T) {
	t.Parallel()

	candidates := make([]string, 1200) // 3 chunks of 500/500/200
	for i := range candidates {
		candidates[i] = fmt.Sprintf("shell-command:%04d", i)
	}
	executor := &recordingExecutor{readCandidates: candidates, readConnected: map[string]bool{}}
	writer := NewEdgeWriter(executor, 0)
	writer.Reader = executor

	if err := writer.cleanupOrphanShellCommands(
		context.Background(),
		shellCommandCandidateKeysByRepoCypher,
		map[string]any{"repo_ids": []string{"repo-a"}, "evidence_source": "es"},
	); err != nil {
		t.Fatalf("cleanupOrphanShellCommands error = %v", err)
	}

	var s2Reads int
	for _, c := range executor.readCalls {
		keys, ok := c.Parameters["keys"].([]string)
		if !ok {
			continue // S1 candidate read (no "keys" param)
		}
		s2Reads++
		if len(keys) > shellCommandConnectedKeysChunkSize {
			t.Fatalf("S2 chunk carried %d keys, want <= %d", len(keys), shellCommandConnectedKeysChunkSize)
		}
	}
	if want := 3; s2Reads != want {
		t.Fatalf("S2 connected-keys reads = %d, want %d (1200 keys chunked at %d)", s2Reads, want, shellCommandConnectedKeysChunkSize)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("delete Execute calls = %d, want 1", len(executor.calls))
	}
	delKeys, _ := executor.calls[0].Parameters["keys"].([]string)
	if len(delKeys) != 1200 {
		t.Fatalf("delete carried %d orphan keys, want 1200 (all candidates were orphan)", len(delKeys))
	}
}
