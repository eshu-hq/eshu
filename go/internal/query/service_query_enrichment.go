// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
)

type serviceQueryEnrichmentOptions struct {
	DirectOnly                bool
	IncludeRelatedModuleUsage bool
	MaxDepth                  int
	Logger                    *slog.Logger
	Operation                 string
}

func enrichServiceQueryContext(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	workloadContext map[string]any,
) error {
	return enrichServiceQueryContextWithOptions(ctx, graph, content, workloadContext, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Operation:                 "service_context",
	})
}

func enrichServiceQueryContextWithOptions(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	workloadContext map[string]any,
	opts serviceQueryEnrichmentOptions,
) error {
	delete(workloadContext, "entry_points")
	if len(workloadContext) == 0 {
		return nil
	}

	repoID := safeStr(workloadContext, "repo_id")
	serviceName := safeStr(workloadContext, "name")
	operation := opts.Operation
	if operation == "" {
		operation = "service_context"
	}
	timer := startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "graph_api_surface")
	if graphAPISurface := queryServiceGraphAPISurface(ctx, graph, repoID); len(graphAPISurface) > 0 {
		workloadContext["api_surface"] = graphAPISurface
	}
	timer.Done(ctx, slog.Bool("has_result", len(mapValue(workloadContext, "api_surface")) > 0))
	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "graph_deployment_evidence")
	graphEvidence, err := queryServiceGraphDeploymentEvidence(ctx, graph, content, repoID)
	if err != nil {
		timer.Done(ctx, slog.Bool("error", true))
		return fmt.Errorf("load graph deployment evidence: %w", err)
	}
	if len(graphEvidence) > 0 {
		workloadContext["deployment_evidence"] = graphEvidence
	}
	timer.Done(ctx, slog.Bool("has_result", len(mapValue(workloadContext, "deployment_evidence")) > 0))
	if repoID == "" || serviceName == "" || content == nil {
		return nil
	}

	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "service_evidence_content")
	evidence, err := loadServiceQueryEvidence(ctx, content, repoID, serviceName)
	timer.Done(
		ctx,
		slog.Int("hostname_count", len(evidence.Hostnames)),
		slog.Int("environment_count", len(evidence.Environments)),
	)
	if err != nil {
		return fmt.Errorf("load service query evidence: %w", err)
	}

	// Load framework-detected routes from fact_records when ContentReader
	// is available (it has access to the same Postgres database).
	if content != nil {
		timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "framework_routes")
		frameworkRoutes, err := content.ListFrameworkRoutes(ctx, repoID)
		timer.Done(ctx, slog.Int("row_count", len(frameworkRoutes)))
		if err != nil {
			return fmt.Errorf("load framework routes: %w", err)
		}
		evidence.FrameworkRoutes = frameworkRoutes
	}

	if hostnames := buildServiceHostnameRows(evidence.Hostnames); len(hostnames) > 0 {
		workloadContext["hostnames"] = hostnames
	}
	if candidates := buildServiceEntrypointCandidateRows(evidence.EntrypointCandidates); len(candidates) > 0 {
		workloadContext["entrypoint_candidates"] = candidates
	}
	if entrypoints := buildServiceEntrypoints(workloadContext, evidence); len(entrypoints) > 0 {
		workloadContext["entrypoints"] = entrypoints
	}

	instanceEnvironments, _ := workloadContext["instances"].([]map[string]any)
	observedEnvironments := mergeStringSets(
		distinctSortedInstanceField(instanceEnvironments, "environment"),
		serviceEvidenceEnvironmentNames(evidence.Environments),
	)
	if len(observedEnvironments) > 0 {
		workloadContext["observed_config_environments"] = observedEnvironments
	}

	if apiSurface := buildServiceAPISurface(evidence); len(apiSurface) > 0 && len(mapValue(workloadContext, "api_surface")) == 0 {
		workloadContext["api_surface"] = apiSurface
	}
	if networkPaths := buildServiceNetworkPaths(workloadContext, mapSliceValue(workloadContext, "entrypoints")); len(networkPaths) > 0 {
		workloadContext["network_paths"] = networkPaths
	}

	if graph != nil {
		hostnames := serviceEvidenceHostnames(evidence)
		traceLimit := boundedTraceEnrichmentLimit(opts.MaxDepth)
		candidates := []provisioningRepositoryCandidate{}
		if !opts.DirectOnly || opts.IncludeRelatedModuleUsage {
			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "graph_provisioning_candidates")
			candidates, err = queryProvisioningRepositoryCandidates(ctx, graph, repoID, traceLimit)
			timer.Done(ctx, slog.Int("row_count", len(candidates)))
			if err != nil {
				return fmt.Errorf("load graph provisioning candidates: %w", err)
			}
		}
		if !opts.DirectOnly {
			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "graph_dependents")
			if dependents := buildGraphDependents(candidates); len(dependents) > 0 {
				workloadContext["dependents"] = dependents
			}
			timer.Done(ctx, slog.Int("row_count", len(candidates)))

			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "consumer_repository_enrichment")
			consumers, err := loadConsumerRepositoryEnrichmentFromCandidates(ctx, graph, content, repoID, serviceName, hostnames, traceLimit, candidates)
			timer.Done(ctx, slog.Int("row_count", len(consumers)))
			if err != nil {
				return fmt.Errorf("load consumer repository enrichment: %w", err)
			}
			if len(consumers) > 0 {
				workloadContext["consumer_repositories"] = consumers
			}
		}

		if opts.IncludeRelatedModuleUsage {
			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "provisioning_source_chains")
			provisioningChains, err := loadProvisioningSourceChainsFromCandidates(ctx, content, candidates)
			timer.Done(ctx, slog.Int("row_count", len(provisioningChains)))
			if err != nil {
				return fmt.Errorf("load provisioning source chains: %w", err)
			}
			if len(provisioningChains) > 0 {
				workloadContext["provisioning_source_chains"] = provisioningChains
			}
		}
		if len(mapSliceValue(workloadContext, "cloud_resources")) == 0 {
			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "cloud_resource_dependencies")
			workloadID := safeStr(workloadContext, "id")
			cloudResources, err := loadMaterializedServiceCloudResourceDependencies(ctx, graph, workloadID, serviceStoryItemLimit)
			timer.Done(ctx, slog.Int("row_count", len(cloudResources)))
			if err != nil {
				return fmt.Errorf("load service cloud resource dependencies: %w", err)
			}
			if len(cloudResources) > 0 {
				workloadContext["cloud_resources"] = cloudResources
				delete(workloadContext, "uncorrelated_cloud_resources")
			}
		}
		if len(mapSliceValue(workloadContext, "cloud_resources")) == 0 {
			timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "uncorrelated_cloud_resource_candidates")
			cloudCandidates, cloudCandidatesTruncated, err := loadUncorrelatedCloudResourceCandidatesBounded(ctx, graph, serviceName, serviceStoryItemLimit)
			timer.Done(
				ctx,
				slog.Int("row_count", len(cloudCandidates)),
				slog.Bool("truncated", cloudCandidatesTruncated),
			)
			if err != nil {
				return fmt.Errorf("load uncorrelated cloud resource candidates: %w", err)
			}
			if len(cloudCandidates) > 0 {
				workloadContext["uncorrelated_cloud_resources"] = cloudCandidates
				if cloudCandidatesTruncated {
					workloadContext["uncorrelated_cloud_resources_truncated"] = true
				}
			}
		}

		// Ingress posture (WAF coverage + TLS termination) is derived strictly
		// from the two materialized AWS protection edges on the service's own
		// internet-facing edge resources. It runs at most one bounded graph query,
		// and only when such an edge resource is present, so the few-seconds
		// context SLA is preserved. With no edge resource it reports an honest
		// unproven posture rather than implying protection.
		timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "ingress_posture")
		ingressPosture, err := loadServiceIngressPosture(ctx, graph, mapSliceValue(workloadContext, "cloud_resources"))
		timer.Done(
			ctx,
			slog.String("waf_coverage", StringVal(ingressPosture, "waf_coverage")),
			slog.String("tls_termination", StringVal(ingressPosture, "tls_termination")),
			slog.Int("edge_count", IntVal(ingressPosture, "edge_count")),
		)
		if err != nil {
			return fmt.Errorf("load service ingress posture: %w", err)
		}
		if len(ingressPosture) > 0 {
			workloadContext["ingress_posture"] = ingressPosture
		}
	}

	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "documentation_overview")
	documentationOverview := buildServiceDocumentationOverview(ctx, graph, workloadContext, evidence)
	targetDocumentation, err := loadServiceStoryTargetDocumentationForOperation(ctx, content, workloadContext, operation)
	timer.Done(
		ctx,
		slog.Bool("has_result", len(documentationOverview) > 0 || len(targetDocumentation) > 0),
		slog.Bool("has_target_documentation", len(targetDocumentation) > 0),
		slog.Int("target_documentation_finding_count", IntVal(targetDocumentation, "finding_count")),
		slog.Bool("error", err != nil),
	)
	if err != nil {
		return fmt.Errorf("load service story target documentation: %w", err)
	}
	documentationOverview = attachStoryTargetDocumentation(documentationOverview, targetDocumentation)
	if len(documentationOverview) > 0 {
		workloadContext["documentation_overview"] = documentationOverview
	}
	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "deployment_evidence")
	deploymentEvidence, err := loadServiceDeploymentEvidence(ctx, graph, content, workloadContext)
	timer.Done(ctx, slog.Bool("has_result", len(deploymentEvidence) > 0))
	if err != nil {
		return fmt.Errorf("load service deployment evidence: %w", err)
	}
	if len(deploymentEvidence) > 0 {
		if graphEvidence := mapValue(workloadContext, "deployment_evidence"); len(graphEvidence) > 0 {
			deploymentEvidence = mergeServiceDeploymentEvidence(deploymentEvidence, graphEvidence)
		}
		workloadContext["deployment_evidence"] = deploymentEvidence
	}
	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "support_target_evidence")
	targetSupport, err := loadServiceStoryTargetSupportForOperation(ctx, content, workloadContext, operation)
	timer.Done(
		ctx,
		slog.Bool("has_result", len(targetSupport) > 0),
		slog.Int("target_support_evidence_count", IntVal(targetSupport, "evidence_count")),
		slog.Bool("error", err != nil),
	)
	if err != nil {
		return fmt.Errorf("load service story target support: %w", err)
	}
	if len(targetSupport) > 0 {
		workloadContext["target_support"] = targetSupport
	}
	buildCtx := newServiceStoryBuildContext(workloadContext)
	if supportOverview := buildServiceSupportOverviewWithContext(buildCtx); len(supportOverview) > 0 {
		workloadContext["support_overview"] = supportOverview
	}
	timer = startServiceQueryStage(ctx, opts.Logger, operation, serviceName, repoID, "overview_assembly")
	workloadContext["deployment_overview"] = buildServiceDeploymentOverviewWithContext(buildCtx)
	workloadContext["story_sections"] = buildServiceStorySectionsWithContext(buildCtx)
	cacheServiceStoryBuildContext(buildCtx)
	timer.Done(ctx)

	return nil
}
