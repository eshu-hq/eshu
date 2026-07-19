// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

type traceDeploymentChainRequest struct {
	ServiceName               string `json:"service_name"`
	DirectOnly                bool   `json:"direct_only"`
	MaxDepth                  int    `json:"max_depth"`
	IncludeRelatedModuleUsage bool   `json:"include_related_module_usage"`
}

type traceEnrichmentConfig struct {
	includeConsumers          bool
	includeProvisioningChains bool
	maxDepth                  int
}

func traceEnrichmentOptions(req traceDeploymentChainRequest) traceEnrichmentConfig {
	includeConsumers := !req.DirectOnly
	return traceEnrichmentConfig{
		includeConsumers:          includeConsumers,
		includeProvisioningChains: includeConsumers && req.IncludeRelatedModuleUsage,
		maxDepth:                  req.MaxDepth,
	}
}

// traceDeploymentChain returns a story-first deployment trace for a service.
// POST /api/v0/impact/trace-deployment-chain
func (h *ImpactHandler) traceDeploymentChain(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.deployment_chain") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"deployment-chain tracing requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.deployment_chain",
			h.profile(),
			requiredProfile("platform_impact.deployment_chain"),
		)
		return
	}

	var req traceDeploymentChainRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ServiceName == "" {
		WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}

	traceOptions := traceEnrichmentOptions(req)
	ctx, err := fetchServiceTraceContext(r.Context(), h.Neo4j, h.Content, h.Logger, req.ServiceName, traceOptions)
	if err != nil {
		if errors.Is(err, errAmbiguousTraceWorkloadSelector) {
			WriteError(w, http.StatusConflict, err.Error())
			return
		}
		if writeContentSubstringIndexUnavailable(w, err) {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if ctx == nil {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if workloadID := safeStr(ctx, "id"); workloadID != "" {
		deploymentSourceResult, err := h.fetchDeploymentSourceResult(r.Context(), workloadID, safeStr(ctx, "repo_id"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment sources: %v", err))
			return
		}
		deploymentSources := deploymentSourceResult.rows
		cloudResourceResult, err := h.fetchCloudResourceResult(r.Context(), workloadID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query cloud resources: %v", err))
			return
		}
		cloudResources := cloudResourceResult.rows
		if len(cloudResources) == 0 {
			contextRows := mapSliceValue(ctx, "cloud_resources")
			contextRows, _ = capMapRows(contextRows, serviceStoryItemLimit)
			if len(contextRows) > 0 {
				cloudResourceResult.rows = contextRows
				cloudResourceResult.limits = nil
				cloudResources = cloudResourceResult.rows
			}
		}
		if len(cloudResources) == 0 && len(mapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
			configRows, configTruncated, configErr := loadConfigDerivedCloudResourceDependenciesBounded(
				r.Context(),
				h.Neo4j,
				mapValue(ctx, "deployment_evidence"),
				serviceStoryItemLimit,
			)
			if configErr != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query config-derived cloud resources: %v", configErr))
				return
			}
			if configTruncated {
				ctx["uncorrelated_cloud_resources_truncated"] = true
			}
			if len(configRows) > 0 && len(mapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
				ctx["uncorrelated_cloud_resources"] = deploymentTraceCloudCandidates(configRows)
			}
		}
		if len(cloudResources) > 0 {
			ctx["cloud_resources"] = cloudResources
			delete(ctx, "uncorrelated_cloud_resources")
		} else if len(mapSliceValue(ctx, "uncorrelated_cloud_resources")) == 0 {
			cloudCandidates, cloudCandidatesTruncated, err := loadUncorrelatedCloudResourceCandidatesBounded(
				r.Context(), h.Neo4j, safeStr(ctx, "name"), serviceStoryItemLimit,
			)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query uncorrelated cloud resources: %v", err))
				return
			}
			if len(cloudCandidates) > 0 {
				ctx["uncorrelated_cloud_resources"] = cloudCandidates
				if cloudCandidatesTruncated {
					ctx["uncorrelated_cloud_resources_truncated"] = true
				}
			}
		}
		k8sResourceResult, err := h.fetchK8sResourceResult(r.Context(), safeStr(ctx, "repo_id"), safeStr(ctx, "name"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query k8s resources: %v", err))
			return
		}
		deploymentSourceGitOps, err := h.fetchDeploymentSourceGitOpsResult(
			r.Context(),
			safeStr(ctx, "name"),
			deploymentSources,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment source gitops evidence: %v", err))
			return
		}
		k8sResourceResult = boundedK8sResourceResult(
			k8sResourceResult.candidates,
			k8sResourceResult.contentLowerBound,
			deploymentSourceGitOps.k8sResources,
			deploymentSourceGitOps.k8sObservedCountIsLowerBound,
		)
		k8sResources := k8sResourceResult.rows
		imageRefs := k8sResourceResult.imageRefs
		imageRegistryTruth, err := h.fetchOCIImageRegistryTruth(r.Context(), imageRefs)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query OCI image registry truth: %v", err))
			return
		}
		ctx["deployment_sources"] = deploymentSources
		ctx["deployment_source_limits"] = deploymentSourceResult.limits
		ctx["cloud_resource_limits"] = cloudResourceResult.limits
		ctx["k8s_resource_limits"] = k8sResourceResult.limits
		if len(cloudResources) > 0 {
			ctx["cloud_resources"] = cloudResources
		}
		ctx["k8s_resources"] = k8sResources
		ctx["image_refs"] = imageRefs
		if len(imageRegistryTruth) > 0 {
			ctx["image_registry_truth"] = imageRegistryTruth
		}
		ctx["controller_entities"] = deploymentSourceGitOps.controllers
		ctx["controller_entity_limits"] = deploymentSourceGitOps.controllerLimits
	}

	WriteSuccess(w, r, http.StatusOK, buildDeploymentTraceResponse(req.ServiceName, ctx), BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", TruthBasisHybrid, "resolved from deployment topology and service evidence"))
}

func fetchServiceTraceContext(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	logger *slog.Logger,
	serviceName string,
	traceOptions traceEnrichmentConfig,
) (map[string]any, error) {
	entityHandler := &EntityHandler{Neo4j: graph, Content: content, Logger: logger}
	workloadID, err := resolveTraceWorkloadSelector(ctx, graph, serviceName)
	if err != nil {
		return nil, err
	}
	var workloadContext map[string]any
	if workloadID != "" {
		workloadContext, err = entityHandler.fetchWorkloadContextForOperation(
			ctx,
			"w.id = $workload_id",
			map[string]any{"workload_id": workloadID},
			"deployment_trace",
		)
	} else {
		workloadContext, err = entityHandler.fetchServiceReadModelWorkloadContext(ctx, serviceName)
	}
	if err != nil || workloadContext == nil {
		return workloadContext, err
	}

	if err := enrichServiceQueryContextWithOptions(ctx, graph, content, workloadContext, serviceQueryEnrichmentOptions{
		DirectOnly:                !traceOptions.includeConsumers,
		IncludeRelatedModuleUsage: traceOptions.includeProvisioningChains,
		MaxDepth:                  traceOptions.maxDepth,
		Logger:                    logger,
		Operation:                 "deployment_trace",
	}); err != nil {
		return nil, fmt.Errorf("enrich service trace context: %w", err)
	}

	return workloadContext, nil
}

func buildDeploymentTraceResponse(serviceName string, workloadContext map[string]any) map[string]any {
	serviceName = canonicalServiceName(serviceName, workloadContext)
	instances, _ := workloadContext["instances"].([]map[string]any)
	topologyEdges := mapSliceValue(workloadContext, "topology_edges")
	provisionedPlatforms := mapSliceValue(workloadContext, "provisioned_platforms")
	deploymentSources, _ := workloadContext["deployment_sources"].([]map[string]any)
	cloudResources, _ := workloadContext["cloud_resources"].([]map[string]any)
	uncorrelatedCloudResources := mapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	k8sResources, _ := workloadContext["k8s_resources"].([]map[string]any)
	imageRefs, _ := workloadContext["image_refs"].([]string)
	imageRegistryTruth := mapSliceValue(workloadContext, "image_registry_truth")
	controllerEntities, _ := workloadContext["controller_entities"].([]map[string]any)
	hostnames := mapSliceValue(workloadContext, "hostnames")
	entrypoints := mapSliceValue(workloadContext, "entrypoints")
	networkPaths := mapSliceValue(workloadContext, "network_paths")
	apiSurface := mapValue(workloadContext, "api_surface")
	dependents := mapSliceValue(workloadContext, "dependents")
	consumerRepositories := mapSliceValue(workloadContext, "consumer_repositories")
	provisioningSourceChains := mapSliceValue(workloadContext, "provisioning_source_chains")
	documentationOverview := mapValue(workloadContext, "documentation_overview")
	supportOverview := mapValue(workloadContext, "support_overview")
	deploymentEvidence := mapValue(workloadContext, "deployment_evidence")
	k8sRelationships := buildK8sRelationships(k8sResources)
	platforms := distinctSortedInstanceField(instances, "platform_name")
	platformKinds := distinctSortedInstanceField(instances, "platform_kind")
	materializedEnvironments := distinctSortedInstanceField(instances, "environment")
	configEnvironments := StringSliceVal(workloadContext, "observed_config_environments")
	mappingMode := deploymentMappingMode(platformKinds, deploymentSources)
	deploymentFacts := buildDeploymentFacts(instances, topologyEdges, provisionedPlatforms, deploymentSources)
	artifactLineage := buildDeploymentTraceArtifactLineage(
		controllerEntities,
		deploymentEvidence,
		k8sResources,
		hostnames,
		apiSurface,
	)
	provenanceOverview := buildDeploymentTraceProvenanceOverview(
		controllerEntities,
		deploymentSources,
		deploymentEvidence,
		artifactLineage,
	)
	story := buildWorkloadStory(workloadContext)
	if provenanceStory := buildDeploymentProvenanceStory(controllerEntities, deploymentSources); provenanceStory != "" {
		story = appendDeploymentTraceStory(story, provenanceStory)
	}
	if workflowStory := buildDeploymentTraceWorkflowProvenanceStory(deploymentEvidence); workflowStory != "" {
		story = appendDeploymentTraceStory(story, workflowStory)
	}
	deploymentOverview := buildServiceDeploymentOverview(workloadContext)
	deliveryPaths := buildNormalizedDeliveryPaths(
		deploymentSources,
		cloudResources,
		k8sResources,
		imageRefs,
		k8sRelationships,
		deploymentEvidence,
	)
	deploymentOverview["deployment_source_count"] = len(deploymentSources)
	deploymentOverview["cloud_resource_count"] = len(cloudResources)
	if len(uncorrelatedCloudResources) > 0 {
		deploymentOverview["uncorrelated_cloud_resource_count"] = len(uncorrelatedCloudResources)
	}
	deploymentOverview["k8s_resource_count"] = len(k8sResources)
	deploymentOverview["image_ref_count"] = len(imageRefs)
	if len(imageRegistryTruth) > 0 {
		deploymentOverview["image_registry_match_count"] = len(imageRegistryTruth)
		deploymentOverview["canonical_image_match_count"] = canonicalOCIImageMatchCount(imageRegistryTruth)
	}
	deploymentOverview["platform_kinds"] = platformKinds
	deploymentOverview["platforms"] = platforms
	deploymentOverview["environments"] = materializedEnvironments
	deploymentOverview["materialized_environments"] = materializedEnvironments
	if len(configEnvironments) > 0 {
		deploymentOverview["config_environments"] = configEnvironments
	}
	if len(provenanceOverview) > 0 {
		deploymentOverview["provenance_families"] = StringSliceVal(provenanceOverview, "families")
	}
	if len(artifactLineage) > 0 {
		deploymentOverview["artifact_lineage_count"] = len(artifactLineage)
	}
	deploymentFactSummary := buildDeploymentFactSummary(
		workloadContext,
		instances,
		materializedEnvironments,
		configEnvironments,
		platforms,
		deploymentSources,
		cloudResources,
		k8sResources,
		imageRefs,
		deploymentFacts,
		mappingMode,
	)

	response := map[string]any{
		"service_name": serviceName,
		"workload_id":  safeStr(workloadContext, "id"),
		"name":         safeStr(workloadContext, "name"),
		"kind":         safeStr(workloadContext, "kind"),
		"repo_id":      safeStr(workloadContext, "repo_id"),
		"repo_name":    safeStr(workloadContext, "repo_name"),
		"subject": map[string]any{
			"type": "service",
			"id":   safeStr(workloadContext, "id"),
			"name": safeStr(workloadContext, "name"),
		},
		"instances":               instances,
		"topology_edges":          topologyEdges,
		"provisioned_platforms":   provisionedPlatforms,
		"deployment_sources":      deploymentSources,
		"cloud_resources":         cloudResources,
		"k8s_resources":           k8sResources,
		"image_refs":              imageRefs,
		"k8s_relationships":       k8sRelationships,
		"deployment_facts":        deploymentFacts,
		"controller_driven_paths": buildControllerDrivenPaths(instances),
		"delivery_paths":          deliveryPaths,
		"story":                   story,
		"story_sections":          buildStorySections(platforms, platformKinds, materializedEnvironments),
		"deployment_overview":     deploymentOverview,
		"controller_overview":     buildControllerOverview(platforms, platformKinds, controllerEntities, deploymentSources, deploymentEvidence, mapValue(workloadContext, "controller_entity_limits")),
		"gitops_overview":         buildGitOpsOverview(platforms, platformKinds, deploymentSources, deploymentEvidence, controllerEntities),
		"runtime_overview":        buildRuntimeOverview(materializedEnvironments),
		"deployment_fact_summary": deploymentFactSummary,
		"drilldowns":              buildDeploymentDrilldowns(serviceName, safeStr(workloadContext, "id")),
	}
	if runtimeTopologyLimits := mapValue(workloadContext, "runtime_topology_limits"); len(runtimeTopologyLimits) > 0 {
		response["runtime_topology_limits"] = runtimeTopologyLimits
	}
	if deploymentSourceLimits := mapValue(workloadContext, "deployment_source_limits"); len(deploymentSourceLimits) > 0 {
		response["deployment_source_limits"] = deploymentSourceLimits
	}
	if cloudResourceLimits := mapValue(workloadContext, "cloud_resource_limits"); len(cloudResourceLimits) > 0 {
		response["cloud_resource_limits"] = cloudResourceLimits
	}
	if k8sResourceLimits := mapValue(workloadContext, "k8s_resource_limits"); len(k8sResourceLimits) > 0 {
		response["k8s_resource_limits"] = k8sResourceLimits
	}
	if len(uncorrelatedCloudResources) > 0 {
		response["uncorrelated_cloud_resources"] = uncorrelatedCloudResources
	}
	if BoolVal(workloadContext, "uncorrelated_cloud_resources_truncated") {
		response["uncorrelated_cloud_resources_truncated"] = true
	}
	if len(imageRegistryTruth) > 0 {
		response["image_registry_truth"] = imageRegistryTruth
	}
	if len(provenanceOverview) > 0 {
		response["provenance_overview"] = provenanceOverview
	}
	if len(artifactLineage) > 0 {
		response["artifact_lineage"] = artifactLineage
	}
	if len(hostnames) > 0 {
		response["hostnames"] = hostnames
	}
	if len(entrypoints) > 0 {
		response["entrypoints"] = entrypoints
	}
	if len(networkPaths) > 0 {
		response["network_paths"] = networkPaths
	}
	if len(apiSurface) > 0 {
		response["api_surface"] = apiSurface
	}
	if len(dependents) > 0 {
		response["dependents"] = dependents
	}
	if len(consumerRepositories) > 0 {
		response["consumer_repositories"] = consumerRepositories
	}
	if len(provisioningSourceChains) > 0 {
		response["provisioning_source_chains"] = provisioningSourceChains
	}
	if len(documentationOverview) > 0 {
		response["documentation_overview"] = documentationOverview
	}
	if len(supportOverview) > 0 {
		response["support_overview"] = supportOverview
	}
	if len(deploymentEvidence) > 0 {
		response["deployment_evidence"] = deploymentEvidence
	}

	return response
}

func buildDeliveryPaths(
	deploymentSources []map[string]any,
	cloudResources []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	k8sRelationships []map[string]any,
) []map[string]any {
	paths := make([]map[string]any, 0, len(deploymentSources)+len(cloudResources)+len(k8sResources)+len(imageRefs)+len(k8sRelationships))
	for _, source := range deploymentSources {
		paths = append(paths, map[string]any{
			"type":       "deployment_source",
			"target":     safeStr(source, "repo_name"),
			"target_id":  safeStr(source, "repo_id"),
			"confidence": floatVal(source, "confidence"),
		})
	}
	for _, resource := range cloudResources {
		paths = append(paths, map[string]any{
			"type":       "cloud_resource",
			"target":     safeStr(resource, "name"),
			"target_id":  safeStr(resource, "id"),
			"confidence": floatVal(resource, "confidence"),
		})
	}
	for _, resource := range k8sResources {
		paths = append(paths, map[string]any{
			"type":      "k8s_resource",
			"target":    safeStr(resource, "entity_name"),
			"target_id": safeStr(resource, "entity_id"),
			"kind":      safeStr(resource, "kind"),
		})
	}
	for _, imageRef := range imageRefs {
		paths = append(paths, map[string]any{
			"type":   "image_ref",
			"target": imageRef,
		})
	}
	for _, relationship := range k8sRelationships {
		paths = append(paths, map[string]any{
			"type":        "k8s_relationship",
			"target":      safeStr(relationship, "target_name"),
			"target_id":   safeStr(relationship, "target_id"),
			"source_name": safeStr(relationship, "source_name"),
			"reason":      safeStr(relationship, "reason"),
			"kind":        safeStr(relationship, "type"),
		})
	}
	return paths
}

func buildDeploymentDrilldowns(serviceName, workloadID string) map[string]any {
	return map[string]any{
		"service_context_path":  "/api/v0/services/" + serviceName + "/context",
		"service_story_path":    "/api/v0/services/" + serviceName + "/story",
		"workload_context_path": "/api/v0/workloads/" + workloadID + "/context",
	}
}
