// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"
)

func TestBuildDeploymentTraceResponseDeduplicatesRepositoryDeliveryPaths(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload-1",
		"name":      "payments-api",
		"kind":      "service",
		"repo_id":   "repo-1",
		"repo_name": "payments",
		"instances": []map[string]any{
			{
				"instance_id":   "inst-1",
				"platform_name": "payments-argocd",
				"platform_kind": "argocd_application",
				"environment":   "prod",
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":    "repo-helm",
				"repo_name":  "deployment-helm",
				"confidence": 0.98,
			},
		},
		"deployment_evidence": map[string]any{
			"delivery_paths": []map[string]any{
				{
					"type":   "deployment_source",
					"target": "deployment-helm",
				},
				{
					"kind": "workflow_artifact",
					"path": ".github/workflows/deploy.yaml",
				},
				{
					"kind": "workflow_artifact",
					"path": ".github/workflows/deploy.yaml",
				},
			},
		},
	}

	got := buildDeploymentTraceResponse("payments-api", ctx)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if gotCount, want := len(deliveryPaths), 2; gotCount != want {
		t.Fatalf("len(delivery_paths) = %d, want %d", gotCount, want)
	}
	if got, want := StringVal(deliveryPaths[0], "target"), "deployment-helm"; got != want {
		t.Fatalf("delivery_paths[0].target = %q, want %q", got, want)
	}
	if got, want := StringVal(deliveryPaths[1], "type"), "repository_delivery_artifact"; got != want {
		t.Fatalf("delivery_paths[1].type = %q, want %q", got, want)
	}
}

func TestBuildDeploymentTraceResponseDoesNotEmitStructurallyEmptyDeliveryPaths(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload-1",
		"name":      "payments-api",
		"kind":      "service",
		"repo_id":   "repo-1",
		"repo_name": "payments",
		"instances": []map[string]any{
			{
				"instance_id":   "inst-1",
				"platform_name": "modern",
				"platform_kind": "kubernetes",
				"environment":   "prod",
			},
		},
		"deployment_sources": []map[string]any{
			{
				"repo_id":    "repo-kustomize",
				"repo_name":  "deployment-kustomize",
				"confidence": 0.98,
			},
		},
		"image_refs": []string{"ghcr.io/acme/payments-api:1.2.3"},
		"deployment_evidence": map[string]any{
			"delivery_paths": []map[string]any{
				{},
				{
					"kind": "workflow_artifact",
					"path": ".github/workflows/deploy.yaml",
				},
			},
		},
	}

	got := buildDeploymentTraceResponse("payments-api", ctx)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	for _, path := range deliveryPaths {
		if StringVal(path, "type") == "" {
			t.Fatalf("delivery path missing type: %#v", path)
		}
		if StringVal(path, "type") == "" &&
			StringVal(path, "target") == "" &&
			StringVal(path, "path") == "" &&
			StringVal(path, "kind") == "" &&
			StringVal(path, "artifact_type") == "" &&
			StringVal(path, "evidence_kind") == "" {
			t.Fatalf("structurally empty delivery path leaked: %#v", path)
		}
	}
}

func TestBuildDeploymentTraceResponseSeparatesControllerIdentityFromObservedTargets(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"id":        "workload:service-edge-api",
		"name":      "service-edge-api",
		"kind":      "service",
		"repo_id":   "repository:r_service_edge_api",
		"repo_name": "service-edge-api",
		"instances": []map[string]any{
			{
				"instance_id":   "workload-instance:service-edge-api:modern",
				"platform_name": "modern",
				"platform_kind": "kubernetes",
				"environment":   "modern",
			},
		},
	}

	got := buildDeploymentTraceResponse("service-edge-api", ctx)

	controllerOverview, ok := got["controller_overview"].(map[string]any)
	if !ok {
		t.Fatalf("controller_overview type = %T, want map[string]any", got["controller_overview"])
	}
	if gotControllers := StringSliceVal(controllerOverview, "controllers"); len(gotControllers) != 0 {
		t.Fatalf("controller_overview.controllers = %#v, want empty when no controller entities exist", gotControllers)
	}
	if gotTargets, wantTargets := StringSliceVal(controllerOverview, "observed_targets"), []string{"modern"}; !reflect.DeepEqual(gotTargets, wantTargets) {
		t.Fatalf("controller_overview.observed_targets = %#v, want %#v", gotTargets, wantTargets)
	}
	if gotKinds, wantKinds := StringSliceVal(controllerOverview, "controller_kinds"), []string{"kubernetes"}; !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("controller_overview.controller_kinds = %#v, want %#v", gotKinds, wantKinds)
	}

	controllerDrivenPaths, ok := got["controller_driven_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("controller_driven_paths type = %T, want []map[string]any", got["controller_driven_paths"])
	}
	if gotCount, wantCount := len(controllerDrivenPaths), 1; gotCount != wantCount {
		t.Fatalf("len(controller_driven_paths) = %d, want %d", gotCount, wantCount)
	}
	if gotController := StringVal(controllerDrivenPaths[0], "controller"); gotController != "" {
		t.Fatalf("controller_driven_paths[0].controller = %q, want empty when only observed target is known", gotController)
	}
	if gotTarget, wantTarget := StringVal(controllerDrivenPaths[0], "observed_target"), "modern"; gotTarget != wantTarget {
		t.Fatalf("controller_driven_paths[0].observed_target = %q, want %q", gotTarget, wantTarget)
	}
}
