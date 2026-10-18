// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
)

type fakeSiblingLookup struct {
	siblings []WorkloadSibling
	names    []string
	err      error
}

func (f *fakeSiblingLookup) ListWorkloadSiblings(
	_ context.Context,
	names []string,
) ([]WorkloadSibling, error) {
	f.names = append([]string(nil), names...)
	return f.siblings, f.err
}

func sameNameProjection() *ProjectionResult {
	return &ProjectionResult{
		ScopeID:      "scope-alpha",
		GenerationID: "gen-1",
		WorkloadRows: []WorkloadRow{{
			RepoID:         "repository:r_aaaaaaaa",
			WorkloadID:     "workload:repository:r_aaaaaaaa:checkout",
			WorkloadKind:   "service",
			WorkloadName:   "checkout",
			Classification: "service",
			Confidence:     0.97,
		}},
		InstanceRows: []InstanceRow{{
			Environment:  "prod",
			InstanceID:   "workload-instance:repository:r_aaaaaaaa:checkout:prod",
			RepoID:       "repository:r_aaaaaaaa",
			WorkloadID:   "workload:repository:r_aaaaaaaa:checkout",
			WorkloadName: "checkout",
			Confidence:   0.97,
		}},
		EndpointRows: []APIEndpointRow{{
			EndpointID:   "endpoint:abc",
			RepoID:       "repository:r_aaaaaaaa",
			WorkloadID:   "workload:repository:r_aaaaaaaa:checkout",
			WorkloadName: "checkout",
			Path:         "/pay",
		}},
	}
}

func TestMaterializeStampsScopeAndGeneration(t *testing.T) {
	t.Parallel()
	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)
	if _, err := m.Materialize(context.Background(), sameNameProjection()); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	for _, want := range []string{
		"w.scope_id = row.scope_id",
		"w.generation_id = row.generation_id",
		"i.scope_id = row.scope_id",
		"endpoint.scope_id = row.scope_id",
	} {
		if !containsCypher(executor.calls, want) {
			t.Fatalf("missing stamp cypher %q", want)
		}
	}
	rows := executor.calls[0].Parameters["rows"].([]map[string]any)
	if rows[0]["scope_id"] != "scope-alpha" || rows[0]["generation_id"] != "gen-1" {
		t.Fatalf("workload row stamps = %v/%v", rows[0]["scope_id"], rows[0]["generation_id"])
	}
}

func TestMaterializeWritesSameNameEdgeForCrossRepoSibling(t *testing.T) {
	t.Parallel()
	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)
	lookup := &fakeSiblingLookup{siblings: []WorkloadSibling{
		{ID: "workload:repository:r_bbbbbbbb:checkout", Name: "checkout", RepoID: "repository:r_bbbbbbbb"},
		// Self echo from the graph read must not self-pair.
		{ID: "workload:repository:r_aaaaaaaa:checkout", Name: "checkout", RepoID: "repository:r_aaaaaaaa"},
		// Same-repo duplicate id under another name must not pair.
		{ID: "workload:repository:r_aaaaaaaa:other", Name: "other", RepoID: "repository:r_aaaaaaaa"},
	}}
	m.SiblingLookup = lookup
	result, err := m.Materialize(context.Background(), sameNameProjection())
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if result.SameNameEdgesWritten != 1 {
		t.Fatalf("SameNameEdgesWritten = %d, want 1", result.SameNameEdgesWritten)
	}
	var pairRows []map[string]any
	for _, call := range executor.calls {
		if strings.Contains(call.Cypher, "MERGE (a)-[rel:SAME_NAME]->(b)") {
			pairRows = call.Parameters["rows"].([]map[string]any)
		}
	}
	if len(pairRows) != 1 {
		t.Fatalf("SAME_NAME pair rows = %d, want 1", len(pairRows))
	}
	row := pairRows[0]
	if row["a"] != "workload:repository:r_aaaaaaaa:checkout" ||
		row["b"] != "workload:repository:r_bbbbbbbb:checkout" {
		t.Fatalf("pair order = %v -> %v, want lexically smaller first", row["a"], row["b"])
	}
	if row["name"] != "checkout" || row["scope_id"] != "scope-alpha" {
		t.Fatalf("pair row = %v", row)
	}
	if len(lookup.names) != 1 || lookup.names[0] != "checkout" {
		t.Fatalf("lookup names = %v", lookup.names)
	}
}

func TestMaterializeSkipsSameNameWithoutLookup(t *testing.T) {
	t.Parallel()
	executor := &fakeNeo4jExecutor{}
	m := NewWorkloadMaterializer(executor)
	result, err := m.Materialize(context.Background(), sameNameProjection())
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if result.SameNameEdgesWritten != 0 {
		t.Fatalf("SameNameEdgesWritten = %d, want 0", result.SameNameEdgesWritten)
	}
	if containsCypher(executor.calls, "SAME_NAME") {
		t.Fatal("SAME_NAME cypher executed without lookup")
	}
}
