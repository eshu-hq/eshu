// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestOpenAPIImpactDeploymentTraceDocumentsCanonicalPlatformIdentity(t *testing.T) {
	recorder := httptest.NewRecorder()
	ServeOpenAPI(recorder, httptest.NewRequest("GET", "/api/v0/openapi.json", nil))

	var spec map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI document: %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	tracePath := mustMapField(t, paths, "/api/v0/impact/trace-deployment-chain")
	tracePost := mustMapField(t, tracePath, "post")
	responses := mustMapField(t, tracePost, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	instances := mustMapField(t, properties, "instances")
	instanceItems := mustMapField(t, instances, "items")
	instanceProperties := mustMapField(t, instanceItems, "properties")
	platforms := mustMapField(t, instanceProperties, "platforms")
	platformItems := mustMapField(t, platforms, "items")
	platformProperties := mustMapField(t, platformItems, "properties")
	if _, ok := platformProperties["platform_id"]; !ok {
		t.Fatal("impact trace instances[].platforms[] schema missing platform_id")
	}
	topologyBasis := mustMapField(t, platformProperties, "topology_basis")
	if got, want := topologyBasis["enum"], []any{"direct_runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("impact trace topology_basis enum = %#v, want %#v", got, want)
	}
	topologyEdges := mustMapField(t, platformProperties, "topology_edges")
	topologyEdgeItems := mustMapField(t, topologyEdges, "items")
	topologyEdgeProperties := mustMapField(t, topologyEdgeItems, "properties")
	relationshipType := mustMapField(t, topologyEdgeProperties, "relationship_type")
	if got, want := relationshipType["enum"], []any{"RUNS_ON"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("instance platform relationship enum = %#v, want %#v", got, want)
	}
	if _, ok := topologyEdgeProperties["properties"]; !ok {
		t.Fatal("impact trace instances[].platforms[].topology_edges[] schema missing properties")
	}
	topology := mustMapField(t, properties, "topology_edges")
	topologyItems := mustMapField(t, topology, "items")
	topologyProperties := mustMapField(t, topologyItems, "properties")
	topologyRelationshipType := mustMapField(t, topologyProperties, "relationship_type")
	if got, want := topologyRelationshipType["enum"], []any{"DEFINES", "INSTANCE_OF"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level topology relationship enum = %#v, want %#v", got, want)
	}
	provisioned := mustMapField(t, properties, "provisioned_platforms")
	provisionedItems := mustMapField(t, provisioned, "items")
	provisionedProperties := mustMapField(t, provisionedItems, "properties")
	if _, ok := provisionedProperties["topology_edges"]; !ok {
		t.Fatal("impact trace provisioned_platforms[] schema missing topology_edges")
	}
	for _, limitsField := range []string{"runtime_topology_limits", "cloud_resource_limits"} {
		if _, ok := properties[limitsField]; !ok {
			t.Fatalf("impact trace schema missing %s", limitsField)
		}
	}
	for _, field := range []string{
		"relationship_type",
		"source_id",
		"source_name",
		"target_id",
		"target_name",
		"confidence",
		"reason",
		"evidence_source",
		"source_tool",
	} {
		if _, ok := topologyEdgeProperties[field]; !ok {
			t.Fatalf("impact trace instances[].platforms[].topology_edges[] schema missing %s", field)
		}
	}
	deploymentSources := mustMapField(t, properties, "deployment_sources")
	deploymentSourceItems := mustMapField(t, deploymentSources, "items")
	deploymentSourceProperties := mustMapField(t, deploymentSourceItems, "properties")
	for _, field := range []string{"relationship_type", "source_id", "target_id"} {
		if _, ok := deploymentSourceProperties[field]; !ok {
			t.Fatalf("impact trace deployment_sources[] schema missing %s", field)
		}
	}
	deploymentSourceLimits := mustMapField(t, properties, "deployment_source_limits")
	deploymentSourceLimitProperties := mustMapField(t, deploymentSourceLimits, "properties")
	for _, field := range []string{
		"limit",
		"query_sentinel_limit",
		"returned_count",
		"observed_count",
		"observed_count_is_lower_bound",
		"canonical_observed_count",
		"repository_observed_count",
		"truncated",
		"ordering",
	} {
		if _, ok := deploymentSourceLimitProperties[field]; !ok {
			t.Fatalf("impact trace deployment_source_limits schema missing %s", field)
		}
	}
}
