// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesInvokesCloudActionDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"function_id":       "content-entity:handler",
				"repo_id":           "repo-a",
				"action":            "s3:putobject",
				"action_id":         "cloud-action:s3:putobject",
				"resolution_method": "import_binding",
				"confidence":        0.90,
				"reason":            "Resolved by following an explicit import or package binding",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInvokesCloudAction, rows, "parser/aws-sdk-call"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MATCH (func:Function {uid: row.function_id})",
		"MERGE (action:CloudAction {id: row.action_id})",
		"ON CREATE SET action.action = row.action",
		"MERGE (func)-[rel:INVOKES_CLOUD_ACTION]->(action)",
		"rel.action = row.action",
		"rel.confidence = row.confidence",
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
	if got, want := row["function_id"], "content-entity:handler"; got != want {
		t.Fatalf("function_id = %v, want %v", got, want)
	}
	if got, want := row["action"], "s3:putobject"; got != want {
		t.Fatalf("action = %v, want %v", got, want)
	}
	if got, want := row["action_id"], "cloud-action:s3:putobject"; got != want {
		t.Fatalf("action_id = %v, want %v", got, want)
	}
	if got, want := row["resolution_method"], "import_binding"; got != want {
		t.Fatalf("resolution_method = %v, want %v", got, want)
	}
	if got, want := row["evidence_source"], "parser/aws-sdk-call"; got != want {
		t.Fatalf("evidence_source = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesInvokesCloudActionSkipsRowsMissingMatchFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"function_id": "", "action": "s3:putobject", "action_id": "cloud-action:s3:putobject"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"function_id": "f1", "action": "", "action_id": "cloud-action:s3:putobject"}},
		{IntentID: "i3", RepositoryID: "r1", Payload: map[string]any{"function_id": "f1", "action": "s3:putobject", "action_id": ""}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInvokesCloudAction, rows, "parser/aws-sdk-call"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", got)
	}
}

func TestEdgeWriterRetractEdgesInvokesCloudActionDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainInvokesCloudAction, rows, "parser/aws-sdk-call"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "INVOKES_CLOUD_ACTION") {
		t.Fatalf("cypher missing INVOKES_CLOUD_ACTION: %s", cypher)
	}
	if !strings.Contains(cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE rel: %s", cypher)
	}
	if !strings.Contains(cypher, "repo_id") {
		t.Fatalf("retract must scope by Function.repo_id: %s", cypher)
	}
}
