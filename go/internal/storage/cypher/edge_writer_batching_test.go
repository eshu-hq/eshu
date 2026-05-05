package cypher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterRetractEdgesPropagatesExecutorError(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{errAtCall: errors.New("connection refused")}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error = %q, want executor error propagated", err.Error())
	}
}

func TestEdgeWriterRequiresExecutor(t *testing.T) {
	t.Parallel()

	writer := NewEdgeWriter(nil, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-b"}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err == nil {
		t.Fatal("WriteEdges() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "executor is required") {
		t.Fatalf("error = %q, want 'executor is required'", err.Error())
	}
}

func TestBatchedWriteEdgesRespectsBatchSize(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 2) // batch size = 2

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r2"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r3"}},
		{IntentID: "i3", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r4"}},
		{IntentID: "i4", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r5"}},
		{IntentID: "i5", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r6"}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	// 5 rows at batch_size=2 → 3 batches (2, 2, 1)
	if got, want := len(executor.calls), 3; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	// First two batches have 2 rows each
	for i := 0; i < 2; i++ {
		batchRows := executor.calls[i].Parameters["rows"].([]map[string]any)
		if got, want := len(batchRows), 2; got != want {
			t.Fatalf("batch %d rows = %d, want %d", i, got, want)
		}
	}
	// Last batch has 1 row
	lastRows := executor.calls[2].Parameters["rows"].([]map[string]any)
	if got, want := len(lastRows), 1; got != want {
		t.Fatalf("last batch rows = %d, want %d", got, want)
	}
}

func TestBatchedWriteEdgesDefaultBatchSize(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0) // uses DefaultBatchSize (500)

	// Create 1000 rows — should produce 2 batches of 500
	rows := make([]reducer.SharedProjectionIntentRow, 1000)
	for i := range rows {
		rows[i] = reducer.SharedProjectionIntentRow{
			IntentID:     fmt.Sprintf("i%d", i),
			RepositoryID: "r1",
			Payload:      map[string]any{"repo_id": "r1", "target_repo_id": fmt.Sprintf("r%d", i+2)},
		}
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
}

func TestBatchedWriteEdgesSkipsEmptyRequiredFields(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		// Valid row
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r2"}},
		// Missing repo_id — should be skipped
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"repo_id": "", "target_repo_id": "r3"}},
		// Missing target_repo_id — should be skipped
		{IntentID: "i3", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": ""}},
		// Valid row
		{IntentID: "i4", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r4"}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	batchRows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if got, want := len(batchRows), 2; got != want {
		t.Fatalf("batch rows = %d, want %d (invalid rows filtered)", got, want)
	}
}

func TestBatchedWriteEdgesAllRowsInvalidIsNoop(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"repo_id": "", "target_repo_id": ""}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 (all rows filtered)", got)
	}
}

func TestBatchedWriteEdgesParameterFidelity(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-1",
			Payload: map[string]any{
				"repo_id":              "repo-1",
				"platform_id":          "platform:eks:aws:cluster-1:prod:us-east-1",
				"platform_name":        "cluster-1",
				"platform_kind":        "eks",
				"platform_provider":    "aws",
				"platform_environment": "prod",
				"platform_region":      "us-east-1",
				"platform_locator":     "arn:aws:eks:us-east-1:123:cluster/cluster-1",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainPlatformInfra, rows, "test-evidence")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}

	batchRows := executor.calls[0].Parameters["rows"].([]map[string]any)
	row := batchRows[0]

	expectedKeys := []string{
		"repo_id", "platform_id", "platform_name", "platform_kind",
		"platform_provider", "platform_environment", "platform_region",
		"platform_locator", "evidence_source",
	}
	for _, key := range expectedKeys {
		if _, ok := row[key]; !ok {
			t.Errorf("missing key %q in row map", key)
		}
	}
	if got, want := row["evidence_source"], "test-evidence"; got != want {
		t.Errorf("evidence_source = %v, want %v", got, want)
	}
	if got, want := row["platform_name"], "cluster-1"; got != want {
		t.Errorf("platform_name = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesCodeCallsChunkManagedGroups(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 500)
	writer.CodeCallBatchSize = 2
	writer.CodeCallGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "caller-1", "callee_entity_id": "callee-1"}},
		{IntentID: "i2", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "caller-2", "callee_entity_id": "callee-2"}},
		{IntentID: "i3", RepositoryID: "repo-a", Payload: map[string]any{"caller_entity_id": "caller-3", "callee_entity_id": "callee-3"}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 2; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
	if got, want := len(executor.groupCalls[0]), 1; got != want {
		t.Fatalf("len(groupCalls[0]) = %d, want %d", got, want)
	}
	if got, want := len(executor.groupCalls[1]), 1; got != want {
		t.Fatalf("len(groupCalls[1]) = %d, want %d", got, want)
	}
	firstBatchRows := executor.groupCalls[0][0].Parameters["rows"].([]map[string]any)
	if got, want := len(firstBatchRows), 2; got != want {
		t.Fatalf("len(first batch rows) = %d, want %d", got, want)
	}
	secondBatchRows := executor.groupCalls[1][0].Parameters["rows"].([]map[string]any)
	if got, want := len(secondBatchRows), 1; got != want {
		t.Fatalf("len(second batch rows) = %d, want %d", got, want)
	}
}

func TestEdgeWriterWriteEdgesNonCodeCallsKeepSingleManagedGroup(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 2)
	writer.CodeCallBatchSize = 1
	writer.CodeCallGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r2"}},
		{IntentID: "i2", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r3"}},
		{IntentID: "i3", RepositoryID: "r1", Payload: map[string]any{"repo_id": "r1", "target_repo_id": "r4"}},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 1; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
	if got, want := len(executor.groupCalls[0]), 2; got != want {
		t.Fatalf("len(groupCalls[0]) = %d, want %d statements", got, want)
	}
}

func TestEdgeWriterWriteEdgesInheritanceChunkManagedGroups(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 2)
	writer.InheritanceGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":   "entity:class:child-1",
				"parent_entity_id":  "entity:class:parent-1",
				"repo_id":           "repo-a",
				"relationship_type": "INHERITS",
			},
		},
		{
			IntentID:     "i2",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":   "entity:class:child-2",
				"parent_entity_id":  "entity:class:parent-2",
				"repo_id":           "repo-a",
				"relationship_type": "INHERITS",
			},
		},
		{
			IntentID:     "i3",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":   "entity:class:child-3",
				"parent_entity_id":  "entity:class:parent-3",
				"repo_id":           "repo-a",
				"relationship_type": "INHERITS",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 2; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
	if got, want := len(executor.groupCalls[0]), 1; got != want {
		t.Fatalf("len(groupCalls[0]) = %d, want %d", got, want)
	}
	if got, want := len(executor.groupCalls[1]), 1; got != want {
		t.Fatalf("len(groupCalls[1]) = %d, want %d", got, want)
	}
	firstBatchRows := executor.groupCalls[0][0].Parameters["rows"].([]map[string]any)
	if got, want := len(firstBatchRows), 2; got != want {
		t.Fatalf("len(first batch rows) = %d, want %d", got, want)
	}
	secondBatchRows := executor.groupCalls[1][0].Parameters["rows"].([]map[string]any)
	if got, want := len(secondBatchRows), 1; got != want {
		t.Fatalf("len(second batch rows) = %d, want %d", got, want)
	}
}

func TestEdgeWriterWriteEdgesSQLRelationshipsChunkManagedGroups(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 2)
	writer.SQLRelationshipGroupBatchSize = 1

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"source_entity_id":  "entity:sql:view:1",
				"target_entity_id":  "entity:sql:table:1",
				"repo_id":           "repo-a",
				"relationship_type": "REFERENCES_TABLE",
			},
		},
		{
			IntentID:     "i2",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"source_entity_id":  "entity:sql:view:2",
				"target_entity_id":  "entity:sql:table:2",
				"repo_id":           "repo-a",
				"relationship_type": "REFERENCES_TABLE",
			},
		},
		{
			IntentID:     "i3",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"source_entity_id":  "entity:sql:view:3",
				"target_entity_id":  "entity:sql:table:3",
				"repo_id":           "repo-a",
				"relationship_type": "REFERENCES_TABLE",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 2; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
	if got, want := len(executor.groupCalls[0]), 1; got != want {
		t.Fatalf("len(groupCalls[0]) = %d, want %d", got, want)
	}
	if got, want := len(executor.groupCalls[1]), 1; got != want {
		t.Fatalf("len(groupCalls[1]) = %d, want %d", got, want)
	}
	firstBatchRows := executor.groupCalls[0][0].Parameters["rows"].([]map[string]any)
	if got, want := len(firstBatchRows), 2; got != want {
		t.Fatalf("len(first batch rows) = %d, want %d", got, want)
	}
	secondBatchRows := executor.groupCalls[1][0].Parameters["rows"].([]map[string]any)
	if got, want := len(secondBatchRows), 1; got != want {
		t.Fatalf("len(second batch rows) = %d, want %d", got, want)
	}
}
