// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesHandlesRouteDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"function_entity_id": "content-entity:gw",
				"repo_id":            "repo-a",
				"path":               "/widgets",
				"http_method":        "GET",
				"framework":          "express",
				"resolution_method":  "same_file",
				"confidence":         0.95,
				"reason":             "Resolved within the caller's file by lexical scope or unique name",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainHandlesRoute, rows, "parser/framework-routes"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MATCH (f:Function {uid: row.function_entity_id})",
		"MATCH (e:Endpoint {repo_id: row.repo_id, path: row.path})",
		"MERGE (f)-[rel:HANDLES_ROUTE]->(e)",
		"rel.http_method = row.http_method",
		"rel.confidence = row.confidence",
		"rel.reason = row.reason",
		"rel.resolution_method = row.resolution_method",
		"rel.evidence_source = row.evidence_source",
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
	if got, want := row["function_entity_id"], "content-entity:gw"; got != want {
		t.Fatalf("function_entity_id = %v, want %v", got, want)
	}
	if got, want := row["repo_id"], "repo-a"; got != want {
		t.Fatalf("repo_id = %v, want %v", got, want)
	}
	if got, want := row["path"], "/widgets"; got != want {
		t.Fatalf("path = %v, want %v", got, want)
	}
	if got, want := row["http_method"], "GET"; got != want {
		t.Fatalf("http_method = %v, want %v", got, want)
	}
	if got, want := row["resolution_method"], "same_file"; got != want {
		t.Fatalf("resolution_method = %v, want %v", got, want)
	}
	if got, want := row["evidence_source"], "parser/framework-routes"; got != want {
		t.Fatalf("evidence_source = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesHandlesRouteSkipsRowsMissingMatchFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"function_entity_id": "", "repo_id": "r1", "path": "/x"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"function_entity_id": "f1", "repo_id": "", "path": "/x"}},
		{IntentID: "i3", RepositoryID: "r1", Payload: map[string]any{"function_entity_id": "f1", "repo_id": "r1", "path": ""}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainHandlesRoute, rows, "parser/framework-routes"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", got)
	}
}

func TestEdgeWriterRetractEdgesHandlesRouteDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainHandlesRoute, rows, "parser/framework-routes"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "HANDLES_ROUTE") {
		t.Fatalf("cypher missing HANDLES_ROUTE: %s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE rel: %s", cypher)
	}
}
