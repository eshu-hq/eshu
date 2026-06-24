// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesRunsInDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"function_id":       "content-entity:gw",
				"repo_id":           "repo-a",
				"resolution_method": "same_file",
				"confidence":        0.5,
				"ambiguous":         true,
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainRunsIn, rows, "reducer/runs-in"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MATCH (func:Function {uid: row.function_id})",
		"MATCH (repo:Repository {id: row.repo_id})-[:DEFINES]->(workload:Workload)",
		"MERGE (func)-[rel:RUNS_IN]->(workload)",
		"rel.confidence = row.confidence",
		"rel.resolution_method = row.resolution_method",
		"rel.evidence_source = row.evidence_source",
		"rel.ambiguous = row.ambiguous",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}

	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	row := batchRows[0]
	if got, want := row["function_id"], "content-entity:gw"; got != want {
		t.Fatalf("function_id = %v, want %v", got, want)
	}
	if got, want := row["repo_id"], "repo-a"; got != want {
		t.Fatalf("repo_id = %v, want %v", got, want)
	}
	if got, want := row["resolution_method"], "same_file"; got != want {
		t.Fatalf("resolution_method = %v, want %v", got, want)
	}
	if got, want := row["evidence_source"], "reducer/runs-in"; got != want {
		t.Fatalf("evidence_source = %v, want %v", got, want)
	}
	if got, want := row["ambiguous"], true; got != want {
		t.Fatalf("ambiguous = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesRunsInSkipsRowsMissingMatchFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"function_id": "", "repo_id": "r1"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"function_id": "f1", "repo_id": ""}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainRunsIn, rows, "reducer/runs-in"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", got)
	}
}

func TestEdgeWriterRetractEdgesRunsInDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRunsIn, rows, "reducer/runs-in"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "RUNS_IN") {
		t.Fatalf("cypher missing RUNS_IN: %s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE rel: %s", cypher)
	}
}
