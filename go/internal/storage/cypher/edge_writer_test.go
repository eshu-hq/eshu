package cypher

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesPlatformInfraDispatch(t *testing.T) {
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

	err := writer.WriteEdges(context.Background(), reducer.DomainPlatformInfra, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if executor.calls[0].Operation != OperationCanonicalUpsert {
		t.Fatalf("operation = %q, want %q", executor.calls[0].Operation, OperationCanonicalUpsert)
	}
	if !strings.Contains(executor.calls[0].Cypher, "PROVISIONS_PLATFORM") {
		t.Fatalf("cypher missing PROVISIONS_PLATFORM: %s", executor.calls[0].Cypher)
	}
}

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

func TestEdgeWriterRetractEdgesPlatformInfraDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-1", Payload: map[string]any{"repo_id": "repo-1"}},
		{IntentID: "i2", RepositoryID: "repo-2", Payload: map[string]any{"repo_id": "repo-2"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainPlatformInfra, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d (retraction is batched)", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "PROVISIONS_PLATFORM") {
		t.Fatalf("cypher missing PROVISIONS_PLATFORM: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DELETE rel") {
		t.Fatalf("cypher missing DELETE: %s", executor.calls[0].Cypher)
	}
}

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
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (repo:Repository {id: repo_id})") {
		t.Fatalf("cypher missing indexed RUNS_ON Repository anchor: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "HAS_DEPLOYMENT_EVIDENCE") {
		t.Fatalf("artifact retract cypher missing evidence edge: %s", executor.calls[1].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "DETACH DELETE artifact") {
		t.Fatalf("artifact retract cypher missing DETACH DELETE: %s", executor.calls[1].Cypher)
	}
}

func TestEdgeWriterRetractEdgesWorkloadDependencyDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainWorkloadDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source:Workload") {
		t.Fatalf("cypher missing Workload match: %s", executor.calls[0].Cypher)
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

func TestEdgeWriterRetractEdgesEmptyRowsIsNoop(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	err := writer.RetractEdges(context.Background(), reducer.DomainPlatformInfra, nil, "finalization/workloads")
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
