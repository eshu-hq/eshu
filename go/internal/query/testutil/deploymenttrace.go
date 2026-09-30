// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testutil

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// deploymenttrace.go holds the every-family-at-cap trace_deployment_chain
// workload context (#7174). The deployment package's size tests and the MCP
// package's dispatch-budget test both shape this one fixture, so the two
// layers measure the same rows instead of drifting copies. Row widths are
// hand-built from the producer code (+/-30%), not recorded from a live graph.

// TraceAtCapScenario names one way to fill every family the trace route emits.
type TraceAtCapScenario struct {
	// Name labels the scenario in test logs.
	Name string
	// PerFamily is the row count for every capped primary family: instances,
	// deployment sources, cloud and k8s resources, controllers, image refs,
	// hostnames, entrypoints, network paths, and deployment artifacts.
	PerFamily int
	// EnrichmentRows is the row count for dependents, consumer repositories,
	// and provisioning source chains (25 at the default max_depth, 100 at
	// the max_depth clamp).
	EnrichmentRows int
	// PlatformsPerInst is the nested instance.platforms row count (the route
	// caps platform edges at 2500 in total).
	PlatformsPerInst int
	// OverviewCopiesRows makes TraceAtCapOverview copy the hostname,
	// entrypoint, and api_surface lists into deployment_overview, as the
	// service-story overview builder may.
	OverviewCopiesRows bool
}

func traceAtCapStrings(n int, format string) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf(format, i))
	}
	return out
}

func traceAtCapRows(n int, build func(i int) map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, build(i))
	}
	return rows
}

// TraceAtCapWorkloadContext fills every family BuildDeploymentTraceResponse reads
// with row shapes copied from the producers (trace_deployment_sources.go,
// deployment/cloud_evidence.go, trace_deployment_k8s_select.go,
// deployment/gitops_helpers.go, repository.BuildGraphDeploymentEvidence,
// oci.JoinTagRepositoryImage, entity workload runtime topology).
func TraceAtCapWorkloadContext(sc TraceAtCapScenario) map[string]any {
	n := sc.PerFamily
	instances := traceAtCapRows(n, func(i int) map[string]any {
		id := fmt.Sprintf("workload-instance:payments-api:prod-us-east-1-%03d", i)
		platforms := traceAtCapRows(sc.PlatformsPerInst, func(p int) map[string]any {
			platformID := fmt.Sprintf("platform:kubernetes:aws:123456789012:us-east-1:prod-cluster-%03d-%02d", i, p)
			return map[string]any{
				"platform_id":         platformID,
				"platform_name":       fmt.Sprintf("prod-cluster-%03d-%02d", i, p),
				"platform_kind":       "kubernetes",
				"platform_confidence": 0.98,
				"platform_reason":     "workload instance runs on the platform discovered from argocd destination",
				"topology_basis":      "direct_runtime",
				"topology_edges": []map[string]any{{
					"relationship_type": "RUNS_ON", "source_id": id, "target_id": platformID,
					"target_name": fmt.Sprintf("prod-cluster-%03d-%02d", i, p), "confidence": 0.98,
					"reason":     "workload instance runs on the platform discovered from argocd destination",
					"properties": map[string]any{"confidence": 0.98, "reason": "argocd destination", "evidence_source": "resolver/cross-repo"},
				}},
			}
		})
		return map[string]any{
			"instance_id": id, "platform_name": fmt.Sprintf("prod-cluster-%03d-00", i), "platform_kind": "kubernetes",
			"platforms": platforms, "environment": fmt.Sprintf("prod-%03d", i%12),
			"materialization_confidence": 0.97,
			"materialization_provenance": []string{"argocd_application", "kustomize_overlay"},
			"platform_confidence":        0.98, "platform_reason": "workload instance runs on the platform discovered from argocd destination",
		}
	})
	topologyEdges := []map[string]any{{
		"relationship_type": "DEFINES", "source_id": "repo:payments", "source_name": "payments",
		"target_id": "workload:payments-api", "target_name": "payments-api", "confidence": 1.0, "reason": "",
		"properties": map[string]any{"confidence": 1.0, "evidence_source": "projector/workload"},
	}}
	for _, inst := range instances {
		id := querycontract.StringVal(inst, "instance_id")
		topologyEdges = append(topologyEdges, map[string]any{
			"relationship_type": "INSTANCE_OF", "source_id": id, "target_id": "workload:payments-api",
			"target_name": "payments-api", "confidence": 0.97, "reason": "",
			"properties": map[string]any{"confidence": 0.97, "evidence_source": "projector/workload"},
		})
	}
	deploymentSources := traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"repo_id": fmt.Sprintf("repository:r_%08x", i), "repo_name": fmt.Sprintf("deployment-charts-%03d", i),
			"relationship_type": "DEPLOYS_FROM", "source_id": fmt.Sprintf("repository:r_%08x", i),
			"target_id": "repository:r_payments", "confidence": 0.93,
			"reason": "argocd application source path references the traced service repository",
		}
	})
	cloudResources := traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"id": fmt.Sprintf("cloud:aws:123456789012:us-east-1:ecs-service:payments-api-%03d", i), "name": fmt.Sprintf("payments-api-%03d", i),
			"kind": "ecs_service", "resource_type": "aws_ecs_service", "provider": "aws", "environment": "prod",
			"confidence": 0.95, "reason": "terraform resource references the workload image and service name",
			"relationship_basis": "config_declared", "resolution_mode": "deterministic", "evidence_source": "resolver/cross-repo",
			"service_anchor_source": "terraform_state", "service_anchor_reason": "state address matches service name",
			"source_fact_id": fmt.Sprintf("fact:0123456789abcdef0123456789abcdef%04d", i), "stable_fact_key": fmt.Sprintf("aws:ecs:payments-api-%03d", i),
			"source_system": "terraform_state", "source_record_id": fmt.Sprintf("aws_ecs_service.payments_api_%03d", i), "collector_kind": "terraform_state",
		}
	})
	k8sResources := traceAtCapRows(n, func(i int) map[string]any {
		kind := []string{"Deployment", "Service", "ConfigMap", "HorizontalPodAutoscaler"}[i%4]
		return map[string]any{
			"entity_id": fmt.Sprintf("entity:k8s:%016x", i), "repo_id": "repository:r_payments",
			"entity_name": fmt.Sprintf("payments-api-%s-%03d", kind, i), "kind": kind,
			"qualified_name":   fmt.Sprintf("prod/%s/payments-api-%03d", kind, i),
			"relative_path":    fmt.Sprintf("deploy/overlays/prod/%s-%03d.yaml", kind, i),
			"container_images": []string{fmt.Sprintf("123456789012.dkr.ecr.us-east-1.amazonaws.com/payments-api:v1.%d.%d", i/10, i%10)},
			"namespace":        "payments-prod", "api_version": "apps/v1",
		}
	})
	imageRefs := make([]string, 0, n)
	registryTruth := traceAtCapRows(n, func(i int) map[string]any {
		ref := fmt.Sprintf("123456789012.dkr.ecr.us-east-1.amazonaws.com/payments-api:v1.%d.%d", i/10, i%10)
		imageRefs = append(imageRefs, ref)
		return map[string]any{
			"image_ref": ref, "tag": fmt.Sprintf("v1.%d.%d", i/10, i%10),
			"digest":   fmt.Sprintf("sha256:%064x", i+1),
			"image_id": fmt.Sprintf("oci-image://123456789012.dkr.ecr.us-east-1.amazonaws.com/payments-api@sha256:%064x", i+1),
			"registry": "123456789012.dkr.ecr.us-east-1.amazonaws.com", "repository": "payments-api",
			"repository_id": "oci-repository://123456789012.dkr.ecr.us-east-1.amazonaws.com/payments-api",
			"media_type":    "application/vnd.oci.image.manifest.v1+json", "provider": "ecr",
		}
	})
	controllers := traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"entity_id": fmt.Sprintf("entity:argocd:%016x", i), "entity_type": "ArgoCDApplication",
			"entity_name": fmt.Sprintf("payments-api-prod-%03d", i), "controller_kind": "argocd_application",
			"repo_id": "repository:r_gitops", "relative_path": fmt.Sprintf("argocd/prod/payments-api-%03d/application.yaml", i),
			"source_repo": "https://github.com/example/deployment-charts", "source_path": fmt.Sprintf("charts/payments-api/overlays/prod-%03d", i),
			"source_ref_kind": "branch", "source_ref_name": "main", "source_ref_namespace": "argocd", "namespace": "argocd",
			"generator_source_repos": []string{}, "generator_source_paths": []string{},
			"template_source_repos": []string{}, "template_source_paths": []string{},
			"dest_server": "https://kubernetes.default.svc", "dest_namespace": "payments-prod",
			"source_root":  fmt.Sprintf("charts/payments-api/overlays/prod-%03d", i),
			"source_roots": []string{fmt.Sprintf("charts/payments-api/overlays/prod-%03d", i)},
		}
	})
	deploymentEvidence := traceAtCapDeploymentEvidence(n)
	hostnames, entrypoints, networkPaths, endpoints := traceAtCapServiceEvidence(n)
	dependents, consumers, chains := traceAtCapEnrichment(sc.EnrichmentRows)
	provisioned := traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"platform_source_id": fmt.Sprintf("repository:r_tf%06x", i), "platform_source_name": fmt.Sprintf("terraform-runtime-%03d", i),
			"platform_id": fmt.Sprintf("platform:ecs:aws:123456789012:us-east-1:cluster-%03d", i), "platform_name": fmt.Sprintf("cluster-%03d", i),
			"platform_kind": "ecs", "platform_confidence": 0.9, "platform_reason": "terraform provisions the platform",
			"relationship_type": "PROVISIONS_PLATFORM", "confidence": 0.9,
		}
	})
	return map[string]any{
		"id": "workload:payments-api", "name": "payments-api", "kind": "service", "repo_id": "repository:r_payments", "repo_name": "payments",
		"instances": instances, "topology_edges": topologyEdges, "provisioned_platforms": provisioned,
		"deployment_sources": deploymentSources, "cloud_resources": cloudResources, "k8s_resources": k8sResources,
		"image_refs": imageRefs, "image_registry_truth": registryTruth, "controller_entities": controllers,
		"deployment_evidence": deploymentEvidence, "hostnames": hostnames, "entrypoints": entrypoints, "network_paths": networkPaths,
		"api_surface": map[string]any{"endpoint_count": n, "method_count": n * 3, "spec_count": 1, "endpoints": endpoints},
		"dependents":  dependents, "consumer_repositories": consumers, "provisioning_source_chains": chains,
		"documentation_overview":       map[string]any{"repo_slug": "example/payments", "docs_route_count": 12},
		"support_overview":             map[string]any{"owner_team": "payments", "on_call": "payments-oncall"},
		"observed_config_environments": []string{"prod", "staging", "qa"},
		"deployment_source_limits":     map[string]any{"limit": n, "query_sentinel_limit": n + 1, "returned_count": n, "observed_count": n, "truncated": false, "ordering": []string{"relationship_type_priority", "repo_name", "source_id", "target_id"}},
		"cloud_resource_limits":        map[string]any{"limit": n, "query_sentinel_limit": n + 1, "returned_count": n, "observed_count": n, "truncated": false, "observation_limit": 2500},
		"k8s_resource_limits":          map[string]any{"limit": n, "query_sentinel_limit": n + 1, "returned_count": n, "observed_count": n, "truncated": false},
		"controller_entity_limits":     map[string]any{"limit": n, "returned_count": n, "truncated": false},
		"runtime_topology_limits":      map[string]any{"instances": map[string]any{"limit": n, "truncated": false}, "platform_edges": map[string]any{"limit": 2500, "truncated": false}},
		"_has_live_evidence":           true,
	}
}

// traceAtCapDeploymentEvidence builds deployment_evidence with n artifacts in
// the repository.BuildGraphDeploymentEvidence shape, index included.
func traceAtCapDeploymentEvidence(n int) map[string]any {
	artifacts := traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"id": fmt.Sprintf("evidence-artifact:%040x", i), "direction": "incoming", "name": fmt.Sprintf("payments-api-prod-%03d", i),
			"domain": "deployment", "path": fmt.Sprintf("argocd/prod/payments-api-%03d/application.yaml", i),
			"evidence_kind": "ARGOCD_APPLICATION_SOURCE", "artifact_family": "argocd", "extractor": "argocd_application_source",
			"relationship_type": "DEPLOYS_FROM", "source_repo_id": "repository:r_gitops", "source_repo_name": "deployment-charts",
			"target_repo_id": "repository:r_payments", "target_repo_name": "payments",
			"evidence_source": "resolver/cross-repo", "source_repo_canonical_id": "github.com/example/deployment-charts",
			"source_repo_scope_key": "scope:s_0a1b2c3d", "target_repo_canonical_id": "github.com/example/payments",
			"target_repo_scope_key": "scope:s_4e5f6a7b", "resolved_id": fmt.Sprintf("resolved:%040x", i),
			"postgres_lookup_basis": "resolved_id", "generation_id": fmt.Sprintf("generation:%032x", i),
			"confidence": 0.94, "start_line": 10 + i, "end_line": 24 + i, "environment": "prod", "runtime_platform_kind": "kubernetes",
			"matched_alias": "payments-api", "matched_value": "charts/payments-api", "commit_sha": "0123456789abcdef0123456789abcdef01234567",
			"source_location": map[string]any{
				"repo_id": "repository:r_gitops", "repo_name": "deployment-charts",
				"path": fmt.Sprintf("argocd/prod/payments-api-%03d/application.yaml", i), "start_line": 10 + i, "end_line": 24 + i,
			},
		}
	})
	resolvedIDs := make([]string, 0, n)
	generationIDs := make([]string, 0, n)
	for _, a := range artifacts {
		resolvedIDs = append(resolvedIDs, querycontract.StringVal(a, "resolved_id"))
		generationIDs = append(generationIDs, querycontract.StringVal(a, "generation_id"))
	}
	bucket := map[string]any{
		"artifact_count": n, "resolved_ids": resolvedIDs, "generation_ids": generationIDs,
		"evidence_kinds": []string{"ARGOCD_APPLICATION_SOURCE"}, "artifact_families": []string{"argocd"}, "relationship_types": []string{"DEPLOYS_FROM"},
	}
	deploymentEvidence := map[string]any{
		"truth_basis": "graph", "artifact_count": n, "ci_artifact_count": 0, "environment_count": 1, "artifacts": artifacts,
		"evidence_index": map[string]any{
			"lookup_basis":       "resolved_id",
			"relationship_types": map[string]any{"DEPLOYS_FROM": bucket}, "artifact_families": map[string]any{"argocd": bucket},
			"evidence_kinds": map[string]any{"ARGOCD_APPLICATION_SOURCE": bucket},
		},
		"artifact_families": []string{"argocd"}, "evidence_kinds": []string{"ARGOCD_APPLICATION_SOURCE"},
		"relationship_types": []string{"DEPLOYS_FROM"}, "environments": []string{"prod"},
		"source_repo_ids": []string{"repository:r_gitops"}, "target_repo_ids": []string{"repository:r_payments"}, "artifact_limit": n,
	}
	// The content-derived lists service.buildServiceDeploymentEvidenceFromOverview
	// adds when the graph holds no evidence (#7174 review F3). They come from
	// repository files with no row cap of their own; the fixture holds 2n of
	// each, a modest repository.
	deploymentEvidence["shared_config_paths"] = traceAtCapStrings(2*n, "deploy/config/payments-api-%03d.yaml")
	deploymentEvidence["delivery_paths"] = traceAtCapRows(2*n, func(i int) map[string]any {
		return map[string]any{
			"kind": "workflow", "path": fmt.Sprintf(".github/workflows/deploy-payments-api-%03d.yaml", i),
			"workflow": fmt.Sprintf("deploy-payments-api-%03d", i), "source_repo": "payments", "environment": "prod",
		}
	})
	// deployment_artifacts is a map of lists, the shape
	// repositoryartifacts.MergeDeploymentArtifactMaps produces.
	artifactRows := func(kind string) []map[string]any {
		return traceAtCapRows(2*n, func(i int) map[string]any {
			return map[string]any{
				"artifact_type": kind, "path": fmt.Sprintf("build/%s/payments-api-%03d/Dockerfile", kind, i),
				"repository": "payments", "image": fmt.Sprintf("registry.example.test/payments-api-%03d", i),
			}
		})
	}
	deploymentEvidence["deployment_artifacts"] = map[string]any{
		"controller_artifacts": artifactRows("controller"), "workflow_artifacts": artifactRows("workflow"),
		"deployment_artifacts": artifactRows("deployment"), "config_paths": artifactRows("config"),
	}
	// The story keys are lists of sentences (repository/deployment_overview_story.go).
	deploymentEvidence["topology_story"] = []string{
		"payments-api ships from deployment-charts through ArgoCD to the prod cluster.",
		"The prod overlay pins the image by digest.",
	}
	deploymentEvidence["delivery_family_story"] = []string{"GitHub Actions builds the image and ArgoCD syncs the chart."}
	return deploymentEvidence
}

// traceAtCapServiceEvidence builds n hostname, entrypoint, network path, and
// api_surface endpoint rows.
func traceAtCapServiceEvidence(n int) (hostnames, entrypoints, networkPaths, endpoints []map[string]any) {
	hostnames = traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"hostname": fmt.Sprintf("payments-api-%03d.prod.example.test", i), "environment": "prod", "visibility": "public",
			"relative_path": fmt.Sprintf("deploy/overlays/prod/ingress-%03d.yaml", i), "source_repo": "payments", "evidence_kind": "ingress_host",
		}
	})
	entrypoints = traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"hostname": fmt.Sprintf("payments-api-%03d.prod.example.test", i), "environment": "prod", "visibility": "public",
			"relative_path": fmt.Sprintf("deploy/overlays/prod/ingress-%03d.yaml", i), "source_repo": "payments", "entrypoint_type": "ingress",
		}
	})
	networkPaths = traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"path_type": "hostname_to_runtime", "from": fmt.Sprintf("payments-api-%03d.prod.example.test", i),
			"to": fmt.Sprintf("prod-cluster-%03d-00", i), "via": "ingress", "environment": "prod", "platform": "kubernetes",
			"reason": "ingress host routes to the workload service on the discovered platform",
		}
	})
	endpoints = traceAtCapRows(n, func(i int) map[string]any {
		return map[string]any{
			"path": fmt.Sprintf("/v3/payments/%03d/{id}", i), "methods": []string{"get", "post", "delete"},
			"operation_ids": []string{fmt.Sprintf("getPayment%03d", i), fmt.Sprintf("createPayment%03d", i)}, "spec_path": "specs/index.yaml",
		}
	})
	return hostnames, entrypoints, networkPaths, endpoints
}

// traceAtCapEnrichment builds enrich dependent, consumer repository, and
// provisioning source chain rows.
func traceAtCapEnrichment(enrich int) (dependents, consumers, chains []map[string]any) {
	dependents = traceAtCapRows(enrich, func(i int) map[string]any {
		return map[string]any{
			"repository": fmt.Sprintf("deployment-helm-%03d", i), "repo_id": fmt.Sprintf("repository:r_dep%05x", i),
			"relationship_types": []string{"DEPLOYS_FROM", "READS_CONFIG_FROM"},
		}
	})
	consumers = traceAtCapRows(enrich, func(i int) map[string]any {
		return map[string]any{
			"repository": fmt.Sprintf("consumer-api-%03d", i), "repo_id": fmt.Sprintf("repository:r_con%05x", i),
			"evidence_kinds": []string{"hostname_reference", "content_search"}, "matched_values": []string{fmt.Sprintf("payments-api-%03d.prod.example.test", i)},
			"sample_paths": []string{"config/prod.json", "deploy/values.yaml", "src/client/payments.go"},
		}
	})
	chains = traceAtCapRows(enrich, func(i int) map[string]any {
		return map[string]any{
			"repository": fmt.Sprintf("terraform-runtime-%03d", i), "repo_id": fmt.Sprintf("repository:r_tf%06x", i),
			"modules": []string{"ecs_service", "alb", "iam_role"}, "relationship_types": []string{"PROVISIONS_DEPENDENCY_FOR"},
			"sample_paths": []string{"env/prod/ecs.tf", "modules/ecs/main.tf"},
		}
	})
	return dependents, consumers, chains
}

// TraceAtCapOverview mirrors the caller-built deployment_overview. withCopies adds
// the normalized hostnames/entrypoints/api_surface the service-story builder
// is documented (response.go attachDeploymentEvidenceOverview) to own.
func TraceAtCapOverview(ctx map[string]any, withCopies bool) map[string]any {
	overview := map[string]any{"deployment_truth_tier": "runtime_confirmed", "deployment_truth_basis": "live_cluster_observation"}
	if withCopies {
		overview["hostnames"] = ctx["hostnames"]
		overview["entrypoints"] = ctx["entrypoints"]
		overview["api_surface"] = ctx["api_surface"]
	}
	return overview
}
