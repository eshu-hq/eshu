// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

// fetchWorkloadContextResultKeys enumerates every key
// fetchWorkloadContextForOperation (entity_workload_context.go) can set on the
// result map it returns. Kept as a literal list, not derived from source, so a
// future key added to that function without a matching schema property fails
// this test loudly instead of silently widening the wire contract (P2-1
// follow-up to #5764): "limitations" was emitted on the primary
// graph-materialized path -- not only the repository read-model fallback --
// but was undeclared in the WorkloadContext OpenAPI schema. getWorkloadContext
// (entity_workload_handlers.go) and getServiceContext (entity.go) both write
// this map verbatim via WriteSuccess for GET
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

// fetchServiceReadModelWorkloadContextResultKeys enumerates every key
// fetchServiceReadModelWorkloadContext (entity_workload_context.go) can set
// on the result map it returns. Kept as a literal list, not derived from
// source, for the same reason as fetchWorkloadContextResultKeys above: a
// future key added to that function without a matching schema property fails
// this test loudly (#5764 P3 review follow-up). This is the OTHER path that
// serves the WorkloadContext schema -- the repository-read-model fallback
// reached when no graph Workload node has materialized yet -- and it emits
// two keys the primary graph-materialized path never does:
// "materialization_status" and "query_basis". Both were undeclared in the
// OpenAPI schema because the original TestOpenAPIWorkloadContextDeclaresEveryFetchedKey
// only ever covered fetchWorkloadContextForOperation's key set.
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

func workloadContextSchemaProperties(t *testing.T) map[string]any {
	t.Helper()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	workloadContext := mustMapField(t, schemas, "WorkloadContext")
	return mustMapField(t, workloadContext, "properties")
}
