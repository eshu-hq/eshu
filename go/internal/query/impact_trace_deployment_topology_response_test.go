// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildDeploymentFactsPreservesExactTopologyRelationships(t *testing.T) {
	t.Parallel()

	facts := buildDeploymentFacts(
		[]map[string]any{{
			"instance_id": "instance:sample-service:production",
			"platforms": []map[string]any{{
				"platform_id": "platform:production-eks", "platform_name": "production-eks", "platform_kind": "kubernetes",
				"topology_edges": []map[string]any{{
					"relationship_type": "RUNS_ON", "source_id": "instance:sample-service:production",
					"target_id": "platform:production-eks", "target_name": "production-eks",
				}},
			}},
		}},
		[]map[string]any{
			{"relationship_type": "DEFINES", "source_id": "repository:sample-service", "target_id": "workload:sample-service"},
			{"relationship_type": "INSTANCE_OF", "source_id": "instance:sample-service:production", "target_id": "workload:sample-service"},
		},
		[]map[string]any{{
			"platform_id": "platform:production-ecs", "platform_name": "production-ecs", "platform_kind": "ecs",
			"topology_edges": []map[string]any{
				{"relationship_type": "PROVISIONS_DEPENDENCY_FOR", "source_id": "repository:infra", "target_id": "repository:sample-service"},
				{"relationship_type": "PROVISIONS_PLATFORM", "source_id": "repository:infra", "target_id": "platform:production-ecs"},
			},
		}},
		nil,
	)

	relationships := map[string]bool{}
	for _, fact := range facts {
		key := StringVal(fact, "type") + "\x00" + StringVal(fact, "source_id") + "\x00" + StringVal(fact, "target_id")
		relationships[key] = true
	}
	for _, want := range []string{
		"DEFINES\x00repository:sample-service\x00workload:sample-service",
		"INSTANCE_OF\x00instance:sample-service:production\x00workload:sample-service",
		"RUNS_ON\x00instance:sample-service:production\x00platform:production-eks",
		"PROVISIONS_DEPENDENCY_FOR\x00repository:infra\x00repository:sample-service",
		"PROVISIONS_PLATFORM\x00repository:infra\x00platform:production-ecs",
	} {
		if !relationships[want] {
			t.Fatalf("deployment topology facts = %#v, want exact relationship %q", relationships, want)
		}
	}
	for _, fact := range facts {
		if got := StringVal(fact, "type"); got == "RUNS_ON_PLATFORM" || got == "MATERIALIZED_IN_ENVIRONMENT" {
			t.Fatalf("deployment facts include invented relationship family %q: %#v", got, facts)
		}
	}
}

func TestBuildDeploymentTraceResponseReportsCollectionCoverage(t *testing.T) {
	t.Parallel()

	got := buildDeploymentTraceResponse("service-edge-api", map[string]any{
		"id": "workload:service-edge-api", "name": "service-edge-api",
		"deployment_sources": []map[string]any{},
		"deployment_source_limits": map[string]any{
			"limit": contextStoryItemLimit, "returned_count": contextStoryItemLimit,
			"observed_count": contextStoryItemLimit + 1, "observed_count_is_lower_bound": true, "truncated": true,
		},
		"cloud_resource_limits": map[string]any{"limit": serviceStoryItemLimit, "truncated": true},
		"runtime_topology_limits": map[string]any{
			"instances": map[string]any{"limit": contextStoryItemLimit, "truncated": false},
		},
	})

	deploymentLimits := mapValue(got, "deployment_source_limits")
	if !BoolVal(deploymentLimits, "truncated") || !BoolVal(deploymentLimits, "observed_count_is_lower_bound") {
		t.Fatalf("deployment_source_limits = %#v, want explicit lower-bound truncation", deploymentLimits)
	}
	if !BoolVal(mapValue(got, "cloud_resource_limits"), "truncated") {
		t.Fatalf("cloud_resource_limits = %#v, want truncation", got["cloud_resource_limits"])
	}
	if len(mapValue(got, "runtime_topology_limits")) == 0 {
		t.Fatal("runtime_topology_limits missing")
	}
}
