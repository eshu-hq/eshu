// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
)

func TestWorkloadMaterializerRetractInstancesIssuesDetachDelete(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	err := m.RetractInstances(context.Background(), []string{"workload-instance:api:production"})
	if err != nil {
		t.Fatalf("RetractInstances() error = %v", err)
	}
	if !containsCypher(executor.calls, "MATCH (i:WorkloadInstance {id: row.instance_id})") {
		t.Fatal("missing WorkloadInstance MATCH-by-id cypher")
	}
	if !containsCypher(executor.calls, "DETACH DELETE i") {
		t.Fatal("missing DETACH DELETE cypher: retraction must remove incident edges, not just the node")
	}
	rows := rowsForCypher(t, executor.calls, "DETACH DELETE i")
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0]["instance_id"], "workload-instance:api:production"; got != want {
		t.Fatalf("rows[0][instance_id] = %#v, want %#v", got, want)
	}
}

func TestWorkloadMaterializerRetractInstancesEmptyIsNoop(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	if err := m.RetractInstances(context.Background(), nil); err != nil {
		t.Fatalf("RetractInstances() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestWorkloadMaterializerRetractInstancesRequiresExecutor(t *testing.T) {
	t.Parallel()

	m := &WorkloadMaterializer{}
	err := m.RetractInstances(context.Background(), []string{"workload-instance:api:production"})
	if err == nil {
		t.Fatal("RetractInstances() error = nil, want error for nil executor")
	}
}
