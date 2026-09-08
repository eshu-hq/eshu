// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestBuildServiceStoryResponseReturnsCompleteDossier(t *testing.T) {
	t.Parallel()

	workloadContext := querytestutil.SampleServiceDossierContext()

	got := BuildServiceStoryResponse("workload:sample-service-api", workloadContext)

	identity := querycontract.MapValue(got, "service_identity")
	if got, want := querycontract.StringVal(identity, "service_id"), "workload:sample-service-api"; got != want {
		t.Fatalf("service_identity.service_id = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(identity, "repo_id"), "repo-sample-service-api"; got != want {
		t.Fatalf("service_identity.repo_id = %q, want %q", got, want)
	}

	apiSurface := querycontract.MapValue(got, "api_surface")
	if got, want := querycontract.IntVal(apiSurface, "endpoint_count"), 2; got != want {
		t.Fatalf("api_surface.endpoint_count = %d, want %d", got, want)
	}
	endpoints := querycontract.MapSliceValue(apiSurface, "endpoints")
	if len(endpoints) != 2 {
		t.Fatalf("len(api_surface.endpoints) = %d, want 2", len(endpoints))
	}

	lanes := querycontract.MapSliceValue(got, "deployment_lanes")
	if len(lanes) != 2 {
		t.Fatalf("len(deployment_lanes) = %d, want dual deployment lanes: %#v", len(lanes), lanes)
	}
	gotLaneTypes := []string{querycontract.StringVal(lanes[0], "lane_type"), querycontract.StringVal(lanes[1], "lane_type")}
	wantLaneTypes := []string{"ecs_terraform", "k8s_gitops"}
	if !reflect.DeepEqual(gotLaneTypes, wantLaneTypes) {
		t.Fatalf("deployment lane types = %#v, want %#v", gotLaneTypes, wantLaneTypes)
	}

	upstream := querycontract.MapSliceValue(got, "upstream_dependencies")
	if len(upstream) != 4 {
		t.Fatalf("len(upstream_dependencies) = %d, want 4", len(upstream))
	}
	if got, want := querycontract.StringVal(upstream[0], "resolved_id"), "resolved-gitops"; got != want {
		t.Fatalf("upstream_dependencies[0].resolved_id = %q, want %q", got, want)
	}

	downstream := querycontract.MapValue(got, "downstream_consumers")
	if got, want := querycontract.IntVal(downstream, "graph_dependent_count"), 1; got != want {
		t.Fatalf("downstream_consumers.graph_dependent_count = %d, want %d", got, want)
	}
	if got, want := querycontract.IntVal(downstream, "content_consumer_count"), 1; got != want {
		t.Fatalf("downstream_consumers.content_consumer_count = %d, want %d", got, want)
	}

	graph := querycontract.MapValue(got, "evidence_graph")
	edges := querycontract.MapSliceValue(graph, "edges")
	if len(edges) != 4 {
		t.Fatalf("len(evidence_graph.edges) = %d, want 2 deployment and 2 runtime edges", len(edges))
	}
	deploymentEdges := 0
	runtimeEdges := 0
	for _, edge := range edges {
		if querycontract.StringVal(edge, "relationship_type") == "RUNS_AS" {
			runtimeEdges++
			continue
		}
		deploymentEdges++
		if querycontract.StringVal(edge, "resolved_id") == "" {
			t.Fatalf("evidence_graph edge missing resolved_id: %#v", edge)
		}
	}
	if deploymentEdges != 2 || runtimeEdges != 2 {
		t.Fatalf("evidence graph edge roles = deployment:%d runtime:%d, want 2/2", deploymentEdges, runtimeEdges)
	}

	limits := querycontract.MapValue(got, "result_limits")
	if got, want := limits["truncated"], false; got != want {
		t.Fatalf("result_limits.truncated = %#v, want false", got)
	}
}

func TestBuildServiceStoryResponseKeepsEmptyDossierSections(t *testing.T) {
	t.Parallel()

	got := BuildServiceStoryResponse("empty-service", map[string]any{
		"id":        "workload:empty-service",
		"name":      "empty-service",
		"kind":      "service",
		"repo_id":   "repo-empty",
		"repo_name": "empty-service",
		"instances": []map[string]any{},
	})

	for _, key := range []string{
		"service_identity",
		"api_surface",
		"deployment_lanes",
		"upstream_dependencies",
		"downstream_consumers",
		"evidence_graph",
		"result_limits",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("response missing empty dossier key %q: %#v", key, got)
		}
	}
	if got, want := querycontract.IntVal(querycontract.MapValue(got, "api_surface"), "endpoint_count"), 0; got != want {
		t.Fatalf("empty api_surface.endpoint_count = %d, want %d", got, want)
	}
	if lanes := querycontract.MapSliceValue(got, "deployment_lanes"); len(lanes) != 0 {
		t.Fatalf("empty deployment_lanes = %#v, want none", lanes)
	}
}

func TestBuildServiceStoryResponseHandlesSingleDeploymentLane(t *testing.T) {
	t.Parallel()

	got := BuildServiceStoryResponse("payments-api", map[string]any{
		"id":        "workload:payments-api",
		"name":      "payments-api",
		"kind":      "service",
		"repo_id":   "repo-payments-api",
		"repo_name": "payments-api",
		"instances": []map[string]any{
			{
				"instance_id":   "workload-instance:payments-api:prod",
				"platform_name": "payments-prod",
				"platform_kind": "argocd_application",
				"environment":   "production",
			},
		},
	})

	lanes := querycontract.MapSliceValue(got, "deployment_lanes")
	if len(lanes) != 1 {
		t.Fatalf("len(deployment_lanes) = %d, want 1: %#v", len(lanes), lanes)
	}
	if got, want := querycontract.StringVal(lanes[0], "lane_type"), "k8s_gitops"; got != want {
		t.Fatalf("deployment_lanes[0].lane_type = %q, want %q", got, want)
	}
}
