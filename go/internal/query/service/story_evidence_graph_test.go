// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestBuildServiceEvidenceGraphSeparatesWorkloadRepositoryConfigAndRuntimeRoles(t *testing.T) {
	context := map[string]any{
		"id":        "workload:api-node-boats",
		"name":      "api-node-boats",
		"repo_id":   "repository:r_service",
		"repo_name": "api-node-boats",
		"instances": []map[string]any{
			{
				"instance_id":   "runtime:api-node-boats:eks-prod",
				"platform_name": "eks-prod",
				"platform_kind": "kubernetes",
				"environment":   "prod",
			},
		},
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				{
					"resolved_id":              "resolved-argocd",
					"relationship_type":        "PROVISIONING_SOURCE_CHAIN",
					"artifact_family":          "argocd",
					"source_repo_id":           "repository:r_argocd_observation",
					"source_repo_name":         "iac-eks-argocd",
					"source_repo_canonical_id": "repository:r_argocd_canonical",
					"source_repo_scope_key":    "scope:s_argocd",
					"target_repo_id":           "repository:r_service",
					"target_repo_name":         "api-node-boats",
				},
				{
					"resolved_id":       "resolved-helm",
					"relationship_type": "READS_CONFIG_FROM",
					"artifact_family":   "helm",
					"source_repo_id":    "repository:r_service",
					"source_repo_name":  "api-node-boats",
					"target_repo_id":    "repository:r_helm",
					"target_repo_name":  "helm-charts",
				},
			},
		},
	}

	graph := buildServiceEvidenceGraph(context)
	nodes := querycontract.MapSliceValue(graph, "nodes")
	roles := map[string]string{}
	for _, node := range nodes {
		roles[querycontract.StringVal(node, "id")] = querycontract.StringVal(node, "role")
	}
	for id, want := range map[string]string{
		"workload:api-node-boats":         "workload",
		"repository:r_service":            "source_repository",
		"repository:r_argocd_observation": "deployment_configuration",
		"repository:r_helm":               "deployment_configuration",
		"runtime:api-node-boats:eks-prod": "runtime_instance",
	} {
		if got := roles[id]; got != want {
			t.Fatalf("node %q role = %q, want %q; nodes=%#v", id, got, want, nodes)
		}
	}

	var argocd map[string]any
	for _, node := range nodes {
		if querycontract.StringVal(node, "id") == "repository:r_argocd_observation" {
			argocd = node
			break
		}
	}
	if got, want := querycontract.StringVal(argocd, "canonical_key"), "repository:r_argocd_canonical"; got != want {
		t.Fatalf("Argo canonical_key = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(argocd, "scope_key"), "scope:s_argocd"; got != want {
		t.Fatalf("Argo scope_key = %q, want %q", got, want)
	}

	edges := querycontract.MapSliceValue(graph, "edges")
	foundRuntime := false
	for _, edge := range edges {
		if querycontract.StringVal(edge, "source") == "workload:api-node-boats" &&
			querycontract.StringVal(edge, "target") == "runtime:api-node-boats:eks-prod" &&
			querycontract.StringVal(edge, "relationship_type") == "RUNS_AS" {
			foundRuntime = true
		}
	}
	if !foundRuntime {
		t.Fatalf("evidence graph missing explicit workload-to-runtime edge: %#v", edges)
	}
}

func TestBuildServiceEvidenceGraphPreservesSourceRoleAgainstDownstreamDuplicates(t *testing.T) {
	context := map[string]any{
		"id":        "workload:api-node-boats",
		"name":      "api-node-boats",
		"repo_id":   "repository:r_service",
		"repo_name": "api-node-boats",
		"consumer_repositories": []map[string]any{
			{"repo_id": "repository:r_service", "repository": "consumer-alias"},
		},
	}

	graph := buildServiceEvidenceGraph(context)
	for _, node := range querycontract.MapSliceValue(graph, "nodes") {
		if querycontract.StringVal(node, "id") != "repository:r_service" {
			continue
		}
		if got, want := querycontract.StringVal(node, "role"), "source_repository"; got != want {
			t.Fatalf("source repository role = %q, want %q", got, want)
		}
		if got, want := querycontract.StringVal(node, "label"), "api-node-boats"; got != want {
			t.Fatalf("source repository label = %q, want %q", got, want)
		}
		return
	}
	t.Fatal("source repository node missing")
}

func TestBuildServiceEvidenceGraphOrdersRuntimeEdgesDeterministically(t *testing.T) {
	context := map[string]any{
		"id":   "workload:api-node-boats",
		"name": "api-node-boats",
		"instances": []map[string]any{
			{"instance_id": "runtime:z", "platform_name": "z"},
			{"instance_id": "runtime:a", "platform_name": "a"},
		},
	}

	edges := querycontract.MapSliceValue(buildServiceEvidenceGraph(context), "edges")
	if len(edges) != 2 {
		t.Fatalf("runtime edges = %d, want 2", len(edges))
	}
	if first, second := querycontract.StringVal(edges[0], "target"), querycontract.StringVal(edges[1], "target"); first != "runtime:a" || second != "runtime:z" {
		t.Fatalf("runtime edge order = [%q %q], want [runtime:a runtime:z]", first, second)
	}
}
