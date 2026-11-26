// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneC13 pins the lane-C13 shape-completeness fixes:
// the WorkloadContext list rows, topology objects, and browser-session
// schemas must name their properties in the served spec instead of bare
// {"type": "object"} shapes or description-only objects, and every
// 200-response property the owner emits must be present and described.
// Each entry names the Go owner that emits the keys the contract asserts.
func TestContractShapesLaneC13(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("spec components has no schemas")
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing or non-object property %q", where, key)
			return nil
		}
		return next
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Errorf("%s: object schema has no properties", where)
			return nil
		}
		return props
	}

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		if node == nil {
			t.Errorf("%s: nil node has no items", where)
			return nil
		}
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Errorf("%s: array schema has no items", where)
			return nil
		}
		return items
	}

	// nodeOf returns the named property or nil after recording the miss,
	// so one RED run reports every gap instead of panicking on the first.
	nodeOf := func(t *testing.T, props map[string]any, where, key string) map[string]any {
		t.Helper()
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing property %q", where, key)
			return nil
		}
		return node
	}

	// requireShaped fails when a property is a bare object: neither
	// named properties, additionalProperties, nor a $ref/allOf/oneOf
	// composition completes it beyond a naive text window.
	requireShaped := func(t *testing.T, props map[string]any, where, key string) {
		t.Helper()
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing property %q", where, key)
			return
		}
		if node["type"] == "object" {
			_, hasProps := node["properties"]
			_, hasAdditional := node["additionalProperties"]
			_, hasRef := node["$ref"]
			_, hasAllOf := node["allOf"]
			_, hasOneOf := node["oneOf"]
			if !hasProps && !hasAdditional && !hasRef && !hasAllOf && !hasOneOf {
				t.Errorf("%s: property %q is a bare object with no properties", where, key)
			}
		}
		if node["type"] == "array" {
			items, ok := node["items"].(map[string]any)
			if !ok {
				t.Errorf("%s: property %q is an array with no items", where, key)
				return
			}
			if items["type"] == "object" {
				_, hasProps := items["properties"]
				_, hasAdditional := items["additionalProperties"]
				_, hasRef := items["$ref"]
				if !hasProps && !hasAdditional && !hasRef {
					t.Errorf("%s: property %q has bare object items", where, key)
				}
			}
		}
	}

	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
			}
		}
	}

	requireScalarTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
		t.Helper()
		for key, typ := range want {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := node["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}

	// requireExactKeys pins the closed property set of an object whose
	// owner emits a bounded key list: missing keys and phantom additions
	// both fail. Conditional keys are pinned too: the spec names every
	// key the owner can emit, and each description says when it is absent.
	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		if len(props) != len(keys) {
			extra := []string{}
			want := map[string]bool{}
			for _, key := range keys {
				want[key] = true
			}
			for key := range props {
				if !want[key] {
					extra = append(extra, key)
				}
			}
			t.Errorf("%s: has %d properties, want %d (extra: %v)", where, len(props), len(keys), extra)
		}
		for _, key := range keys {
			if _, ok := props[key]; !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
	}

	workload := func(t *testing.T) map[string]any {
		t.Helper()
		node, ok := schemas["WorkloadContext"]
		if !ok {
			t.Fatal("spec components.schemas has no WorkloadContext")
		}
		return propsOf(t, node.(map[string]any), "WorkloadContext")
	}

	t.Run("context identity described", func(t *testing.T) {
		t.Parallel()
		// Owner entity/workload_context.go fetchWorkloadContextDecision
		// and FetchServiceReadModelWorkloadContext: graph row id/name/kind
		// plus the resolved repo_id/repo_name, and the merged instance and
		// content environment names.
		props := workload(t)
		requireDescribed(t, props, "WorkloadContext",
			"id", "name", "kind", "repo_id", "repo_name", "observed_config_environments")
		requireScalarTypes(t, props, "WorkloadContext", map[string]string{
			"id": "string", "name": "string", "kind": "string",
			"repo_id": "string", "repo_name": "string",
			"observed_config_environments": "array",
		})
	})

	t.Run("service evidence rows", func(t *testing.T) {
		t.Parallel()
		// Owners service/query_enrichment_rows.go buildServiceHostnameRows
		// and buildServiceEntrypointCandidateRows, and
		// service/contract_helpers.go buildServiceEntrypoints.
		props := workload(t)
		for _, key := range []string{"hostnames", "entrypoint_candidates", "entrypoints", "network_paths"} {
			requireShaped(t, props, "WorkloadContext", key)
		}
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "hostnames"), "WorkloadContext.hostnames"), "WorkloadContext.hostnames[]"),
			"WorkloadContext.hostnames[]", "hostname", "environment", "relative_path", "reason")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "entrypoint_candidates"), "WorkloadContext.entrypoint_candidates"), "WorkloadContext.entrypoint_candidates[]"),
			"WorkloadContext.entrypoint_candidates[]", "candidate", "classification", "relative_path", "reason")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "entrypoints"), "WorkloadContext.entrypoints"), "WorkloadContext.entrypoints[]"),
			"WorkloadContext.entrypoints[]", "type", "target", "environment", "visibility", "relative_path", "reason")
		// Owner service/contract_helpers.go buildServiceNetworkPaths:
		// one row per entrypoint that matches a runtime instance.
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "network_paths"), "WorkloadContext.network_paths"), "WorkloadContext.network_paths[]"),
			"WorkloadContext.network_paths[]", "path_type", "from_type", "from", "to_type", "to",
			"platform_kind", "environment", "reason", "visibility")
	})

	t.Run("downstream consumer rows", func(t *testing.T) {
		t.Parallel()
		// Owners service/contract_helpers.go BuildGraphDependents,
		// deployment/consumer_enrichment_sources.go
		// LoadConsumerRepositoryEnrichmentWithSources, and
		// deployment/enrichment.go LoadProvisioningSourceChainsFromCandidates.
		// Owner service/story_overview.go
		// buildServiceStorySectionsWithContext emits only title/summary.
		props := workload(t)
		for _, key := range []string{"dependents", "consumer_repositories", "provisioning_source_chains", "story_sections"} {
			requireShaped(t, props, "WorkloadContext", key)
		}
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "story_sections"), "WorkloadContext.story_sections"), "WorkloadContext.story_sections[]"),
			"WorkloadContext.story_sections[]", "title", "summary")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "dependents"), "WorkloadContext.dependents"), "WorkloadContext.dependents[]"),
			"WorkloadContext.dependents[]", "repository", "repo_id", "relationship_types", "relationship_reasons")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "consumer_repositories"), "WorkloadContext.consumer_repositories"), "WorkloadContext.consumer_repositories[]"),
			"WorkloadContext.consumer_repositories[]", "repository", "repo_id", "consumer_kinds",
			"graph_relationship_types", "graph_relationship_reasons",
			"evidence_kinds", "matched_values", "sample_paths")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "provisioning_source_chains"), "WorkloadContext.provisioning_source_chains"), "WorkloadContext.provisioning_source_chains[]"),
			"WorkloadContext.provisioning_source_chains[]", "repository", "repo_id", "relationship_types",
			"relationship_reasons", "evidence_kinds", "sample_paths", "modules", "config_paths")
	})

	t.Run("repository panel rows", func(t *testing.T) {
		t.Parallel()
		// Owners repository/context_helpers.go QueryRepoDependencies
		// (plus relationship_overview.go copyRelationshipEvidenceMetadata
		// and querycontract AddRelationshipConfidenceBasis) and
		// repository/infrastructure.go queryRepoInfrastructureRows.
		props := workload(t)
		for _, key := range []string{"dependencies", "infrastructure"} {
			requireShaped(t, props, "WorkloadContext", key)
		}
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "dependencies"), "WorkloadContext.dependencies"), "WorkloadContext.dependencies[]"),
			"WorkloadContext.dependencies[]", "type", "target_name", "target_id", "evidence_type",
			"resolved_id", "generation_id", "confidence", "evidence_count", "evidence_kinds",
			"resolution_source", "confidence_basis", "rationale")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "infrastructure"), "WorkloadContext.infrastructure"), "WorkloadContext.infrastructure[]"),
			"WorkloadContext.infrastructure[]", "type", "name", "file_path", "kind", "source",
			"terraform_source", "config_path", "provider", "resource_type", "resource_service",
			"resource_category", "deployment_name", "includes", "inputs", "locals")
	})

	t.Run("cloud resource rows", func(t *testing.T) {
		t.Parallel()
		// Owners deployment/cloud_resource_dependencies.go
		// LoadMaterializedServiceCloudResourceDependencies (CompactStringMap:
		// every key sparse) and deployment/cloud_resource_candidates.go
		// LoadUncorrelatedCloudResourceCandidatesBounded. The uncorrelated
		// set ships under its own key with its own truncation flag.
		props := workload(t)
		for _, key := range []string{"cloud_resources", "uncorrelated_cloud_resources"} {
			requireShaped(t, props, "WorkloadContext", key)
		}
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "cloud_resources"), "WorkloadContext.cloud_resources"), "WorkloadContext.cloud_resources[]"),
			"WorkloadContext.cloud_resources[]", "id", "name", "kind", "resource_type", "provider",
			"environment", "resource_id", "arn", "account_id", "region", "relationship_basis",
			"resolution_mode", "evidence_source", "service_anchor_source", "service_anchor_reason",
			"source_fact_id", "stable_fact_key", "source_system", "source_record_id", "collector_kind")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "uncorrelated_cloud_resources"), "WorkloadContext.uncorrelated_cloud_resources"), "WorkloadContext.uncorrelated_cloud_resources[]"),
			"WorkloadContext.uncorrelated_cloud_resources[]", "id", "name", "kind", "resource_type",
			"provider", "environment", "source", "config_path", "resource_service", "resource_category",
			"resource_id", "arn", "account_id", "region", "service_kind", "service_anchor_status",
			"candidate_status", "service_anchor_reason", "missing_relationship")
		requireDescribed(t, props, "WorkloadContext",
			"cloud_resources", "uncorrelated_cloud_resources", "uncorrelated_cloud_resources_truncated",
			"consumer_repositories_downstream_truncated", "consumer_repositories_evidence_file_truncated")
	})

	t.Run("posture surface and support overviews", func(t *testing.T) {
		t.Parallel()
		// Owners service/ingress_posture.go buildIngressPosture,
		// repository/api_surface.go buildGraphAPISurface plus
		// service/query_enrichment_rows.go buildServiceAPISurface (the
		// union ships under one key), and service/story_overview.go
		// buildServiceSupportOverviewWithContext.
		props := workload(t)
		for _, key := range []string{"ingress_posture", "api_surface", "support_overview"} {
			requireShaped(t, props, "WorkloadContext", key)
		}
		ingressProps := propsOf(t, child(t, props, "ingress_posture", "WorkloadContext"), "WorkloadContext.ingress_posture")
		requireExactKeys(t, ingressProps, "WorkloadContext.ingress_posture",
			"waf_coverage", "tls_termination", "edge_count", "waf_protected",
			"tls_terminated", "truth_basis", "reason", "edges")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, ingressProps, "WorkloadContext.ingress_posture", "edges"), "WorkloadContext.ingress_posture.edges"), "WorkloadContext.ingress_posture.edges[]"),
			"WorkloadContext.ingress_posture.edges[]", "id", "name", "resource_type", "waf_coverage", "tls_termination")
		surfaceProps := propsOf(t, child(t, props, "api_surface", "WorkloadContext"), "WorkloadContext.api_surface")
		requireExactKeys(t, surfaceProps, "WorkloadContext.api_surface",
			"truth_basis", "endpoint_count", "method_count", "operation_id_count", "endpoints",
			"source_paths", "spec_paths", "spec_versions", "api_versions", "detail_limit", "detail_truncated",
			"frameworks", "framework_route_count", "spec_count", "parsed_spec_count", "docs_routes", "hostnames")
		depOverview := child(t, props, "deployment_overview", "WorkloadContext")
		depOverviewProps := propsOf(t, depOverview, "WorkloadContext.deployment_overview")
		depSurfProps := propsOf(t, child(t, depOverviewProps, "api_surface", "WorkloadContext.deployment_overview"), "WorkloadContext.deployment_overview.api_surface")
		depEnd := nodeOf(t, depSurfProps, "WorkloadContext.deployment_overview.api_surface", "endpoints")
		if depEnd != nil {
			items := itemsOf(t, depEnd, "WorkloadContext.deployment_overview.api_surface.endpoints")
			if desc, _ := items["description"].(string); desc == "" {
				t.Errorf("WorkloadContext.deployment_overview.api_surface.endpoints items has no description")
			}
		}
		supportProps := propsOf(t, child(t, props, "support_overview", "WorkloadContext"), "WorkloadContext.support_overview")
		requireExactKeys(t, supportProps, "WorkloadContext.support_overview",
			"dependency_count", "dependent_count", "consumer_repository_count", "provisioning_source_count",
			"observed_environment_count", "entrypoint_host_count", "entrypoint_count", "network_path_count",
			"cloud_resource_count", "has_api_surface", "has_documentation_overview", "endpoint_count",
			"method_count", "spec_count", "deployment_tool_family_count", "delivery_path_count",
			"delivery_workflow_count", "target_support", "consumer_list_tool", "consumer_list_path")
	})

	t.Run("runtime topology rows", func(t *testing.T) {
		t.Parallel()
		// Owners entity/workload_runtime_topology.go newWorkloadInstance
		// and observedTopologyEdge (via workload_platform_topology.go
		// platformTopologyEdge), entity/workload_platform.go
		// attachDirectPlatforms, and entity/workload_provisioned_platforms.go
		// normalizeProvisionedPlatform.
		props := workload(t)
		instanceProps := propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "instances"), "WorkloadContext.instances"), "WorkloadContext.instances[]")
		requireExactKeys(t, instanceProps, "WorkloadContext.instances[]",
			"instance_id", "platform_name", "platform_kind", "platforms", "environment",
			"materialization_confidence", "materialization_provenance", "platform_confidence", "platform_reason")
		requireDescribed(t, instanceProps, "WorkloadContext.instances[]",
			"instance_id", "platform_name", "platform_kind", "environment",
			"materialization_confidence", "materialization_provenance", "platform_confidence", "platform_reason")
		edgeProps := propsOf(t, itemsOf(t, nodeOf(t, props, "WorkloadContext", "topology_edges"), "WorkloadContext.topology_edges"), "WorkloadContext.topology_edges[]")
		requireExactKeys(t, edgeProps, "WorkloadContext.topology_edges[]",
			"relationship_type", "source_id", "source_name", "target_id", "target_name",
			"confidence", "reason", "evidence_source", "source_tool", "properties")
		requireDescribed(t, edgeProps, "WorkloadContext.topology_edges[]",
			"relationship_type", "source_id", "source_name", "target_id", "target_name",
			"confidence", "reason", "evidence_source", "source_tool", "properties")
		platformProps := propsOf(t, itemsOf(t, child(t, props, "provisioned_platforms", "WorkloadContext"), "WorkloadContext.provisioned_platforms"), "WorkloadContext.provisioned_platforms[]")
		requireDescribed(t, platformProps, "WorkloadContext.provisioned_platforms[]",
			"platform_id", "platform_name", "platform_kind", "platform_provider", "platform_region")
	})

	t.Run("browser session schemas", func(t *testing.T) {
		t.Parallel()
		// Owner auth/session/browser_types.go BrowserSessionAuthResponse
		// (via BrowserSessionResponseFor) and BrowserSessionResponse.
		authNode, ok := schemas["BrowserSessionAuth"]
		if !ok {
			t.Fatal("spec components.schemas has no BrowserSessionAuth")
		}
		authProps := propsOf(t, authNode.(map[string]any), "BrowserSessionAuth")
		requireExactKeys(t, authProps, "BrowserSessionAuth",
			"mode", "tenant_id", "workspace_id", "subject_class", "subject_id_hash",
			"policy_revision_hash", "role_ids", "all_scopes", "allowed_scope_ids",
			"allowed_repository_ids", "permission_catalog_enforced", "allowed_permission_features",
			"external_provider_config_id")
		requireDescribed(t, authProps, "BrowserSessionAuth",
			"mode", "tenant_id", "workspace_id", "subject_class", "subject_id_hash",
			"policy_revision_hash", "role_ids", "all_scopes", "allowed_scope_ids",
			"allowed_repository_ids", "permission_catalog_enforced", "allowed_permission_features",
			"external_provider_config_id")
		respNode, ok := schemas["BrowserSessionResponse"]
		if !ok {
			t.Fatal("spec components.schemas has no BrowserSessionResponse")
		}
		respProps := propsOf(t, respNode.(map[string]any), "BrowserSessionResponse")
		requireDescribed(t, respProps, "BrowserSessionResponse", "idle_expires_at", "absolute_expires_at")
	})

	t.Run("evidence packet citation twin", func(t *testing.T) {
		t.Parallel()
		// Owner querycontract/evidence/citation_handle.go
		// EvidenceCitationHandle: the inline source_facts[].citation twin
		// must carry the same descriptions as the citations[] layer the
		// slices-2/3 lane already completed.
		packetNode, ok := schemas["InvestigationEvidencePacket"]
		if !ok {
			t.Fatal("spec components.schemas has no InvestigationEvidencePacket")
		}
		packetProps := propsOf(t, packetNode.(map[string]any), "InvestigationEvidencePacket")
		factProps := propsOf(t, itemsOf(t, nodeOf(t, packetProps, "InvestigationEvidencePacket", "source_facts"), "InvestigationEvidencePacket.source_facts"), "InvestigationEvidencePacket.source_facts[]")
		citationProps := propsOf(t, child(t, factProps, "citation", "InvestigationEvidencePacket.source_facts[]"), "InvestigationEvidencePacket.source_facts[].citation")
		requireDescribed(t, citationProps, "InvestigationEvidencePacket.source_facts[].citation",
			"kind", "repo_id", "relative_path", "entity_id", "evidence_family", "reason",
			"start_line", "end_line", "confidence")
	})
}
