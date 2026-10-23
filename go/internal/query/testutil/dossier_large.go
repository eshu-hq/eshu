// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testutil

import "fmt"

// OversizedServiceDossierContext builds a workload context for a large
// service: every row family past the story's 50-row cap, with the row keys the
// production builders emit (BuildGraphDependents, the consumer and provisioning
// chain enrichment, the hostname and entrypoint builders, and the materialized
// cloud-resource reader) and row widths that match what the graph and content
// readers return for a real deployment. The story builder run over it
// serializes past the 256 KiB MCP response budget, which the byte-size tests for
// #8798 pin. Names are synthetic.
func OversizedServiceDossierContext() map[string]any {
	const rows = 60
	endpoints := make([]map[string]any, 0, rows)
	artifacts := make([]map[string]any, 0, rows)
	deliveryPaths := make([]map[string]any, 0, rows)
	sharedConfig := make([]map[string]any, 0, rows)
	hostnames := make([]map[string]any, 0, rows)
	entrypoints := make([]map[string]any, 0, rows)
	instances := make([]map[string]any, 0, rows)
	dependents := make([]map[string]any, 0, rows)
	consumers := make([]map[string]any, 0, rows)
	chains := make([]map[string]any, 0, rows)
	cloud := make([]map[string]any, 0, rows)
	for i := range rows {
		endpoints = append(endpoints, map[string]any{
			"id":            fmt.Sprintf("endpoint:checkout-service:%03d", i),
			"path":          fmt.Sprintf("/v3/orders/{order_id}/lines/%03d", i),
			"methods":       []string{"get", "put", "delete"},
			"operation_ids": []string{fmt.Sprintf("getOrderLine%03d", i), fmt.Sprintf("putOrderLine%03d", i), fmt.Sprintf("deleteOrderLine%03d", i)},
			"spec_path":     "specs/checkout-service/openapi/orders.yaml",
		})
		artifacts = append(artifacts, map[string]any{
			"id": fmt.Sprintf("artifact-%03d", i), "direction": "incoming", "relationship_type": "DEPLOYS_FROM",
			"resolved_id": fmt.Sprintf("resolved-%03d-0123456789abcdef", i), "confidence": 0.94, "artifact_family": "argocd",
			"runtime_platform_kind": "argocd_applicationset", "source_repo_id": fmt.Sprintf("repo-gitops-%03d", i),
			"source_repo_name": fmt.Sprintf("deployment-charts-%03d", i), "target_repo_id": "repo-checkout-service",
			"target_repo_name": "checkout-service", "path": fmt.Sprintf("argocd/staging/checkout-service/app-%03d.yaml", i),
			"evidence_kind": "ARGOCD_APPLICATIONSET_DEPLOYS_FROM", "rationale": "applicationset template references the service chart and image repository",
			"extractor": "argocd-appset", "first_seen": "2026-05-01T10:00:00Z", "last_seen": "2026-10-20T10:00:00Z",
		})
		deliveryPaths = append(deliveryPaths, map[string]any{
			"path": fmt.Sprintf("deploy/staging/checkout-service/overlay-%03d/kustomization.yaml", i), "tool_family": "kustomize",
			"kind": "delivery_path", "environment": "staging", "repo_id": "repo-gitops", "matched_value": "checkout-service",
		})
		sharedConfig = append(sharedConfig, map[string]any{
			"path": fmt.Sprintf("config/shared/checkout-service/values-%03d.yaml", i), "tool_family": "helm",
			"kind": "shared_config", "environment": "staging", "repo_id": "repo-gitops",
		})
		hostnames = append(hostnames, map[string]any{
			"hostname": fmt.Sprintf("checkout-%03d.staging.example.test", i), "environment": "staging",
			"relative_path": fmt.Sprintf("charts/checkout-service/templates/ingress-%03d.yaml", i), "reason": "exact_hostname_candidate_reference",
		})
		entrypoints = append(entrypoints, map[string]any{
			"type": "hostname", "target": fmt.Sprintf("checkout-%03d.staging.example.test", i), "environment": "staging", "visibility": "public",
			"relative_path": fmt.Sprintf("charts/checkout-service/templates/ingress-%03d.yaml", i), "reason": "exact_hostname_candidate_reference",
		})
		instances = append(instances, map[string]any{
			"instance_id": fmt.Sprintf("inst-%03d", i), "platform_name": fmt.Sprintf("eks-staging-%03d", i), "platform_kind": "argocd_applicationset",
			"environment": "staging", "platform_id": fmt.Sprintf("platform:eks-staging-%03d", i), "namespace": "checkout",
			"materialization_status": "materialized", "workload_id": "workload:checkout-service",
		})
		dependents = append(dependents, map[string]any{
			"repository": fmt.Sprintf("consumer-service-%03d", i), "repo_id": fmt.Sprintf("repo-consumer-%03d", i),
			"relationship_types":   []string{"DEPLOYS_FROM", "READS_CONFIG_FROM"},
			"relationship_reasons": []string{"helm_values_reference"},
		})
		consumers = append(consumers, map[string]any{
			"repository": fmt.Sprintf("search-service-%03d", i), "repo_id": fmt.Sprintf("repo-search-%03d", i),
			"consumer_kinds": []string{"hostname_reference_consumer"},
			"evidence_kinds": []string{"hostname_reference"},
			"matched_values": []string{"checkout-service.staging.example.test", "checkout-service"},
			"sample_paths":   []string{fmt.Sprintf("config/staging-%03d.json", i), fmt.Sprintf("src/clients/checkout-%03d.ts", i)},
		})
		chains = append(chains, map[string]any{
			"repository": fmt.Sprintf("terraform-runtime-%03d", i), "repo_id": fmt.Sprintf("repo-terraform-%03d", i),
			"relationship_types": []string{"PROVISIONS_DEPENDENCY_FOR"}, "relationship_reasons": []string{"terraform_module_source"},
			"evidence_kinds": []string{"terraform_module_source"}, "sample_paths": []string{fmt.Sprintf("env/staging/%03d/main.tf", i)},
			"modules": []string{"ecs_service", "alb_listener", "iam_role"},
		})
		cloud = append(cloud, map[string]any{
			"id": fmt.Sprintf("cloud:aws:sqs:checkout-queue-%03d", i), "name": fmt.Sprintf("checkout-queue-%03d", i), "kind": "sqs_queue",
			"resource_type": "aws_sqs_queue", "provider": "aws", "environment": "staging", "account_id": "000000000000", "region": "us-east-1",
			"arn":                fmt.Sprintf("arn:aws:sqs:us-east-1:000000000000:checkout-queue-%03d", i),
			"relationship_basis": "materialized_workload_cloud_relationship", "resolution_mode": "exact_arn",
			"evidence_source": "terraform_state", "service_anchor_source": "resource_tag", "service_anchor_reason": "service_tag_match",
		})
	}
	return map[string]any{
		"id": "workload:checkout-service", "name": "checkout-service", "kind": "service",
		"repo_id": "repo-checkout-service", "repo_name": "checkout-service",
		"instances": instances, "hostnames": hostnames, "entrypoints": entrypoints,
		"api_surface": map[string]any{
			"endpoint_count": rows, "method_count": 3 * rows, "spec_count": 1, "endpoints": endpoints,
		},
		"dependents": dependents, "consumer_repositories": consumers,
		"provisioning_source_chains": chains, "cloud_resources": cloud,
		"ci_cd_evidence": map[string]any{
			"run_count": 14, "workflow_count": 3, "latest_run_status": "success", "provider": "github_actions",
			"workflows": []string{"build.yaml", "deploy.yaml", "release.yaml"},
		},
		"supply_chain_evidence": map[string]any{
			"image_package": map[string]any{
				"candidate_image_ref_count": 2,
				"candidate_image_refs":      []string{"registry.example.test/team/checkout-service:prod", "registry.example.test/team/checkout-service:staging"},
				"missing_evidence":          []string{},
				"evidence": []map[string]any{
					{
						"source": "supply_chain_read_model", "image_ref": "registry.example.test/team/checkout-service:prod",
						"digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "repository_id": "repo-checkout-service",
						"identity_id": "identity-001", "identity_outcome": "exact", "identity_strength": "digest",
						"identity_evidence_fact_ids": []string{"fact-001", "fact-002"},
						"sbom_attachment_id":         "sbom-attachment-001", "sbom_document_id": "sbom-doc-001",
						"sbom_document_digest":   "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
						"sbom_attachment_status": "attached_verified", "sbom_artifact_kind": "sbom", "sbom_format": "spdx",
						"sbom_evidence_fact_ids": []string{"fact-003"}, "evidence_fact_ids": []string{"fact-001", "fact-002", "fact-003"},
					},
				},
			},
		},
		"documentation_overview": map[string]any{"repo_slug": "example/checkout-service", "docs_route_count": 2},
		"deployment_evidence": map[string]any{
			"artifacts": artifacts, "delivery_paths": deliveryPaths, "shared_config_paths": sharedConfig,
			"artifact_count": rows, "tool_families": []string{"argocd", "kustomize", "helm"},
		},
	}
}

// OversizedServiceDossierCandidatesOnly is the reachable shape of a service
// with no attached cloud resources and a full candidate list: the same large
// workload context as OversizedServiceDossierContext, with cloud_resources
// removed and 60 uncorrelated cloud-resource candidates, past the story's 50-row
// cap, carrying the status labels the candidate readers attach.
// The story enrichment loads candidates only when no cloud resource attached,
// and drops them when one did, so no real story carries both families.
func OversizedServiceDossierCandidatesOnly() map[string]any {
	const rows = 60
	candidateStatuses := []string{"uncorrelated", "ambiguous_anchor", "stale_anchor", "weak_anchor"}
	uncorrelated := make([]map[string]any, 0, rows)
	for i := range rows {
		uncorrelated = append(uncorrelated, map[string]any{
			"id": fmt.Sprintf("cloud:aws:sns:checkout-topic-%03d", i), "name": fmt.Sprintf("checkout-topic-%03d", i), "kind": "sns_topic",
			"resource_type": "aws_sns_topic", "provider": "aws", "environment": "staging", "source": "terraform_state",
			"config_path": fmt.Sprintf("env/staging/%03d/sns.tf", i), "arn": fmt.Sprintf("arn:aws:sns:us-east-1:000000000000:checkout-topic-%03d", i),
			"account_id": "000000000000", "region": "us-east-1", "service_anchor_status": "weak",
			"candidate_status": candidateStatuses[i%len(candidateStatuses)], "service_anchor_reason": "name_only_match",
			"missing_relationship": "workload_cloud_relationship", "resource_service": "sns", "service_kind": "sns",
		})
	}
	ctx := OversizedServiceDossierContext()
	delete(ctx, "cloud_resources")
	ctx["uncorrelated_cloud_resources"] = uncorrelated
	return ctx
}

// OversizedServiceDossierWithEntrypointFamilies is the standard large service
// plus the two lists the standard fixture leaves out: 60 entrypoint_candidates
// and 60 network_paths, each past the story's 50-row cap, with the row widths
// the enrichment builds. entrypoint_candidates grows with repository size (357
// rows on the largest QA service) and network_paths has one row per entrypoint
// that matches a runtime instance, so a large service can carry every family at
// its cap at once. The #8798 tests use it to prove the handles view projects
// both lists and to measure the story when nothing is below its cap.
func OversizedServiceDossierWithEntrypointFamilies() map[string]any {
	const rows = 60
	candidateClasses := []string{"ambiguous_hostname", "config_key", "field_path"}
	candidates := make([]map[string]any, 0, rows)
	networkPaths := make([]map[string]any, 0, rows)
	for i := range rows {
		candidates = append(candidates, map[string]any{
			"candidate":      fmt.Sprintf("checkout.settings.queue.endpoint%03d.internal", i),
			"classification": candidateClasses[i%len(candidateClasses)],
			"relative_path":  fmt.Sprintf("charts/checkout-service/config/settings-%03d.yaml", i),
			"reason":         "ambiguous_without_stronger_hostname_evidence",
		})
		networkPaths = append(networkPaths, map[string]any{
			"path_type": "hostname_to_runtime", "from_type": "hostname", "from": fmt.Sprintf("checkout-%03d.staging.example.test", i),
			"to_type": "runtime_platform", "to": fmt.Sprintf("eks-staging-%03d", i), "platform_kind": "argocd_applicationset",
			"environment": "staging", "reason": "exact_hostname_candidate_reference", "visibility": "public",
		})
	}
	ctx := OversizedServiceDossierContext()
	ctx["entrypoint_candidates"] = candidates
	ctx["network_paths"] = networkPaths
	return ctx
}
