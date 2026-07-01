// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterRetractEdgesRepoDependencyDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source_repo:Repository") {
		t.Fatalf("cypher missing Repository match: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "UNWIND $repo_ids AS repo_id") {
		t.Fatalf("cypher missing per-repo unwind anchor: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: repo_id})") {
		t.Fatalf("cypher missing indexed source Repository anchor: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: repo_id})-[") {
		t.Fatalf("cypher expands relationships before completing indexed Repository anchor: %s", executor.calls[0].Cypher)
	}
	anchorIndex := strings.Index(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: repo_id})")
	expansionIndex := strings.Index(executor.calls[0].Cypher, "MATCH (source_repo)-[rel:")
	if anchorIndex < 0 || expansionIndex < 0 || anchorIndex >= expansionIndex {
		t.Fatalf("cypher must anchor Repository before relationship expansion: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (repo:Repository {id: repo_id})") {
		t.Fatalf("cypher missing indexed RUNS_ON Repository anchor: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "HAS_DEPLOYMENT_EVIDENCE") {
		t.Fatalf("artifact retract cypher missing evidence edge: %s", executor.calls[1].Cypher)
	}
	if strings.Contains(executor.calls[1].Cypher, "MATCH (source_repo:Repository {id: repo_id})-[") {
		t.Fatalf("artifact retract expands relationships before completing indexed Repository anchor: %s", executor.calls[1].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "DETACH DELETE artifact") {
		t.Fatalf("artifact retract cypher missing DETACH DELETE: %s", executor.calls[1].Cypher)
	}
}
