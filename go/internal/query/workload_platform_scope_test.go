// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestFetchWorkloadPlatformRowsBatchesExactInstanceIDs(t *testing.T) {
	t.Parallel()

	runCalls := 0
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				runCalls++
				if !strings.Contains(cypher, "MATCH (repo:Repository)-[:DEFINES]->(w:Workload)<-[:INSTANCE_OF]-(i:WorkloadInstance)-[runsOn:RUNS_ON]->(p:Platform)") {
					t.Fatalf("cypher = %q, want repository/workload-anchored RUNS_ON lookup", cypher)
				}
				for _, predicate := range []string{
					"repo.id = $repo_id", "w.id = $workload_id", "i.id IN $instance_ids",
				} {
					if !strings.Contains(cypher, predicate) {
						t.Fatalf("cypher = %q, want exact predicate %q", cypher, predicate)
					}
				}
				if strings.Contains(cypher, "MATCH (i)-[runsOn:RUNS_ON]->") {
					t.Fatalf("cypher = %q, want WorkloadInstance label and RUNS_ON traversal in one MATCH", cypher)
				}
				if !strings.Contains(cypher, "p.id as platform_id") {
					t.Fatalf("cypher = %q, want canonical Platform id projection", cypher)
				}
				gotIDs := StringSliceVal(params, "instance_ids")
				wantIDs := []string{
					"workload-instance:sample-service:example-prod",
					"workload-instance:sample-service:platform-qa",
				}
				if !reflect.DeepEqual(gotIDs, wantIDs) {
					t.Fatalf("params[instance_ids] = %#v, want %#v", gotIDs, wantIDs)
				}
				if got, want := StringVal(params, "repo_id"), "repository:sample"; got != want {
					t.Fatalf("params[repo_id] = %q, want %q", got, want)
				}
				if got, want := StringVal(params, "workload_id"), "workload:sample-service"; got != want {
					t.Fatalf("params[workload_id] = %q, want %q", got, want)
				}
				return []map[string]any{
					{
						"instance_id":         "workload-instance:sample-service:example-prod",
						"platform_name":       "example-prod",
						"platform_kind":       "kubernetes",
						"platform_confidence": 0.99,
						"platform_reason":     "resolved_deployment_evidence",
					},
					{
						"instance_id":         "workload-instance:sample-service:platform-qa",
						"platform_name":       "platform-qa",
						"platform_kind":       "kubernetes",
						"platform_confidence": 0.98,
						"platform_reason":     "resolved_deployment_evidence",
					},
				}, nil
			},
		},
	}

	rows, err := handler.fetchWorkloadPlatformRows(
		context.Background(), "repository:sample", "workload:sample-service",
		[]map[string]any{
			{"instance_id": "workload-instance:sample-service:example-prod"},
			{"instance_id": "workload-instance:sample-service:platform-qa"},
		},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadPlatformRows() error = %v, want nil", err)
	}
	if runCalls != 1 {
		t.Fatalf("graph calls = %d, want 1", runCalls)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
}

func TestFetchWorkloadPlatformRowsOmitUnownedEvidenceForScopedTokens(t *testing.T) {
	t.Parallel()

	calls := 0
	handler := &EntityHandler{Neo4j: fakeWorkloadGraphReader{
		run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
			calls++
			return []map[string]any{{"platform_id": "platform:unowned"}}, nil
		},
	}}
	ctx := ContextWithAuthContext(t.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:allowed"},
	})

	rows, err := handler.fetchWorkloadPlatformRows(
		ctx,
		"repository:allowed",
		"workload:orders",
		[]map[string]any{{"instance_id": "workload-instance:orders:prod"}},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadPlatformRows() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want no repository-unowned platform evidence", rows)
	}
	if calls != 0 {
		t.Fatalf("graph calls = %d, want zero for scoped token", calls)
	}
}
