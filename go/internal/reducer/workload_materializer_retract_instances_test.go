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

	err := m.RetractInstances(
		context.Background(),
		[]string{"workload-instance:api:production"},
		[]string{"repo-api"},
		EvidenceSourceWorkloads,
	)
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

// TestWorkloadMaterializerRetractInstancesHasDeleteTimeOwnershipPredicate is
// the regression test for CRITICAL 2 (round-2 review of #5473): the retract
// Cypher matched by id ALONE, with no delete-time re-check of ownership.
// Instance ids are not repository-namespaced and the MERGE key is id-only, so
// a stale id-list computed from a pre-delete Lookup snapshot could DETACH
// DELETE another scope's freshly (re-)written node sharing the same id. The
// fix adds `WHERE i.repo_id IN $repo_ids AND i.evidence_source =
// $evidence_source` and threads repo_ids/evidence_source as query
// parameters -- mirroring retractWorkloadDependencyEdgesCypher in
// storage/cypher/canonical.go. This test proves the predicate text is present
// in the executed statement and that repo_ids/evidence_source are threaded as
// parameters on every batch call, not just the rows.
func TestWorkloadMaterializerRetractInstancesHasDeleteTimeOwnershipPredicate(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	err := m.RetractInstances(
		context.Background(),
		[]string{"workload-instance:api:production"},
		[]string{"repo-api", "repo-other"},
		EvidenceSourceWorkloads,
	)
	if err != nil {
		t.Fatalf("RetractInstances() error = %v", err)
	}

	if !containsCypher(executor.calls, "WHERE i.repo_id IN $repo_ids AND i.evidence_source = $evidence_source") {
		t.Fatal("missing delete-time ownership predicate: a stale retract decision could delete a node another scope now owns")
	}

	var found bool
	for _, call := range executor.calls {
		if !contains(call.Cypher, "DETACH DELETE i") {
			continue
		}
		found = true
		gotRepoIDs, ok := call.Parameters["repo_ids"].([]string)
		if !ok {
			t.Fatalf("repo_ids parameter has type %T, want []string", call.Parameters["repo_ids"])
		}
		if len(gotRepoIDs) != 2 || gotRepoIDs[0] != "repo-api" || gotRepoIDs[1] != "repo-other" {
			t.Fatalf("repo_ids parameter = %v, want [repo-api repo-other]", gotRepoIDs)
		}
		gotEvidenceSource, ok := call.Parameters["evidence_source"].(string)
		if !ok || gotEvidenceSource != EvidenceSourceWorkloads {
			t.Fatalf("evidence_source parameter = %#v, want %q", call.Parameters["evidence_source"], EvidenceSourceWorkloads)
		}
	}
	if !found {
		t.Fatal("no DETACH DELETE call recorded")
	}
}

func TestWorkloadMaterializerRetractInstancesEmptyIsNoop(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	if err := m.RetractInstances(context.Background(), nil, []string{"repo-api"}, EvidenceSourceWorkloads); err != nil {
		t.Fatalf("RetractInstances() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0", len(executor.calls))
	}
}

func TestWorkloadMaterializerRetractInstancesRequiresExecutor(t *testing.T) {
	t.Parallel()

	m := &WorkloadMaterializer{}
	err := m.RetractInstances(
		context.Background(),
		[]string{"workload-instance:api:production"},
		[]string{"repo-api"},
		EvidenceSourceWorkloads,
	)
	if err == nil {
		t.Fatal("RetractInstances() error = nil, want error for nil executor")
	}
}

func TestWorkloadMaterializerRetractInstancesRequiresRepoIDs(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	err := m.RetractInstances(context.Background(), []string{"workload-instance:api:production"}, nil, EvidenceSourceWorkloads)
	if err == nil {
		t.Fatal("RetractInstances() error = nil, want error for empty repo_ids: a delete-time predicate with no repo scope cannot be safely built")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0: no cypher should be issued without a repo scope", len(executor.calls))
	}
}

func TestWorkloadMaterializerRetractInstancesRequiresEvidenceSource(t *testing.T) {
	t.Parallel()

	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)

	err := m.RetractInstances(context.Background(), []string{"workload-instance:api:production"}, []string{"repo-api"}, "")
	if err == nil {
		t.Fatal("RetractInstances() error = nil, want error for empty evidence_source")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %d, want 0: no cypher should be issued without an evidence source", len(executor.calls))
	}
}
