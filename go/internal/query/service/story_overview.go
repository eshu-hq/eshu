// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

func BuildServiceStoryResponse(serviceName string, workloadContext map[string]any) map[string]any {
	buildCtx := newServiceStoryBuildContext(workloadContext)
	cacheServiceStoryBuildContext(buildCtx)
	serviceName = canonicalServiceName(serviceName, workloadContext)
	response := make(map[string]any, 24)
	response["service_name"] = serviceName
	response["story"] = querycontract.BuildWorkloadStoryWithAPISurface(workloadContext, buildCtx.apiSurface, buildCtx.hasAPISurface)
	response["story_sections"] = buildServiceStorySectionsWithContext(buildCtx)
	response["deployment_overview"] = buildServiceDeploymentOverviewWithContext(buildCtx)
	response["code_to_runtime_trace"] = buildServiceCodeToRuntimeTrace(workloadContext)
	for _, key := range []string{"documentation_overview", "support_overview"} {
		if value, ok := workloadContext[key]; ok && value != nil {
			response[key] = value
		}
	}
	if ciCDEvidence := querycontract.MapValue(workloadContext, "ci_cd_evidence"); len(ciCDEvidence) > 0 {
		response["ci_cd_evidence"] = ciCDEvidence
	}
	enrichServiceStoryDossierResponseWithContext(response, buildCtx)
	response["investigation"] = buildServiceInvestigationPacketWithContext(serviceName, buildCtx, InvestigationOptions{})
	querycontract.AttachEvidenceBoundaries(response, "get_service_story")
	return querycontract.AttachAnswerMetadata(response)
}

type serviceStoryBuildContext struct {
	workloadContext map[string]any
	apiSurface      map[string]any
	hasAPISurface   bool
}

type serviceStoryAPISurfaceCache struct {
	apiSurface    map[string]any
	hasAPISurface bool
}

const serviceStoryAPISurfaceCacheKey = "__service_story_normalized_api_surface"

func newServiceStoryBuildContext(workloadContext map[string]any) serviceStoryBuildContext {
	if cached, ok := workloadContext[serviceStoryAPISurfaceCacheKey].(serviceStoryAPISurfaceCache); ok {
		return serviceStoryBuildContext{
			workloadContext: workloadContext,
			apiSurface:      cached.apiSurface,
			hasAPISurface:   cached.hasAPISurface,
		}
	}
	apiSurface, ok := normalizedServiceAPISurface(workloadContext)
	return serviceStoryBuildContext{
		workloadContext: workloadContext,
		apiSurface:      apiSurface,
		hasAPISurface:   ok,
	}
}

func cacheServiceStoryBuildContext(buildCtx serviceStoryBuildContext) {
	if buildCtx.workloadContext == nil {
		return
	}
	if _, ok := buildCtx.workloadContext[serviceStoryAPISurfaceCacheKey]; ok {
		return
	}
	buildCtx.workloadContext[serviceStoryAPISurfaceCacheKey] = serviceStoryAPISurfaceCache{
		apiSurface:    buildCtx.apiSurface,
		hasAPISurface: buildCtx.hasAPISurface,
	}
}

func (c serviceStoryBuildContext) dossierAPISurface() map[string]any {
	if !c.hasAPISurface {
		return emptyServiceDossierAPISurface()
	}
	return c.apiSurface
}

// canonicalServiceName returns the workload context's canonical name. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func canonicalServiceName(requestedServiceName string, workloadContext map[string]any) string {
	return querycontract.CanonicalServiceName(requestedServiceName, workloadContext)
}

// distinctSortedInstanceField returns the sorted distinct values of key
// across instances. The implementation moved to querycontract for #6060;
// this wrapper keeps root callers unchanged.
func distinctSortedInstanceField(instances []map[string]any, key string) []string {
	return querycontract.DistinctSortedInstanceField(instances, key)
}

func BuildServiceDeploymentOverview(workloadContext map[string]any) map[string]any {
	return buildServiceDeploymentOverviewWithContext(newServiceStoryBuildContext(workloadContext))
}

func buildServiceDeploymentOverviewWithContext(buildCtx serviceStoryBuildContext) map[string]any {
	workloadContext := buildCtx.workloadContext
	instances, _ := workloadContext["instances"].([]map[string]any)
	platforms := distinctSortedInstanceField(instances, "platform_name")
	materializedEnvironments := distinctSortedInstanceField(instances, "environment")
	configEnvironments := querycontract.StringSliceVal(workloadContext, "observed_config_environments")

	// Known disclosure gap (#5582, follow-up to #5471 review round 2 P2):
	// hasLiveEvidence and hasDeploymentSources are hardcoded false here, so
	// a workload that trace_deployment_chain reports as runtime_confirmed
	// (via fetchWorkloadLiveEvidence) can report only config_only or no
	// tier at all from the service story surface for the SAME workload.
	// deployment-truth-tiers.md documents all three surfaces as applying
	// the vocabulary "consistently" through this one classifier; that is
	// true for the classifier call itself but not yet for the inputs each
	// surface feeds it. #5582 tracks wiring the story build context's
	// live-evidence and deployment-source signals so this asymmetry closes.
	// Mirrors the supply-chain-impact disclosure pattern (#5472/#5474).
	tier := truth.ClassifyDeploymentTruthTier(
		false, // hasLiveEvidence: story context has no live probe today (#5582)
		len(instances) > 0,
		false, // deployment sources not surfaced in story overview (#5582)
		len(configEnvironments) > 0,
	)
	overview := map[string]any{
		"instance_count":                 len(instances),
		"environment_count":              len(materializedEnvironments),
		"materialized_environment_count": len(materializedEnvironments),
		"config_environment_count":       len(configEnvironments),
		"platform_count":                 len(platforms),
		"platforms":                      platforms,
		"environments":                   materializedEnvironments,
		"materialized_environments":      materializedEnvironments,
	}
	if tier != "" {
		overview["deployment_truth_tier"] = string(tier)
	}
	if len(configEnvironments) > 0 {
		overview["config_environments"] = configEnvironments
	}
	if hostnames := querycontract.MapSliceValue(workloadContext, "hostnames"); len(hostnames) > 0 {
		overview["hostname_count"] = len(hostnames)
		overview["hostnames"] = hostnames
	}
	if entrypoints := querycontract.MapSliceValue(workloadContext, "entrypoints"); len(entrypoints) > 0 {
		overview["entrypoint_count"] = len(entrypoints)
		overview["entrypoints"] = entrypoints
	}
	if networkPaths := querycontract.MapSliceValue(workloadContext, "network_paths"); len(networkPaths) > 0 {
		overview["network_path_count"] = len(networkPaths)
	}
	if buildCtx.hasAPISurface {
		overview["api_surface"] = buildCtx.apiSurface
	}
	if dependents := querycontract.MapSliceValue(workloadContext, "dependents"); len(dependents) > 0 {
		overview["dependent_count"] = len(dependents)
	}
	if consumers := querycontract.MapSliceValue(workloadContext, "consumer_repositories"); len(consumers) > 0 {
		overview["consumer_repository_count"] = len(consumers)
	}
	if provisioningChains := querycontract.MapSliceValue(workloadContext, "provisioning_source_chains"); len(provisioningChains) > 0 {
		overview["provisioning_source_chain_count"] = len(provisioningChains)
	}
	if cloudResources := querycontract.MapSliceValue(workloadContext, "cloud_resources"); len(cloudResources) > 0 {
		overview["cloud_resource_count"] = len(cloudResources)
		overview["cloud_relationship_basis"] = serviceCloudRelationshipBasis(cloudResources)
	}
	if cloudCandidates := querycontract.MapSliceValue(workloadContext, "uncorrelated_cloud_resources"); len(cloudCandidates) > 0 {
		overview["uncorrelated_cloud_resource_count"] = len(cloudCandidates)
		overview["missing_cloud_relationship"] = "workload_cloud_relationship"
	}
	if deploymentEvidence := querycontract.MapValue(workloadContext, "deployment_evidence"); len(deploymentEvidence) > 0 {
		if toolFamilies := serviceDeploymentToolFamilies(deploymentEvidence); len(toolFamilies) > 0 {
			overview["deployment_tool_families"] = toolFamilies
		}
		if artifactCount := querycontract.IntVal(deploymentEvidence, "artifact_count"); artifactCount > 0 {
			overview["deployment_evidence_artifact_count"] = artifactCount
		}
		if deliveryPaths := querycontract.MapSliceValue(deploymentEvidence, "delivery_paths"); len(deliveryPaths) > 0 {
			overview["delivery_path_count"] = len(deliveryPaths)
		}
		if deliveryWorkflows := querycontract.MapSliceValue(deploymentEvidence, "delivery_workflows"); len(deliveryWorkflows) > 0 {
			overview["delivery_workflow_count"] = len(deliveryWorkflows)
		}
		if sharedConfigPaths := querycontract.MapSliceValue(deploymentEvidence, "shared_config_paths"); len(sharedConfigPaths) > 0 {
			overview["shared_config_path_count"] = len(sharedConfigPaths)
		}
	}
	return overview
}

func serviceCloudRelationshipBasis(resources []map[string]any) string {
	if len(resources) == 0 {
		return ""
	}
	bases := map[string]struct{}{}
	for _, resource := range resources {
		basis := querycontract.StringVal(resource, "relationship_basis")
		if basis == "" {
			basis = "materialized_workload_cloud_relationship"
		}
		bases[basis] = struct{}{}
	}
	if len(bases) != 1 {
		return "mixed"
	}
	for basis := range bases {
		return basis
	}
	return ""
}

func buildServiceStorySectionsWithContext(buildCtx serviceStoryBuildContext) []map[string]any {
	workloadContext := buildCtx.workloadContext
	overview := buildServiceDeploymentOverviewWithContext(buildCtx)
	sections := []map[string]any{
		{
			"title": "deployment",
			"summary": fmt.Sprintf(
				"%d instance(s), %d environment signal(s), %d platform target(s)",
				querycontract.IntVal(overview, "instance_count"),
				querycontract.IntVal(overview, "environment_count"),
				querycontract.IntVal(overview, "platform_count"),
			),
		},
	}

	if hostnames := querycontract.MapSliceValue(workloadContext, "hostnames"); len(hostnames) > 0 {
		sections = append(sections, map[string]any{
			"title":   "entrypoints",
			"summary": fmt.Sprintf("%d observed hostname entrypoint(s)", len(hostnames)),
		})
	}
	if networkPaths := querycontract.MapSliceValue(workloadContext, "network_paths"); len(networkPaths) > 0 {
		sections = append(sections, map[string]any{
			"title":   "network",
			"summary": fmt.Sprintf("%d evidence-backed network path(s) connect entrypoints to runtime targets", len(networkPaths)),
		})
	}
	if buildCtx.hasAPISurface {
		sections = append(sections, map[string]any{
			"title": "api",
			"summary": fmt.Sprintf(
				"%d endpoint(s), %d method(s), %d spec file(s)",
				querycontract.IntVal(buildCtx.apiSurface, "endpoint_count"),
				querycontract.IntVal(buildCtx.apiSurface, "method_count"),
				querycontract.IntVal(buildCtx.apiSurface, "spec_count"),
			),
		})
	}
	if consumers := querycontract.MapSliceValue(workloadContext, "consumer_repositories"); len(consumers) > 0 {
		sections = append(sections, map[string]any{
			"title":   "consumers",
			"summary": fmt.Sprintf("%d consumer repo(s) observed from graph and content evidence", len(consumers)),
		})
	}
	if dependents := querycontract.MapSliceValue(workloadContext, "dependents"); len(dependents) > 0 {
		sections = append(sections, map[string]any{
			"title":   "dependents",
			"summary": fmt.Sprintf("%d graph-derived dependent repo(s) observed from typed relationships", len(dependents)),
		})
	}
	if provisioningChains := querycontract.MapSliceValue(workloadContext, "provisioning_source_chains"); len(provisioningChains) > 0 {
		sections = append(sections, map[string]any{
			"title":   "provisioning",
			"summary": fmt.Sprintf("%d provisioning source chain(s) observed", len(provisioningChains)),
		})
	}
	if deploymentEvidence := querycontract.MapValue(workloadContext, "deployment_evidence"); len(deploymentEvidence) > 0 {
		toolFamilies := serviceDeploymentToolFamilies(deploymentEvidence)
		deliveryPathCount := len(querycontract.MapSliceValue(deploymentEvidence, "delivery_paths"))
		if deliveryPathCount == 0 {
			deliveryPathCount = querycontract.IntVal(deploymentEvidence, "artifact_count")
		}
		sections = append(sections, map[string]any{
			"title": "delivery",
			"summary": fmt.Sprintf(
				"%d delivery evidence item(s) across tool families %s",
				deliveryPathCount,
				impact.JoinOrNone(toolFamilies),
			),
		})
	}
	if ciCDEvidence := querycontract.MapValue(workloadContext, "ci_cd_evidence"); len(ciCDEvidence) > 0 {
		sections = append(sections, map[string]any{
			"title":   "ci_cd",
			"summary": querycontract.CicdEvidenceStorySummary(ciCDEvidence),
		})
	}
	return sections
}

func serviceDeploymentToolFamilies(deploymentEvidence map[string]any) []string {
	return querycontract.ServiceDeploymentToolFamilies(deploymentEvidence)
}

func buildServiceDocumentationOverview(
	ctx context.Context,
	graph querycontract.GraphQuery,
	workloadContext map[string]any,
	evidence QueryEvidence,
) map[string]any {
	repoID := querycontract.SafeStr(workloadContext, "repo_id")
	repoName := querycontract.SafeStr(workloadContext, "repo_name")
	if repoID == "" && repoName == "" {
		return nil
	}

	overview := map[string]any{
		"repo_id":               repoID,
		"repo_name":             repoName,
		"portable_identifier":   repoID,
		"docs_route_count":      len(evidence.DocsRoutes),
		"api_spec_count":        len(evidence.APISpecs),
		"entrypoint_host_count": len(buildServiceHostnameRows(evidence.Hostnames)),
	}

	if graph != nil && repoID != "" {
		row, err := graph.RunSingle(ctx, fmt.Sprintf(
			`MATCH (r:Repository {id: $repo_id}) RETURN %s`,
			querycontract.RepoProjection("r"),
		), map[string]any{"repo_id": repoID})
		if err == nil && row != nil {
			repo := querycontract.RepoRefFromRow(row)
			overview["remote_url"] = repo.RemoteURL
			overview["repo_slug"] = repo.RepoSlug
			overview["has_remote"] = repo.HasRemote
			overview["local_path_present"] = repo.LocalPath != ""
		}
	}

	specPaths := make([]string, 0, len(evidence.APISpecs))
	for _, spec := range evidence.APISpecs {
		specPaths = append(specPaths, spec.RelativePath)
	}
	sort.Strings(specPaths)
	if len(specPaths) > 0 {
		overview["api_spec_paths"] = specPaths
	}
	return overview
}

func buildServiceSupportOverview(workloadContext map[string]any) map[string]any {
	return buildServiceSupportOverviewWithContext(newServiceStoryBuildContext(workloadContext))
}

func buildServiceSupportOverviewWithContext(buildCtx serviceStoryBuildContext) map[string]any {
	workloadContext := buildCtx.workloadContext
	overview := map[string]any{
		"dependency_count":           len(querycontract.MapSliceValue(workloadContext, "dependencies")),
		"dependent_count":            len(querycontract.MapSliceValue(workloadContext, "dependents")),
		"consumer_repository_count":  len(querycontract.MapSliceValue(workloadContext, "consumer_repositories")),
		"provisioning_source_count":  len(querycontract.MapSliceValue(workloadContext, "provisioning_source_chains")),
		"observed_environment_count": len(querycontract.StringSliceVal(workloadContext, "observed_config_environments")),
		"entrypoint_host_count":      len(querycontract.MapSliceValue(workloadContext, "hostnames")),
		"entrypoint_count":           len(querycontract.MapSliceValue(workloadContext, "entrypoints")),
		"network_path_count":         len(querycontract.MapSliceValue(workloadContext, "network_paths")),
		"cloud_resource_count":       len(querycontract.MapSliceValue(workloadContext, "cloud_resources")),
		"has_api_surface":            len(querycontract.MapValue(workloadContext, "api_surface")) > 0,
		"has_documentation_overview": len(querycontract.MapValue(workloadContext, "documentation_overview")) > 0,
	}
	if buildCtx.hasAPISurface {
		overview["endpoint_count"] = querycontract.IntVal(buildCtx.apiSurface, "endpoint_count")
		overview["method_count"] = querycontract.IntVal(buildCtx.apiSurface, "method_count")
		overview["spec_count"] = querycontract.IntVal(buildCtx.apiSurface, "spec_count")
	}
	if deploymentEvidence := querycontract.MapValue(workloadContext, "deployment_evidence"); len(deploymentEvidence) > 0 {
		overview["deployment_tool_family_count"] = len(querycontract.StringSliceValue(deploymentEvidence, "tool_families"))
		overview["delivery_path_count"] = len(querycontract.MapSliceValue(deploymentEvidence, "delivery_paths"))
		overview["delivery_workflow_count"] = len(querycontract.MapSliceValue(deploymentEvidence, "delivery_workflows"))
	}
	if targetSupport := querycontract.MapValue(workloadContext, "target_support"); len(targetSupport) > 0 {
		overview["target_support"] = targetSupport
	}
	return overview
}
