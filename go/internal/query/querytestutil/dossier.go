// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

// SampleServiceDossierContext builds the canonical sample-service workload
// context map the service-story and deployment-trace response tests shape
// fixtures from. It moved here from the query root with lane B2 of #6060
// because root, impact/, and impacttrace/ tests all need it and test files
// cannot share helpers across packages.
func SampleServiceDossierContext() map[string]any {
	return map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-sample-service-api",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{"instance_id": "inst-prod", "platform_name": "eks-prod", "platform_kind": "argocd_applicationset", "environment": "production"},
			{"instance_id": "inst-qa", "platform_name": "ecs-qa", "platform_kind": "ecs_service", "environment": "qa"},
		},
		"api_surface": map[string]any{
			"endpoint_count": 2,
			"method_count":   3,
			"spec_count":     1,
			"endpoints": []map[string]any{
				{"path": "/v3/items", "methods": []string{"get"}, "operation_ids": []string{"listItems"}, "spec_path": "specs/index.yaml"},
				{"path": "/v3/items/{id}", "methods": []string{"get", "delete"}, "operation_ids": []string{"getItem", "deleteItem"}, "spec_path": "specs/index.yaml"},
			},
		},
		"documentation_overview": map[string]any{
			"repo_slug":        "example/sample-service-api",
			"docs_route_count": 2,
		},
		"dependencies": []map[string]any{
			{"type": "READS_CONFIG_FROM", "target_name": "config-service", "target_id": "repo-config"},
		},
		"dependents": []map[string]any{
			{"repository": "deployment-helm", "repo_id": "repo-helm", "relationship_types": []string{"DEPLOYS_FROM"}},
		},
		"consumer_repositories": []map[string]any{
			{"repository": "sample-search-api", "repo_id": "repo-search", "evidence_kinds": []string{"hostname_reference"}, "matched_values": []string{"sample-service-api.qa.example.test"}, "sample_paths": []string{"config/qa.json"}},
		},
		"provisioning_source_chains": []map[string]any{
			{"repository": "terraform-runtime", "repo_id": "repo-terraform", "modules": []string{"ecs_service"}},
		},
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				{"id": "artifact-gitops", "direction": "incoming", "relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-gitops", "confidence": 0.94, "artifact_family": "argocd", "source_repo_id": "repo-gitops", "source_repo_name": "deployment-charts", "target_repo_id": "repo-sample-service-api", "target_repo_name": "sample-service-api", "path": "argocd/prod/app.yaml"},
				{"id": "artifact-terraform", "direction": "incoming", "relationship_type": "PROVISIONS_DEPENDENCY_FOR", "resolved_id": "resolved-terraform", "confidence": 0.91, "artifact_family": "terraform", "runtime_platform_kind": "ecs_service", "source_repo_id": "repo-terraform", "source_repo_name": "terraform-runtime", "target_repo_id": "repo-sample-service-api", "target_repo_name": "sample-service-api", "path": "env/qa/ecs.tf"},
			},
		},
	}
}
