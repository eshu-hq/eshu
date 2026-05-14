package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func buildServiceStoryResponse(serviceName string, workloadContext map[string]any) map[string]any {
	serviceName = canonicalServiceName(serviceName, workloadContext)
	response := map[string]any{
		"service_name":        serviceName,
		"story":               buildWorkloadStory(workloadContext),
		"story_sections":      buildServiceStorySections(workloadContext),
		"deployment_overview": buildServiceDeploymentOverview(workloadContext),
	}
	for _, key := range []string{"documentation_overview", "support_overview"} {
		if value, ok := workloadContext[key]; ok && value != nil {
			response[key] = value
		}
	}
	enrichServiceStoryDossierResponse(response, workloadContext)
	response["investigation"] = buildServiceInvestigationPacket(serviceName, workloadContext, serviceInvestigationOptions{})
	return response
}

func canonicalServiceName(requestedServiceName string, workloadContext map[string]any) string {
	if canonicalName := safeStr(workloadContext, "name"); canonicalName != "" {
		return canonicalName
	}
	return strings.TrimSpace(requestedServiceName)
}

func buildServiceDeploymentOverview(workloadContext map[string]any) map[string]any {
	instances, _ := workloadContext["instances"].([]map[string]any)
	platforms := distinctSortedInstanceField(instances, "platform_name")
	materializedEnvironments := distinctSortedInstanceField(instances, "environment")
	configEnvironments := StringSliceVal(workloadContext, "observed_config_environments")

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
	if len(configEnvironments) > 0 {
		overview["config_environments"] = configEnvironments
	}
	if hostnames := mapSliceValue(workloadContext, "hostnames"); len(hostnames) > 0 {
		overview["hostname_count"] = len(hostnames)
		overview["hostnames"] = hostnames
	}
	if entrypoints := mapSliceValue(workloadContext, "entrypoints"); len(entrypoints) > 0 {
		overview["entrypoint_count"] = len(entrypoints)
		overview["entrypoints"] = entrypoints
	}
	if networkPaths := mapSliceValue(workloadContext, "network_paths"); len(networkPaths) > 0 {
		overview["network_path_count"] = len(networkPaths)
	}
	if apiSurface := mapValue(workloadContext, "api_surface"); len(apiSurface) > 0 {
		overview["api_surface"] = apiSurface
	}
	if dependents := mapSliceValue(workloadContext, "dependents"); len(dependents) > 0 {
		overview["dependent_count"] = len(dependents)
	}
	if consumers := mapSliceValue(workloadContext, "consumer_repositories"); len(consumers) > 0 {
		overview["consumer_repository_count"] = len(consumers)
	}
	if provisioningChains := mapSliceValue(workloadContext, "provisioning_source_chains"); len(provisioningChains) > 0 {
		overview["provisioning_source_chain_count"] = len(provisioningChains)
	}
	if deploymentEvidence := mapValue(workloadContext, "deployment_evidence"); len(deploymentEvidence) > 0 {
		if toolFamilies := serviceDeploymentToolFamilies(deploymentEvidence); len(toolFamilies) > 0 {
			overview["deployment_tool_families"] = toolFamilies
		}
		if artifactCount := IntVal(deploymentEvidence, "artifact_count"); artifactCount > 0 {
			overview["deployment_evidence_artifact_count"] = artifactCount
		}
		if deliveryPaths := mapSliceValue(deploymentEvidence, "delivery_paths"); len(deliveryPaths) > 0 {
			overview["delivery_path_count"] = len(deliveryPaths)
		}
		if deliveryWorkflows := mapSliceValue(deploymentEvidence, "delivery_workflows"); len(deliveryWorkflows) > 0 {
			overview["delivery_workflow_count"] = len(deliveryWorkflows)
		}
		if sharedConfigPaths := mapSliceValue(deploymentEvidence, "shared_config_paths"); len(sharedConfigPaths) > 0 {
			overview["shared_config_path_count"] = len(sharedConfigPaths)
		}
	}
	return overview
}

func buildServiceStorySections(workloadContext map[string]any) []map[string]any {
	overview := buildServiceDeploymentOverview(workloadContext)
	sections := []map[string]any{
		{
			"title": "deployment",
			"summary": fmt.Sprintf(
				"%d instance(s), %d environment signal(s), %d platform target(s)",
				IntVal(overview, "instance_count"),
				IntVal(overview, "environment_count"),
				IntVal(overview, "platform_count"),
			),
		},
	}

	if hostnames := mapSliceValue(workloadContext, "hostnames"); len(hostnames) > 0 {
		sections = append(sections, map[string]any{
			"title":   "entrypoints",
			"summary": fmt.Sprintf("%d observed hostname entrypoint(s)", len(hostnames)),
		})
	}
	if networkPaths := mapSliceValue(workloadContext, "network_paths"); len(networkPaths) > 0 {
		sections = append(sections, map[string]any{
			"title":   "network",
			"summary": fmt.Sprintf("%d evidence-backed network path(s) connect entrypoints to runtime targets", len(networkPaths)),
		})
	}
	if apiSurface := mapValue(workloadContext, "api_surface"); len(apiSurface) > 0 {
		sections = append(sections, map[string]any{
			"title": "api",
			"summary": fmt.Sprintf(
				"%d endpoint(s), %d method(s), %d spec file(s)",
				IntVal(apiSurface, "endpoint_count"),
				IntVal(apiSurface, "method_count"),
				IntVal(apiSurface, "spec_count"),
			),
		})
	}
	if consumers := mapSliceValue(workloadContext, "consumer_repositories"); len(consumers) > 0 {
		sections = append(sections, map[string]any{
			"title":   "consumers",
			"summary": fmt.Sprintf("%d consumer repo(s) observed from graph and content evidence", len(consumers)),
		})
	}
	if dependents := mapSliceValue(workloadContext, "dependents"); len(dependents) > 0 {
		sections = append(sections, map[string]any{
			"title":   "dependents",
			"summary": fmt.Sprintf("%d graph-derived dependent repo(s) observed from typed relationships", len(dependents)),
		})
	}
	if provisioningChains := mapSliceValue(workloadContext, "provisioning_source_chains"); len(provisioningChains) > 0 {
		sections = append(sections, map[string]any{
			"title":   "provisioning",
			"summary": fmt.Sprintf("%d provisioning source chain(s) observed", len(provisioningChains)),
		})
	}
	if deploymentEvidence := mapValue(workloadContext, "deployment_evidence"); len(deploymentEvidence) > 0 {
		toolFamilies := serviceDeploymentToolFamilies(deploymentEvidence)
		deliveryPathCount := len(mapSliceValue(deploymentEvidence, "delivery_paths"))
		if deliveryPathCount == 0 {
			deliveryPathCount = IntVal(deploymentEvidence, "artifact_count")
		}
		sections = append(sections, map[string]any{
			"title": "delivery",
			"summary": fmt.Sprintf(
				"%d delivery evidence item(s) across tool families %s",
				deliveryPathCount,
				joinOrNone(toolFamilies),
			),
		})
	}
	return sections
}

func serviceDeploymentToolFamilies(deploymentEvidence map[string]any) []string {
	if toolFamilies := stringSliceValue(deploymentEvidence, "tool_families"); len(toolFamilies) > 0 {
		return toolFamilies
	}
	return stringSliceValue(deploymentEvidence, "artifact_families")
}

func buildServiceDocumentationOverview(
	ctx context.Context,
	graph GraphQuery,
	workloadContext map[string]any,
	evidence ServiceQueryEvidence,
) map[string]any {
	repoID := safeStr(workloadContext, "repo_id")
	repoName := safeStr(workloadContext, "repo_name")
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
			RepoProjection("r"),
		), map[string]any{"repo_id": repoID})
		if err == nil && row != nil {
			repo := RepoRefFromRow(row)
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
	overview := map[string]any{
		"dependency_count":           len(mapSliceValue(workloadContext, "dependencies")),
		"dependent_count":            len(mapSliceValue(workloadContext, "dependents")),
		"consumer_repository_count":  len(mapSliceValue(workloadContext, "consumer_repositories")),
		"provisioning_source_count":  len(mapSliceValue(workloadContext, "provisioning_source_chains")),
		"observed_environment_count": len(StringSliceVal(workloadContext, "observed_config_environments")),
		"entrypoint_host_count":      len(mapSliceValue(workloadContext, "hostnames")),
		"entrypoint_count":           len(mapSliceValue(workloadContext, "entrypoints")),
		"network_path_count":         len(mapSliceValue(workloadContext, "network_paths")),
		"has_api_surface":            len(mapValue(workloadContext, "api_surface")) > 0,
		"has_documentation_overview": len(mapValue(workloadContext, "documentation_overview")) > 0,
	}
	if apiSurface := mapValue(workloadContext, "api_surface"); len(apiSurface) > 0 {
		overview["endpoint_count"] = IntVal(apiSurface, "endpoint_count")
		overview["method_count"] = IntVal(apiSurface, "method_count")
		overview["spec_count"] = IntVal(apiSurface, "spec_count")
	}
	if deploymentEvidence := mapValue(workloadContext, "deployment_evidence"); len(deploymentEvidence) > 0 {
		overview["deployment_tool_family_count"] = len(stringSliceValue(deploymentEvidence, "tool_families"))
		overview["delivery_path_count"] = len(mapSliceValue(deploymentEvidence, "delivery_paths"))
		overview["delivery_workflow_count"] = len(mapSliceValue(deploymentEvidence, "delivery_workflows"))
	}
	return overview
}
