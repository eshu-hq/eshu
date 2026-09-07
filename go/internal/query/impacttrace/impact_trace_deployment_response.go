// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

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
	dependentsTruncated, consumerRepositoriesTruncated bool
	provisioningSourceChainsTruncated                  bool
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
	f := deploymentTraceFields{serviceName: querycontract.CanonicalServiceName(serviceName, workloadContext)}
	f.instances, _ = workloadContext["instances"].([]map[string]any)
	f.topologyEdges = querycontract.MapSliceValue(workloadContext, "topology_edges")
	f.provisionedPlatforms = querycontract.MapSliceValue(workloadContext, "provisioned_platforms")
	f.deploymentSources, _ = workloadContext["deployment_sources"].([]map[string]any)
	f.cloudResources, _ = workloadContext["cloud_resources"].([]map[string]any)
	f.uncorrelatedCloudResources = querycontract.MapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	f.k8sResources, _ = workloadContext["k8s_resources"].([]map[string]any)
	f.imageRefs, _ = workloadContext["image_refs"].([]string)
	f.imageRegistryTruth = querycontract.MapSliceValue(workloadContext, "image_registry_truth")
	f.controllerEntities, _ = workloadContext["controller_entities"].([]map[string]any)
	f.hostnames = querycontract.MapSliceValue(workloadContext, "hostnames")
	f.entrypoints = querycontract.MapSliceValue(workloadContext, "entrypoints")
	f.networkPaths = querycontract.MapSliceValue(workloadContext, "network_paths")
	f.apiSurface = querycontract.MapValue(workloadContext, "api_surface")
	f.dependents = querycontract.MapSliceValue(workloadContext, "dependents")
	f.consumerRepositories = querycontract.MapSliceValue(workloadContext, "consumer_repositories")
	f.provisioningSourceChains = querycontract.MapSliceValue(workloadContext, "provisioning_source_chains")
	// #5720 round-2 P1-1: threads queryProvisioningRepositoryCandidates's
	// truncated bool (set on workloadContext by service_query_enrichment.go)
	// onto the trace-deployment-chain response, mirroring
	// uncorrelatedCloudResourcesTruncated below. That is exact for
	// dependentsTruncated and provisioningSourceChainsTruncated. PR #5933
	// review fix (Copilot): consumerRepositoriesTruncated is not that same
	// bool -- since round 9 it is the merged consumersTruncated signal
	// loadConsumerRepositoryEnrichmentFromCandidates returns, which folds in
	// the candidate bound plus the evidence-file, hostname, and
	// content-search bounds underneath consumer_repositories.
	f.dependentsTruncated = querycontract.BoolVal(workloadContext, "dependents_truncated")
	f.consumerRepositoriesTruncated = querycontract.BoolVal(workloadContext, "consumer_repositories_truncated")
	f.provisioningSourceChainsTruncated = querycontract.BoolVal(workloadContext, "provisioning_source_chains_truncated")
	f.documentationOverview = querycontract.MapValue(workloadContext, "documentation_overview")
	f.supportOverview = querycontract.MapValue(workloadContext, "support_overview")
	f.deploymentEvidence = querycontract.MapValue(workloadContext, "deployment_evidence")
	f.controllerLimits = querycontract.MapValue(workloadContext, "controller_entity_limits")
	f.runtimeTopologyLimits = querycontract.MapValue(workloadContext, "runtime_topology_limits")
	f.deploymentSourceLimits = querycontract.MapValue(workloadContext, "deployment_source_limits")
	f.cloudResourceLimits = querycontract.MapValue(workloadContext, "cloud_resource_limits")
	f.k8sResourceLimits = querycontract.MapValue(workloadContext, "k8s_resource_limits")
	f.uncorrelatedCloudResourcesTruncated = querycontract.BoolVal(workloadContext, "uncorrelated_cloud_resources_truncated")
	f.k8sRelationships = BuildK8sRelationships(f.k8sResources)
	f.platforms = querycontract.DistinctSortedInstanceField(f.instances, "platform_name")
	f.platformKinds = querycontract.DistinctSortedInstanceField(f.instances, "platform_kind")
	f.materializedEnvironments = querycontract.DistinctSortedInstanceField(f.instances, "environment")
	f.configEnvironments = querycontract.StringSliceVal(workloadContext, "observed_config_environments")
	f.mappingMode = deploymentMappingMode(f.platformKinds, f.deploymentSources)
	f.deploymentFacts = BuildDeploymentFacts(
		f.instances,
		f.topologyEdges,
		f.provisionedPlatforms,
		f.deploymentSources,
	)
	f.artifactLineage = buildDeploymentTraceArtifactLineage(
		f.controllerEntities, f.deploymentEvidence, f.k8sResources, f.hostnames, f.apiSurface,
	)
	f.provenanceOverview = BuildDeploymentTraceProvenanceOverview(
		f.controllerEntities, f.deploymentSources, f.deploymentEvidence, f.artifactLineage,
	)
	f.story = querycontract.BuildWorkloadStory(workloadContext)
	if provenanceStory := buildDeploymentProvenanceStory(f.controllerEntities, f.deploymentSources); provenanceStory != "" {
		f.story = appendDeploymentTraceStory(f.story, provenanceStory)
	}
	if workflowStory := BuildDeploymentTraceWorkflowProvenanceStory(f.deploymentEvidence); workflowStory != "" {
		f.story = appendDeploymentTraceStory(f.story, workflowStory)
	}
	f.deliveryPaths = BuildNormalizedDeliveryPaths(
		f.deploymentSources, f.cloudResources, f.k8sResources, f.imageRefs, f.k8sRelationships, f.deploymentEvidence,
	)
	// NOTE: applyDeploymentOverviewCounts runs in BuildDeploymentTraceResponse,
	// after the caller-built deploymentOverview map is attached; running it
	// here would attach counts to the nil map. See #6060.
	// D2 (#5471): thread the live-evidence probe result (set by the handler
	// on workloadContext["_has_live_evidence"]) through to the fact summary
	// so an exact-match live cluster observation can promote the deployment
	// truth tier from config_only to runtime_confirmed.
	hasLiveEvidence, _ := workloadContext["_has_live_evidence"].(bool)
	f.deploymentFactSummary = BuildDeploymentFactSummary(
		workloadContext, f.instances, f.materializedEnvironments, f.configEnvironments, f.platforms,
		f.deploymentSources, f.cloudResources, f.k8sResources, f.imageRefs, f.deploymentFacts, f.mappingMode,
		hasLiveEvidence,
	)
	return f
}

// applyDeploymentOverviewCounts fills in the count/summary fields of
// deploymentOverview, split out of buildDeploymentTraceFields for funlen.
// The instance/environment/platform/config counts derive here (not only in
// the caller-built map) because they are pure functions of the same workload
// context the production overview builder reads: instance_count,
// environment_count, platform_count, and config_environment_count reproduce
// buildServiceDeploymentOverview's values exactly, so production responses
// are unchanged while unit tests can assert instance summarization without
// replicating the service-story builder. Keys the caller owns outright
// (deployment_truth_tier, hostnames, api_surface, dependents, …) stay
// caller-provided. See #6060.
func (f *deploymentTraceFields) applyDeploymentOverviewCounts() {
	f.deploymentOverview["instance_count"] = len(f.instances)
	f.deploymentOverview["environment_count"] = len(f.materializedEnvironments)
	f.deploymentOverview["materialized_environment_count"] = len(f.materializedEnvironments)
	f.deploymentOverview["config_environment_count"] = len(f.configEnvironments)
	f.deploymentOverview["platform_count"] = len(f.platforms)
	attachDeploymentEvidenceOverview(f.deploymentOverview, f)
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
		f.deploymentOverview["provenance_families"] = querycontract.StringSliceVal(f.provenanceOverview, "families")
	}
	if len(f.artifactLineage) > 0 {
		f.deploymentOverview["artifact_lineage_count"] = len(f.artifactLineage)
	}
}

// attachDeploymentEvidenceOverview surfaces the count fields for the
// service-evidence slices the fields already parsed from the workload
// context (hostnames, entrypoints, network paths, dependents, consumer
// repositories, provisioning chains). Counts derive unconditionally: they
// are len() over the same slices the production overview builder reads, so
// production values are unchanged. Raw evidence lists are never attached
// here: the service-story builder owns the normalized forms it emits
// (hostnames, entrypoints, api_surface), and the remaining lists
// (network_paths, dependents, consumer_repositories,
// provisioning_source_chains) must stay absent unless the caller provided
// them — backfilling would add keys the base production response never
// carried. deployment_truth_tier stays fully caller-owned: truth
// classification cannot cross into this package. See #6060.
func attachDeploymentEvidenceOverview(overview map[string]any, f *deploymentTraceFields) {
	// Conditional like the production builder: absent evidence leaves no
	// zero-count keys behind, so production responses are unchanged.
	if len(f.hostnames) > 0 {
		overview["hostname_count"] = len(f.hostnames)
	}
	if len(f.entrypoints) > 0 {
		overview["entrypoint_count"] = len(f.entrypoints)
	}
	if len(f.networkPaths) > 0 {
		overview["network_path_count"] = len(f.networkPaths)
	}
	if len(f.dependents) > 0 {
		overview["dependent_count"] = len(f.dependents)
	}
	if len(f.consumerRepositories) > 0 {
		overview["consumer_repository_count"] = len(f.consumerRepositories)
	}
	if len(f.provisioningSourceChains) > 0 {
		overview["provisioning_source_chain_count"] = len(f.provisioningSourceChains)
	}
}

// deploymentOverview is caller-built (the production handler reaches it
// through ImpactHandler.TraceContext; tests pass canned maps) because the
// service-story overview shaping cannot cross into this package. The map must
// be non-nil: counts are attached in place. See #6060.
// BuildDeploymentTraceResponse shapes the trace_deployment_chain response.
// Exported because the impact package's trace handler and adapter tests call
// it from outside this package. See #6060.
func BuildDeploymentTraceResponse(serviceName string, workloadContext map[string]any, deploymentOverview map[string]any) map[string]any {
	f := buildDeploymentTraceFields(serviceName, workloadContext)
	f.deploymentOverview = deploymentOverview
	f.applyDeploymentOverviewCounts()

	response := map[string]any{
		"service_name": f.serviceName,
		"workload_id":  querycontract.SafeStr(workloadContext, "id"),
		"name":         querycontract.SafeStr(workloadContext, "name"),
		"kind":         querycontract.SafeStr(workloadContext, "kind"),
		"repo_id":      querycontract.SafeStr(workloadContext, "repo_id"),
		"repo_name":    querycontract.SafeStr(workloadContext, "repo_name"),
		"subject": map[string]any{
			"type": "service",
			"id":   querycontract.SafeStr(workloadContext, "id"),
			"name": querycontract.SafeStr(workloadContext, "name"),
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
		"drilldowns":              buildDeploymentDrilldowns(f.serviceName, querycontract.SafeStr(workloadContext, "id")),
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
	if f.dependentsTruncated {
		response["dependents_truncated"] = true
	}
	if len(f.consumerRepositories) > 0 {
		response["consumer_repositories"] = f.consumerRepositories
	}
	if f.consumerRepositoriesTruncated {
		response["consumer_repositories_truncated"] = true
	}
	if len(f.provisioningSourceChains) > 0 {
		response["provisioning_source_chains"] = f.provisioningSourceChains
	}
	if f.provisioningSourceChainsTruncated {
		response["provisioning_source_chains_truncated"] = true
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
			"target":     querycontract.SafeStr(source, "repo_name"),
			"target_id":  querycontract.SafeStr(source, "repo_id"),
			"confidence": querycontract.FloatVal(source, "confidence"),
		})
	}
	for _, resource := range cloudResources {
		paths = append(paths, map[string]any{
			"type":       "cloud_resource",
			"target":     querycontract.SafeStr(resource, "name"),
			"target_id":  querycontract.SafeStr(resource, "id"),
			"confidence": querycontract.FloatVal(resource, "confidence"),
		})
	}
	for _, resource := range k8sResources {
		paths = append(paths, map[string]any{
			"type":      "k8s_resource",
			"target":    querycontract.SafeStr(resource, "entity_name"),
			"target_id": querycontract.SafeStr(resource, "entity_id"),
			"kind":      querycontract.SafeStr(resource, "kind"),
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
			"target":      querycontract.SafeStr(relationship, "target_name"),
			"target_id":   querycontract.SafeStr(relationship, "target_id"),
			"source_name": querycontract.SafeStr(relationship, "source_name"),
			"reason":      querycontract.SafeStr(relationship, "reason"),
			"kind":        querycontract.SafeStr(relationship, "type"),
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
