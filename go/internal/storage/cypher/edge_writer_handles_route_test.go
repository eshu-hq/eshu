package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
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
				"repo_id":           "repo-a",
				"function_id":       "entity:listUsers",
				"path":              "/users",
				"method":            "get",
				"resolution_method": codeprovenance.MethodSameFile,
				"confidence":        codeprovenance.Confidence(codeprovenance.MethodSameFile),
				"reason":            codeprovenance.Reason(codeprovenance.MethodSameFile),
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainHandlesRoute, rows, "parser/framework-routes")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "UNWIND $rows AS row") {
		t.Fatalf("cypher missing UNWIND: %s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (func:Function {uid: row.function_id})") {
		t.Fatalf("cypher missing Function match: %s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (repo:Repository {id: row.repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint {path: row.path})") {
		t.Fatalf("cypher missing anchored Endpoint match: %s", cypher)
	}
	if !strings.Contains(cypher, "MERGE (func)-[rel:HANDLES_ROUTE]->(endpoint)") {
		t.Fatalf("cypher missing HANDLES_ROUTE MERGE: %s", cypher)
	}

	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := batchRows[0]["function_id"], "entity:listUsers"; got != want {
		t.Fatalf("function_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["repo_id"], "repo-a"; got != want {
		t.Fatalf("repo_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["path"], "/users"; got != want {
		t.Fatalf("path = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["method"], "get"; got != want {
		t.Fatalf("method = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["evidence_source"], "parser/framework-routes"; got != want {
		t.Fatalf("evidence_source = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["resolution_method"], codeprovenance.MethodSameFile; got != want {
		t.Fatalf("resolution_method = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesHandlesRouteSkipsEmptyFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"repo_id": "", "function_id": "f1", "path": "/users"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "function_id": "", "path": "/users"}},
		{IntentID: "i3", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "function_id": "f1", "path": ""}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainHandlesRoute, rows, "parser/framework-routes")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", got)
	}
}
