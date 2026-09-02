// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// fetchWorkloadContextResultKeys is the reviewed key list for
// fetchWorkloadContextForOperation (entity_workload_context.go). It is pinned
// from BOTH sides, because either direction alone is a false green (#5764
// round-7 P2-1 -- the earlier comment here claimed a guarantee this list did
// not carry):
//
//   - TestOpenAPIWorkloadContextDeclaresEveryFetchedKey proves list ⊆ schema,
//     so a reviewed key cannot be missing an OpenAPI property. That is what
//     caught "limitations" being emitted on the primary graph-materialized
//     path -- not only the repository read-model fallback -- while undeclared
//     in the WorkloadContext schema.
//   - TestFetchWorkloadContextEmitsOnlyDeclaredKeys drives the production
//     function and proves emitted == list, so a key added to that function
//     cannot reach the wire without being reviewed here first. Without it, an
//     added key silently widened the contract: this list is hand-maintained
//     and nothing read the function.
//
// getWorkloadContext (entity_workload_handlers.go) and getServiceContext
// (entity.go) both write this map verbatim via WriteSuccess for GET
// /api/v0/workloads/{workload_id}/context and GET
// /api/v0/services/{service_name}/context, which share the WorkloadContext
// schema.
var fetchWorkloadContextResultKeys = []string{
	"id",
	"name",
	"kind",
	"repo_id",
	"repo_name",
	"instances",
	"topology_edges",
	"provisioned_platforms",
	"runtime_topology_limits",
	"deployment_evidence",
	"dependencies",
	"infrastructure",
	"limitations",
}

// TestOpenAPIWorkloadContextDeclaresEveryFetchedKey keeps the WorkloadContext
// wire contract aligned with every key fetchWorkloadContextForOperation can
// set.
func TestOpenAPIWorkloadContextDeclaresEveryFetchedKey(t *testing.T) {
	t.Parallel()

	properties := workloadContextSchemaProperties(t)

	for _, key := range fetchWorkloadContextResultKeys {
		if _, ok := properties[key]; !ok {
			t.Errorf("WorkloadContext schema properties missing %q, set by fetchWorkloadContextForOperation", key)
		}
	}
}

// fetchServiceReadModelWorkloadContextResultKeys is the reviewed key list for
// fetchServiceReadModelWorkloadContext (entity_workload_context.go), pinned
// from both sides exactly like fetchWorkloadContextResultKeys above:
// TestOpenAPIWorkloadContextDeclaresEveryReadModelKey proves list ⊆ schema and
// TestFetchServiceReadModelWorkloadContextEmitsOnlyDeclaredKeys proves
// emitted == list. This is the OTHER path that serves the WorkloadContext
// schema -- the repository-read-model fallback reached when no graph Workload
// node has materialized yet -- and it emits two keys the primary
// graph-materialized path never does: "materialization_status" and
// "query_basis". Both were undeclared in the OpenAPI schema because the
// original TestOpenAPIWorkloadContextDeclaresEveryFetchedKey only ever covered
// fetchWorkloadContextForOperation's key set. This function is not
// queryplan-digest-tracked (it issues no direct graph Run), so no other gate
// notices a new key here (#5764 round-7 P2-1).
var fetchServiceReadModelWorkloadContextResultKeys = []string{
	"id",
	"name",
	"kind",
	"repo_id",
	"repo_name",
	"instances",
	"dependencies",
	"infrastructure",
	"materialization_status",
	"query_basis",
	"limitations",
}

// TestOpenAPIWorkloadContextDeclaresEveryReadModelKey is the read-model-path
// companion to TestOpenAPIWorkloadContextDeclaresEveryFetchedKey: it proves
// the WorkloadContext schema also covers every key
// fetchServiceReadModelWorkloadContext can set, since both functions feed the
// same schema via getWorkloadContext/getServiceContext (#5764 P3 review
// follow-up).
func TestOpenAPIWorkloadContextDeclaresEveryReadModelKey(t *testing.T) {
	t.Parallel()

	properties := workloadContextSchemaProperties(t)

	for _, key := range fetchServiceReadModelWorkloadContextResultKeys {
		if _, ok := properties[key]; !ok {
			t.Errorf("WorkloadContext schema properties missing %q, set by fetchServiceReadModelWorkloadContext", key)
		}
	}
}

// TestFetchWorkloadContextEmitsOnlyDeclaredKeys closes the emitted-side of the
// WorkloadContext key contract (#5764 round-7 P2-1). The fixture is built to
// reach every conditional emit in fetchWorkloadContextForOperation --
// "deployment_evidence" needs the workload row to carry one, "limitations"
// needs the infrastructure read to degrade -- so the assertion is exact set
// equality, not containment: adding result["undeclared_new_key"] to the
// production function fails here.
func TestFetchWorkloadContextEmitsOnlyDeclaredKeys(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (w:Workload) WHERE") {
					return nil, nil
				}
				return map[string]any{
					"id":      "workload:api",
					"name":    "api",
					"kind":    "service",
					"repo_id": "repo-1",
					"deployment_evidence": map[string]any{
						"delivery_family_paths": []map[string]any{},
					},
				}, nil
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, infrastructureGraphReadCypherFragment):
					return nil, fmt.Errorf("private graph detail: %w", ErrGraphUnavailable)
				case strings.Contains(cypher, "<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-1", "repo_name": "api"}}, nil
				}
				return nil, nil
			},
		},
	}

	result, err := handler.fetchWorkloadContextForOperation(
		t.Context(),
		"w.id = $workload_id",
		map[string]any{"workload_id": "workload:api"},
		"workload_context",
	)
	if err != nil {
		t.Fatalf("fetchWorkloadContextForOperation() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("fetchWorkloadContextForOperation() = nil, want a result map")
	}
	requireSameKeySet(t, "fetchWorkloadContextForOperation", result, fetchWorkloadContextResultKeys)
}

// TestFetchServiceReadModelWorkloadContextEmitsOnlyDeclaredKeys is the
// read-model companion: every key in that function's returned map literal is
// unconditional, so this fixture only has to reach the return (#5764 round-7
// P2-1). Adding result["readmodel_undeclared"] there fails here.
func TestFetchServiceReadModelWorkloadContextEmitsOnlyDeclaredKeys(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo-readmodel",
				Name: "readmodel-job",
				Path: "/repos/readmodel-job",
			}},
			summary: RepositoryReadModelSummary{
				Available:     true,
				WorkloadNames: []string{"readmodel-job"},
			},
		},
	}

	result, err := handler.fetchServiceReadModelWorkloadContext(t.Context(), "readmodel-job")
	if err != nil {
		t.Fatalf("fetchServiceReadModelWorkloadContext() error = %v, want nil", err)
	}
	if result == nil {
		t.Fatal("fetchServiceReadModelWorkloadContext() = nil, want a result map")
	}
	requireSameKeySet(t, "fetchServiceReadModelWorkloadContext", result, fetchServiceReadModelWorkloadContextResultKeys)
}

// requireSameKeySet asserts the keys a production context function emitted are
// exactly the reviewed list, naming both directions of the difference so a
// failure says whether a key was added to production or dropped from the list.
func requireSameKeySet(t *testing.T, function string, emitted map[string]any, declared []string) {
	t.Helper()

	declaredSet := make(map[string]struct{}, len(declared))
	for _, key := range declared {
		declaredSet[key] = struct{}{}
	}
	emittedKeys := make([]string, 0, len(emitted))
	for key := range emitted {
		emittedKeys = append(emittedKeys, key)
		if _, ok := declaredSet[key]; !ok {
			t.Errorf("%s emitted key %q that is not in its reviewed key list; add it there and to the WorkloadContext OpenAPI schema", function, key)
		}
	}
	sort.Strings(emittedKeys)
	for _, key := range declared {
		if _, ok := emitted[key]; !ok {
			t.Errorf("%s did not emit reviewed key %q; emitted keys were %v", function, key, emittedKeys)
		}
	}
}

func workloadContextSchemaProperties(t *testing.T) map[string]any {
	t.Helper()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	components := querytestutil.MustMapField(t, spec, "components")
	schemas := querytestutil.MustMapField(t, components, "schemas")
	workloadContext := querytestutil.MustMapField(t, schemas, "WorkloadContext")
	return querytestutil.MustMapField(t, workloadContext, "properties")
}
