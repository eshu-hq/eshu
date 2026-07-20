// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
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
	uncorrelatedCloudResources := mustMapField(t, properties, "uncorrelated_cloud_resources")
	uncorrelatedDescription, _ := uncorrelatedCloudResources["description"].(string)
	for _, required := range []string{
		"candidate_status",
		"missing_relationship",
		"Deployment-config candidates are globally ordered by name and canonical ID",
		"Deployment-config candidates expose match_basis",
		"uncorrelated",
		"ambiguous_anchor",
		"stale_anchor",
		"weak_anchor",
	} {
		if !strings.Contains(uncorrelatedDescription, required) {
			t.Fatalf("uncorrelated_cloud_resources description missing %q: %q", required, uncorrelatedDescription)
		}
	}
	uncorrelatedCloudResourcesTruncated := mustMapField(t, properties, "uncorrelated_cloud_resources_truncated")
	truncationDescription, _ := uncorrelatedCloudResourcesTruncated["description"].(string)
	for _, required := range []string{
		"candidate discovery was incomplete",
		"returned list was capped",
		"deployment-config evidence",
		"anchor input",
		"even when no rows were returned",
	} {
		if !strings.Contains(truncationDescription, required) {
			t.Fatalf("uncorrelated_cloud_resources_truncated description missing %q: %q", required, truncationDescription)
		}
	}
	instances := mustMapField(t, properties, "instances")
	instanceItems := mustMapField(t, instances, "items")
	instanceProperties := mustMapField(t, instanceItems, "properties")
	platforms := mustMapField(t, instanceProperties, "platforms")
	platformItems := mustMapField(t, platforms, "items")
	platformProperties := mustMapField(t, platformItems, "properties")
	assertRequiredProperty(t, platformItems, "topology_basis", "impact trace instances[].platforms[]")
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
	assertRequiredProperty(t, provisionedItems, "topology_basis", "impact trace provisioned_platforms[]")
	if _, ok := provisionedProperties["topology_edges"]; !ok {
		t.Fatal("impact trace provisioned_platforms[] schema missing topology_edges")
	}
	assertProvisioningFallbackTopologyBasis(t, provisionedProperties, "impact trace provisioned_platforms[]")
	assertProvisionedPlatformSchema(t, provisionedProperties, "impact trace provisioned_platforms[]")
	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	workloadSession := mustMapField(t, schemas, "WorkloadContext")
	workloadSessionProperties := mustMapField(t, workloadSession, "properties")
	workloadInstances := mustMapField(t, workloadSessionProperties, "instances")
	workloadInstanceItems := mustMapField(t, workloadInstances, "items")
	workloadInstanceProperties := mustMapField(t, workloadInstanceItems, "properties")
	workloadPlatforms := mustMapField(t, workloadInstanceProperties, "platforms")
	workloadPlatformItems := mustMapField(t, workloadPlatforms, "items")
	workloadPlatformProperties := mustMapField(t, workloadPlatformItems, "properties")
	assertRequiredProperty(t, workloadPlatformItems, "topology_basis", "WorkloadContext instances[].platforms[]")
	workloadTopologyBasis := mustMapField(t, workloadPlatformProperties, "topology_basis")
	if got, want := workloadTopologyBasis["enum"], []any{"direct_runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("WorkloadContext direct topology_basis enum = %#v, want %#v", got, want)
	}
	workloadProvisioned := mustMapField(t, workloadSessionProperties, "provisioned_platforms")
	workloadProvisionedItems := mustMapField(t, workloadProvisioned, "items")
	workloadProvisionedProperties := mustMapField(t, workloadProvisionedItems, "properties")
	assertRequiredProperty(t, workloadProvisionedItems, "topology_basis", "WorkloadContext provisioned_platforms[]")
	assertProvisioningFallbackTopologyBasis(
		t,
		workloadProvisionedProperties,
		"WorkloadContext provisioned_platforms[]",
	)
	assertProvisionedPlatformSchema(t, workloadProvisionedProperties, "WorkloadContext provisioned_platforms[]")
	for _, limitsField := range []string{"runtime_topology_limits", "cloud_resource_limits", "k8s_resource_limits"} {
		if _, ok := properties[limitsField]; !ok {
			t.Fatalf("impact trace schema missing %s", limitsField)
		}
	}
	runtimeTopologyLimits := mustMapField(t, properties, "runtime_topology_limits")
	runtimeTopologyProperties := mustMapField(t, runtimeTopologyLimits, "properties")
	for _, collection := range []string{"instances", "platform_edges", "provisioned_platforms"} {
		collectionLimits := mustMapField(t, runtimeTopologyProperties, collection)
		collectionLimitProperties := mustMapField(t, collectionLimits, "properties")
		for _, field := range []string{
			"limit",
			"query_sentinel_limit",
			"returned_count",
			"observed_count",
			"observed_count_is_lower_bound",
			"truncated",
			"ordering",
		} {
			if _, ok := collectionLimitProperties[field]; !ok {
				t.Fatalf("impact trace runtime_topology_limits.%s schema missing %s", collection, field)
			}
		}
	}
	k8sResourceLimits := mustMapField(t, properties, "k8s_resource_limits")
	k8sResourceLimitProperties := mustMapField(t, k8sResourceLimits, "properties")
	for _, field := range []string{
		"limit",
		"query_sentinel_limit",
		"deployment_source_query_sentinel_limit",
		"returned_count",
		"observed_count",
		"observed_count_is_lower_bound",
		"content_observed_count",
		"content_observed_count_is_lower_bound",
		"deployment_source_observed_count",
		"deployment_source_observed_count_is_lower_bound",
		"truncated",
		"ordering",
		"k8s_select_candidate_sentinel_limit",
		"k8s_relationships_complete",
		"k8s_relationships_incomplete_reason",
	} {
		if _, ok := k8sResourceLimitProperties[field]; !ok {
			t.Fatalf("impact trace k8s_resource_limits schema missing %s", field)
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

func assertRequiredProperty(t *testing.T, schema map[string]any, field, context string) {
	t.Helper()
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("%s required = %T, want array containing %q", context, schema["required"], field)
	}
	for _, candidate := range required {
		if candidate == field {
			return
		}
	}
	t.Fatalf("%s required = %#v, want %q", context, required, field)
}

func assertProvisionedPlatformSchema(t *testing.T, properties map[string]any, context string) {
	t.Helper()
	for _, field := range []string{
		"platform_id",
		"platform_name",
		"platform_kind",
		"platform_provider",
		"platform_region",
		"platform_locator",
		"platform_confidence",
		"platform_reason",
		"topology_edges",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("%s schema missing %s", context, field)
		}
	}
	edges := mustMapField(t, properties, "topology_edges")
	edgeItems := mustMapField(t, edges, "items")
	for _, field := range []string{"relationship_type", "source_id", "target_id", "properties"} {
		assertRequiredProperty(t, edgeItems, field, context+".topology_edges[]")
	}
	edgeProperties := mustMapField(t, edgeItems, "properties")
	relationshipType := mustMapField(t, edgeProperties, "relationship_type")
	if got, want := relationshipType["enum"], []any{"PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s topology relationship enum = %#v, want %#v", context, got, want)
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
		"properties",
	} {
		if _, ok := edgeProperties[field]; !ok {
			t.Fatalf("%s topology_edges[] schema missing %s", context, field)
		}
	}
}

func assertProvisioningFallbackTopologyBasis(
	t *testing.T,
	properties map[string]any,
	context string,
) {
	t.Helper()
	topologyBasis := mustMapField(t, properties, "topology_basis")
	if got, want := topologyBasis["enum"], []any{"provisioning_fallback"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s topology_basis enum = %#v, want %#v", context, got, want)
	}
}

func TestProvisionedPlatformResponseSatisfiesRequiredPropertiesSchema(t *testing.T) {
	t.Parallel()

	edges := provisionedPlatformTopologyEdges(map[string]any{
		"platform_source_id":            "repository:infra",
		"platform_dependency_target_id": "repository:service",
		"platform_id":                   "platform:prod",
	})
	for _, edge := range edges {
		properties, ok := edge["properties"]
		if !ok {
			t.Fatalf("propertyless %s response omitted OpenAPI-required properties", StringVal(edge, "relationship_type"))
		}
		if _, ok := properties.(map[string]any); !ok {
			t.Fatalf("propertyless %s response properties = %T, want object", StringVal(edge, "relationship_type"), properties)
		}
	}
}
