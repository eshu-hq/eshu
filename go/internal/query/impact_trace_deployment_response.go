// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// impact_trace_deployment_response.go builds the trace_deployment_chain response
// shape from an already-filtered workload context. The handler and its query
// helpers apply repository authorization before every bounded selection; this
// file only shapes their admitted rows. Split out to keep both files under the
// repo's file-length cap.

// deploymentTraceFields holds every value buildDeploymentTraceResponse derives
// from the workload context before shaping the response map. Split out of
// buildDeploymentTraceResponse (funlen) so each function stays focused: this
// struct computation, the base response shape, and the optional-field
// attachment.
type deploymentTraceFields struct {
	serviceName                                        string
	instances, deploymentSources, cloudResources       []map[string]any
	topologyEdges, provisionedPlatforms                []map[string]any
	uncorrelatedCloudResources, k8sResources           []map[string]any
	imageRefs                                          []string
	imageRegistryTruth, controllerEntities             []map[string]any
	hostnames, entrypoints, networkPaths               []map[string]any
	apiSurface                                         map[string]any
	dependents, consumerRepositories                   []map[string]any
	provisioningSourceChains                           []map[string]any
	documentationOverview, supportOverview             map[string]any
	deploymentEvidence                                 map[string]any
	controllerLimits, runtimeTopologyLimits            map[string]any
	deploymentSourceLimits, cloudResourceLimits        map[string]any
	k8sResourceLimits                                  map[string]any
	uncorrelatedCloudResourcesTruncated                bool
	k8sRelationships                                   []map[string]any
	platforms, platformKinds, materializedEnvironments []string
	configEnvironments                                 []string
	mappingMode                                        string
	deploymentFacts                                    []map[string]any
	artifactLineage                                    []map[string]any
	provenanceOverview                                 map[string]any
	story                                              string
	deploymentOverview                                 map[string]any
	deliveryPaths                                      []map[string]any
	deploymentFactSummary                              map[string]any
}

func buildDeploymentTraceFields(serviceName string, workloadContext map[string]any) deploymentTraceFields {
	f := deploymentTraceFields{serviceName: canonicalServiceName(serviceName, workloadContext)}
	f.instances, _ = workloadContext["instances"].([]map[string]any)
	f.topologyEdges = mapSliceValue(workloadContext, "topology_edges")
	f.provisionedPlatforms = mapSliceValue(workloadContext, "provisioned_platforms")
	f.deploymentSources, _ = workloadContext["deployment_sources"].([]map[string]any)
	f.cloudResources, _ = workloadContext["cloud_resources"].([]map[string]any)
	f.uncorrelatedCloudResources = mapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	f.k8sResources, _ = workloadContext["k8s_resources"].([]map[string]any)
	f.imageRefs, _ = workloadContext["image_refs"].([]string)
	f.imageRegistryTruth = mapSliceValue(workloadContext, "image_registry_truth")
	f.controllerEntities, _ = workloadContext["controller_entities"].([]map[string]any)
	f.hostnames = mapSliceValue(workloadContext, "hostnames")
	f.entrypoints = mapSliceValue(workloadContext, "entrypoints")
	f.networkPaths = mapSliceValue(workloadContext, "network_paths")
	f.apiSurface = mapValue(workloadContext, "api_surface")
	f.dependents = mapSliceValue(workloadContext, "dependents")
	f.consumerRepositories = mapSliceValue(workloadContext, "consumer_repositories")
	f.provisioningSourceChains = mapSliceValue(workloadContext, "provisioning_source_chains")
	f.documentationOverview = mapValue(workloadContext, "documentation_overview")
	f.supportOverview = mapValue(workloadContext, "support_overview")
	f.deploymentEvidence = mapValue(workloadContext, "deployment_evidence")
	f.controllerLimits = mapValue(workloadContext, "controller_entity_limits")
	f.runtimeTopologyLimits = mapValue(workloadContext, "runtime_topology_limits")
	f.deploymentSourceLimits = mapValue(workloadContext, "deployment_source_limits")
	f.cloudResourceLimits = mapValue(workloadContext, "cloud_resource_limits")
	f.k8sResourceLimits = mapValue(workloadContext, "k8s_resource_limits")
	f.uncorrelatedCloudResourcesTruncated = BoolVal(workloadContext, "uncorrelated_cloud_resources_truncated")
	f.k8sRelationships = buildK8sRelationships(f.k8sResources)
	f.platforms = distinctSortedInstanceField(f.instances, "platform_name")
	f.platformKinds = distinctSortedInstanceField(f.instances, "platform_kind")
	f.materializedEnvironments = distinctSortedInstanceField(f.instances, "environment")
	f.configEnvironments = StringSliceVal(workloadContext, "observed_config_environments")
	f.mappingMode = deploymentMappingMode(f.platformKinds, f.deploymentSources)
	f.deploymentFacts = buildDeploymentFacts(
		f.instances,
		f.topologyEdges,
		f.provisionedPlatforms,
		f.deploymentSources,
	)
	f.artifactLineage = buildDeploymentTraceArtifactLineage(
		f.controllerEntities, f.deploymentEvidence, f.k8sResources, f.hostnames, f.apiSurface,
	)
	f.provenanceOverview = buildDeploymentTraceProvenanceOverview(
		f.controllerEntities, f.deploymentSources, f.deploymentEvidence, f.artifactLineage,
	)
	f.story = buildWorkloadStory(workloadContext)
	if provenanceStory := buildDeploymentProvenanceStory(f.controllerEntities, f.deploymentSources); provenanceStory != "" {
		f.story = appendDeploymentTraceStory(f.story, provenanceStory)
	}
	if workflowStory := buildDeploymentTraceWorkflowProvenanceStory(f.deploymentEvidence); workflowStory != "" {
		f.story = appendDeploymentTraceStory(f.story, workflowStory)
	}
	f.deploymentOverview = buildServiceDeploymentOverview(workloadContext)
	f.deliveryPaths = buildNormalizedDeliveryPaths(
		f.deploymentSources, f.cloudResources, f.k8sResources, f.imageRefs, f.k8sRelationships, f.deploymentEvidence,
	)
	f.applyDeploymentOverviewCounts()
	// D2 (#5471): thread the live-evidence probe result (set by the handler
	// on workloadContext["_has_live_evidence"]) through to the fact summary
	// so an exact-match live cluster observation can promote the deployment
	// truth tier from config_only to runtime_confirmed.
	hasLiveEvidence, _ := workloadContext["_has_live_evidence"].(bool)
	f.deploymentFactSummary = buildDeploymentFactSummary(
		workloadContext, f.instances, f.materializedEnvironments, f.configEnvironments, f.platforms,
		f.deploymentSources, f.cloudResources, f.k8sResources, f.imageRefs, f.deploymentFacts, f.mappingMode,
		hasLiveEvidence,
	)
	return f
}

// applyDeploymentOverviewCounts fills in the count/summary fields of
// deploymentOverview, split out of buildDeploymentTraceFields for funlen.
func (f *deploymentTraceFields) applyDeploymentOverviewCounts() {
	f.deploymentOverview["deployment_source_count"] = len(f.deploymentSources)
	f.deploymentOverview["cloud_resource_count"] = len(f.cloudResources)
	if len(f.uncorrelatedCloudResources) > 0 {
		f.deploymentOverview["uncorrelated_cloud_resource_count"] = len(f.uncorrelatedCloudResources)
	}
	f.deploymentOverview["k8s_resource_count"] = len(f.k8sResources)
	f.deploymentOverview["image_ref_count"] = len(f.imageRefs)
	if len(f.imageRegistryTruth) > 0 {
		f.deploymentOverview["image_registry_match_count"] = len(f.imageRegistryTruth)
		f.deploymentOverview["canonical_image_match_count"] = canonicalOCIImageMatchCount(f.imageRegistryTruth)
	}
	f.deploymentOverview["platform_kinds"] = f.platformKinds
	f.deploymentOverview["platforms"] = f.platforms
	f.deploymentOverview["environments"] = f.materializedEnvironments
	f.deploymentOverview["materialized_environments"] = f.materializedEnvironments
	if len(f.configEnvironments) > 0 {
		f.deploymentOverview["config_environments"] = f.configEnvironments
	}
	if len(f.provenanceOverview) > 0 {
		f.deploymentOverview["provenance_families"] = StringSliceVal(f.provenanceOverview, "families")
	}
	if len(f.artifactLineage) > 0 {
		f.deploymentOverview["artifact_lineage_count"] = len(f.artifactLineage)
	}
}

func buildDeploymentTraceResponse(serviceName string, workloadContext map[string]any) map[string]any {
	f := buildDeploymentTraceFields(serviceName, workloadContext)

	response := map[string]any{
		"service_name": f.serviceName,
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
		"instances":               f.instances,
		"topology_edges":          f.topologyEdges,
		"provisioned_platforms":   f.provisionedPlatforms,
		"deployment_sources":      f.deploymentSources,
		"cloud_resources":         f.cloudResources,
		"k8s_resources":           f.k8sResources,
		"image_refs":              f.imageRefs,
		"k8s_relationships":       f.k8sRelationships,
		"deployment_facts":        f.deploymentFacts,
		"controller_driven_paths": buildControllerDrivenPaths(f.instances),
		"delivery_paths":          f.deliveryPaths,
		"story":                   f.story,
		"story_sections":          buildStorySections(f.platforms, f.platformKinds, f.materializedEnvironments),
		"deployment_overview":     f.deploymentOverview,
		"controller_overview": buildControllerOverview(
			f.platforms,
			f.platformKinds,
			f.controllerEntities,
			f.deploymentSources,
			f.deploymentEvidence,
			f.controllerLimits,
		),
		"gitops_overview":         buildGitOpsOverview(f.platforms, f.platformKinds, f.deploymentSources, f.deploymentEvidence, f.controllerEntities),
		"runtime_overview":        buildRuntimeOverview(f.materializedEnvironments),
		"deployment_fact_summary": f.deploymentFactSummary,
		"drilldowns":              buildDeploymentDrilldowns(f.serviceName, safeStr(workloadContext, "id")),
	}
	f.attachOptionalFields(response)
	return response
}

// attachOptionalFields adds the response fields that are only present when
// non-empty, split out of buildDeploymentTraceResponse for funlen.
func (f *deploymentTraceFields) attachOptionalFields(response map[string]any) {
	if len(f.runtimeTopologyLimits) > 0 {
		response["runtime_topology_limits"] = f.runtimeTopologyLimits
	}
	if len(f.deploymentSourceLimits) > 0 {
		response["deployment_source_limits"] = f.deploymentSourceLimits
	}
	if len(f.cloudResourceLimits) > 0 {
		response["cloud_resource_limits"] = f.cloudResourceLimits
	}
	if len(f.k8sResourceLimits) > 0 {
		response["k8s_resource_limits"] = f.k8sResourceLimits
	}
	if len(f.uncorrelatedCloudResources) > 0 {
		response["uncorrelated_cloud_resources"] = f.uncorrelatedCloudResources
	}
	if f.uncorrelatedCloudResourcesTruncated {
		response["uncorrelated_cloud_resources_truncated"] = true
	}
	if len(f.imageRegistryTruth) > 0 {
		response["image_registry_truth"] = f.imageRegistryTruth
	}
	if len(f.provenanceOverview) > 0 {
		response["provenance_overview"] = f.provenanceOverview
	}
	if len(f.artifactLineage) > 0 {
		response["artifact_lineage"] = f.artifactLineage
	}
	if len(f.hostnames) > 0 {
		response["hostnames"] = f.hostnames
	}
	if len(f.entrypoints) > 0 {
		response["entrypoints"] = f.entrypoints
	}
	if len(f.networkPaths) > 0 {
		response["network_paths"] = f.networkPaths
	}
	if len(f.apiSurface) > 0 {
		response["api_surface"] = f.apiSurface
	}
	if len(f.dependents) > 0 {
		response["dependents"] = f.dependents
	}
	if len(f.consumerRepositories) > 0 {
		response["consumer_repositories"] = f.consumerRepositories
	}
	if len(f.provisioningSourceChains) > 0 {
		response["provisioning_source_chains"] = f.provisioningSourceChains
	}
	if len(f.documentationOverview) > 0 {
		response["documentation_overview"] = f.documentationOverview
	}
	if len(f.supportOverview) > 0 {
		response["support_overview"] = f.supportOverview
	}
	if len(f.deploymentEvidence) > 0 {
		response["deployment_evidence"] = f.deploymentEvidence
	}
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
