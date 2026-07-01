// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesRepoDependencyDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":        "repo-a",
				"target_repo_id": "repo-b",
				"evidence_type":  "docker_compose_depends_on",
				"resolved_id":    "resolved-depends-on-1",
				"generation_id":  "gen-1",
				"evidence_count": 2,
				"evidence_kinds": []string{"DOCKER_COMPOSE_DEPENDS_ON"},
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DEPENDS_ON") {
		t.Fatalf("cypher missing DEPENDS_ON: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MERGE (source_repo:Repository {id: row.repo_id})") {
		t.Fatalf("cypher missing source Repository MERGE: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MERGE (target_repo:Repository {id: row.target_repo_id})") {
		t.Fatalf("cypher missing target Repository MERGE: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "rel.evidence_type = row.evidence_type") {
		t.Fatalf("cypher missing evidence_type write: %s", executor.calls[0].Cypher)
	}
	rowsOut, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rowsOut) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := rowsOut[0]["evidence_type"], "docker_compose_depends_on"; got != want {
		t.Fatalf("row evidence_type = %v, want %v", got, want)
	}
	if got, want := rowsOut[0]["resolved_id"], "resolved-depends-on-1"; got != want {
		t.Fatalf("row resolved_id = %v, want %v", got, want)
	}
	if got, want := rowsOut[0]["generation_id"], "gen-1"; got != want {
		t.Fatalf("row generation_id = %v, want %v", got, want)
	}
	if got, want := rowsOut[0]["evidence_count"], 2; got != want {
		t.Fatalf("row evidence_count = %v, want %v", got, want)
	}
	if got, want := rowsOut[0]["evidence_kinds"], []string{"DOCKER_COMPOSE_DEPENDS_ON"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("row evidence_kinds = %#v, want %#v", got, want)
	}
}

func TestEdgeWriterWriteEdgesWorkloadDependencyDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"workload_id":        "wl-a",
				"target_workload_id": "wl-b",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainWorkloadDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DEPENDS_ON") {
		t.Fatalf("cypher missing DEPENDS_ON: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source:Workload") {
		t.Fatalf("cypher missing Workload match: %s", executor.calls[0].Cypher)
	}
}

func TestEdgeWriterWriteEdgesCodeCallDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"caller_entity_id": "entity:function:caller",
				"callee_entity_id": "entity:function:callee",
				"call_kind":        "jsx_component",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "REFERENCES") {
		t.Fatalf("cypher missing REFERENCES edge: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "CALLS") {
		t.Fatalf("cypher unexpectedly included CALLS edge: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "UNWIND") {
		t.Fatalf("cypher missing UNWIND: %s", executor.calls[0].Cypher)
	}
	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := batchRows[0]["caller_entity_id"], "entity:function:caller"; got != want {
		t.Fatalf("caller_entity_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["call_kind"], "jsx_component"; got != want {
		t.Fatalf("call_kind = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesGoTypeReferenceDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"caller_entity_id":  "entity:function:caller",
				"callee_entity_id":  "entity:struct:callee",
				"relationship_type": "REFERENCES",
				"call_kind":         "go.composite_literal_type_reference",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "REFERENCES") {
		t.Fatalf("cypher missing REFERENCES edge: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "target:Function|Class|Struct|Interface|TypeAlias|File") {
		t.Fatalf("cypher missing type target labels: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source:Function|Class|Struct|Interface|TypeAlias|File") {
		t.Fatalf("cypher missing type source labels: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "MERGE (source)-[rel:CALLS]") {
		t.Fatalf("cypher unexpectedly included CALLS edge: %s", executor.calls[0].Cypher)
	}
}

func TestEdgeWriterWriteEdgesDirectCodeCallDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"caller_entity_id": "entity:function:caller",
				"callee_entity_id": "entity:function:callee",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "CALLS") {
		t.Fatalf("cypher missing CALLS edge: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "REFERENCES") {
		t.Fatalf("cypher unexpectedly included REFERENCES edge: %s", executor.calls[0].Cypher)
	}
}

func TestEdgeWriterWriteEdgesPythonMetaclassDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"source_entity_id":  "entity:class:logged",
				"target_entity_id":  "entity:class:meta",
				"relationship_type": "USES_METACLASS",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/python-metaclass")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "USES_METACLASS") {
		t.Fatalf("cypher missing USES_METACLASS edge: %s", executor.calls[0].Cypher)
	}
	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := batchRows[0]["source_entity_id"], "entity:class:logged"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["target_entity_id"], "entity:class:meta"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := batchRows[0]["relationship_type"], "USES_METACLASS"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesMultipleRowsBatched(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-b"}},
		{IntentID: "i2", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-c"}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d (batched)", got, want)
	}
	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatal("expected rows parameter to be []map[string]any")
	}
	if got, want := len(batchRows), 2; got != want {
		t.Fatalf("batch rows = %d, want %d", got, want)
	}
}

func TestEdgeWriterWriteEdgesEmptyRowsIsNoop(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, nil, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestEdgeWriterWriteEdgesUnknownDomainReturnsError(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", Payload: map[string]any{}},
	}

	err := writer.WriteEdges(context.Background(), "unknown_domain", rows, "finalization/workloads")
	if err == nil {
		t.Fatal("WriteEdges() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "unsupported domain") {
		t.Fatalf("error = %q, want 'unsupported domain'", err.Error())
	}
}

func TestEdgeWriterWriteEdgesPropagatesExecutorError(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{errAtCall: errors.New("neo4j timeout")}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a", "target_repo_id": "repo-b"}},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err == nil {
		t.Fatal("WriteEdges() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "neo4j timeout") {
		t.Fatalf("error = %q, want executor error propagated", err.Error())
	}
}

func TestEdgeWriterRetractEdgesCodeCallDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "CALLS|REFERENCES") {
		t.Fatalf("cypher missing CALLS|REFERENCES retract: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source.repo_id IN $repo_ids") {
		t.Fatalf("cypher missing repo_id filter: %s", executor.calls[0].Cypher)
	}
}

func TestEdgeWriterRetractEdgesCodeCallDeltaScopesToFilePaths(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"delta_projection":  true,
				"delta_file_paths":  []string{"/repo/src/changed.go", "/repo/src/deleted.go"},
				"caller_entity_id":  "entity:function:caller",
				"callee_entity_id":  "entity:function:callee",
				"evidence_source":   "parser/code-calls",
				"relationship_type": "CALLS",
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	call := executor.calls[0]
	if strings.Contains(call.Cypher, "source.repo_id IN $repo_ids") {
		t.Fatalf("delta retract cypher = %q, want no repo-wide source filter", call.Cypher)
	}
	if !strings.Contains(call.Cypher, "source.path IN $file_paths") {
		t.Fatalf("delta retract cypher = %q, want source.path file-scope filter", call.Cypher)
	}
	gotPaths, ok := call.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths type = %T, want []string", call.Parameters["file_paths"])
	}
	wantPaths := []string{"/repo/src/changed.go", "/repo/src/deleted.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if _, ok := call.Parameters["repo_ids"]; ok {
		t.Fatalf("repo_ids unexpectedly present in delta retract parameters: %#v", call.Parameters)
	}
}

func TestEdgeWriterRetractEdgesCodeCallRejectsDeltaWithoutFilePaths(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want malformed delta scope error")
	}
	if got := len(executor.calls); got != 0 {
		t.Fatalf("executor calls = %d, want 0 for malformed delta scope", got)
	}
}

func TestEdgeWriterRetractEdgesEmptyRowsIsNoop(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	err := writer.RetractEdges(context.Background(), reducer.DomainWorkloadDependency, nil, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestEdgeWriterRetractEdgesUnknownDomainReturnsError(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), "unknown_domain", rows, "finalization/workloads")
	if err == nil {
		t.Fatal("RetractEdges() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "unsupported domain") {
		t.Fatalf("error = %q, want 'unsupported domain'", err.Error())
	}
}
