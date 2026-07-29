// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecDocumentsSupplyChainRuntimeContextRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := mustMapField(t, path, "get")
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	findings := mustMapField(t, properties, "findings")
	items := mustMapField(t, findings, "items")
	itemProperties := mustMapField(t, items, "properties")
	runtimeContext := mustMapField(t, itemProperties, "runtime_context")

	const wantDescription = "Read-time-resolved runtime context joined from the finding's repository_id at query time (#5746): the workloads, services, deployments, environments, and catalog refs that repository currently maps to. Populated when this finding shape is returned by the findings list or impact explain route; the transformed investigation-packet shape omits it. truth_basis is always read_time_resolved so callers can distinguish these IDs from baked workload_ids/service_ids/environments. The workload_id/service_id/environment filters resolve only the same current active repository mappings (#5747); stale baked values cannot satisfy them. An empty runtime_context is an honest 'no runtime facts landed yet' (fresh ingest) that self-heals on the next read; it is not an error and not 'never scanned'."
	if got := runtimeContext["description"]; got != wantDescription {
		t.Fatalf("runtime_context.description = %#v, want %#v", got, wantDescription)
	}
}
