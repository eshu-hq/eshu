package query

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
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
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if ctx == nil {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if workloadID := safeStr(ctx, "id"); workloadID != "" {
		deploymentSources, err := h.fetchDeploymentSources(r.Context(), workloadID, safeStr(ctx, "repo_id"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment sources: %v", err))
			return
		}
		cloudResources, err := h.fetchCloudResources(r.Context(), workloadID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query cloud resources: %v", err))
			return
		}
		k8sResources, imageRefs, err := h.fetchK8sResources(r.Context(), safeStr(ctx, "repo_id"), safeStr(ctx, "name"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query k8s resources: %v", err))
			return
		}
		controllerEntities, deploymentRepoK8s, deploymentRepoImages, err := h.fetchDeploymentSourceGitOps(
			r.Context(),
			safeStr(ctx, "name"),
			deploymentSources,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query deployment source gitops evidence: %v", err))
			return
		}
		k8sResources = mergeDeploymentTraceRows(k8sResources, deploymentRepoK8s)
		imageRefs = uniqueSortedStrings(append(append([]string{}, imageRefs...), deploymentRepoImages...))
		ctx["deployment_sources"] = deploymentSources
		ctx["cloud_resources"] = cloudResources
		ctx["k8s_resources"] = k8sResources
		ctx["image_refs"] = imageRefs
		ctx["controller_entities"] = controllerEntities
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
	workloadContext, err := entityHandler.fetchServiceWorkloadContext(ctx, serviceName, "deployment_trace")
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
	deploymentSources, _ := workloadContext["deployment_sources"].([]map[string]any)
	cloudResources, _ := workloadContext["cloud_resources"].([]map[string]any)
	k8sResources, _ := workloadContext["k8s_resources"].([]map[string]any)
	imageRefs, _ := workloadContext["image_refs"].([]string)
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
	deploymentFacts := buildDeploymentFacts(instances, deploymentSources)
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
	deploymentOverview["k8s_resource_count"] = len(k8sResources)
	deploymentOverview["image_ref_count"] = len(imageRefs)
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
		"controller_overview":     buildControllerOverview(platforms, platformKinds, controllerEntities, deploymentSources, deploymentEvidence),
		"gitops_overview":         buildGitOpsOverview(platforms, platformKinds, deploymentSources, deploymentEvidence, controllerEntities),
		"runtime_overview":        buildRuntimeOverview(materializedEnvironments),
		"deployment_fact_summary": deploymentFactSummary,
		"drilldowns":              buildDeploymentDrilldowns(serviceName, safeStr(workloadContext, "id")),
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

func (h *ImpactHandler) fetchDeploymentSources(
	ctx context.Context,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	return fetchDeploymentSourcesFromGraph(ctx, h.Neo4j, workloadID, repoID)
}

func fetchDeploymentSourcesFromGraph(
	ctx context.Context,
	reader GraphQuery,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	canonicalRows, err := reader.Run(ctx, `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)
		RETURN DISTINCT repo.id as repo_id, repo.name as repo_name, rel.confidence as confidence, rel.reason as reason
		ORDER BY repo_name
	`, map[string]any{
		"workload_id": workloadID,
	})
	if err != nil {
		return nil, err
	}
	repositoryRows := []map[string]any{}
	if strings.TrimSpace(repoID) != "" {
		repositoryRows, err = reader.Run(ctx, `
			MATCH (targetRepo:Repository {id: $repo_id})<-[rel:DEPLOYS_FROM]-(repo:Repository)
			RETURN DISTINCT repo.id as repo_id, repo.name as repo_name, rel.confidence as confidence,
			       coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason
			ORDER BY repo_name
		`, map[string]any{
			"repo_id": repoID,
		})
		if err != nil {
			return nil, err
		}
	}
	rows := mergeDeploymentSourceRows(canonicalRows, repositoryRows)
	sources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, map[string]any{
			"repo_id":    StringVal(row, "repo_id"),
			"repo_name":  StringVal(row, "repo_name"),
			"confidence": floatVal(row, "confidence"),
			"reason":     StringVal(row, "reason"),
		})
	}
	return sources, nil
}

func mergeDeploymentSourceRows(
	canonicalRows []map[string]any,
	repositoryRows []map[string]any,
) []map[string]any {
	merged := make([]map[string]any, 0, len(canonicalRows)+len(repositoryRows))
	seen := make(map[string]struct{}, len(canonicalRows)+len(repositoryRows))
	appendRow := func(row map[string]any) {
		key := StringVal(row, "repo_id")
		if key == "" {
			key = StringVal(row, "repo_name")
		}
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, row)
	}
	for _, row := range canonicalRows {
		appendRow(row)
	}
	for _, row := range repositoryRows {
		appendRow(row)
	}
	return merged
}

func buildDeploymentFactSummary(
	workloadContext map[string]any,
	instances []map[string]any,
	materializedEnvironments []string,
	configEnvironments []string,
	platforms []string,
	deploymentSources []map[string]any,
	cloudResources []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	deploymentFacts []map[string]any,
	mappingMode string,
) map[string]any {
	overallConfidence, confidenceReason := deploymentOverallConfidence(instances, deploymentSources, configEnvironments)
	summary := map[string]any{
		"instance_count":                 len(instances),
		"environment_count":              len(materializedEnvironments),
		"materialized_environment_count": len(materializedEnvironments),
		"config_environment_count":       len(configEnvironments),
		"platform_count":                 len(platforms),
		"deployment_source_count":        len(deploymentSources),
		"cloud_resource_count":           len(cloudResources),
		"k8s_resource_count":             len(k8sResources),
		"image_ref_count":                len(imageRefs),
		"fact_count":                     len(deploymentFacts),
		"has_repository":                 safeStr(workloadContext, "repo_id") != "",
		"mapping_mode":                   mappingMode,
		"overall_confidence":             overallConfidence,
		"overall_confidence_reason":      confidenceReason,
	}
	if limitations := deploymentFactSummaryLimitations(instances, configEnvironments); len(limitations) > 0 {
		summary["limitations"] = limitations
	}
	return summary
}

func deploymentOverallConfidence(
	instances []map[string]any,
	deploymentSources []map[string]any,
	configEnvironments []string,
) (float64, string) {
	if len(instances) > 0 {
		minConfidence := 1.0
		found := false
		for _, instance := range instances {
			confidence := firstPositiveFloat(
				floatVal(instance, "materialization_confidence"),
				floatVal(instance, "platform_confidence"),
			)
			if confidence <= 0 {
				continue
			}
			found = true
			if confidence < minConfidence {
				minConfidence = confidence
			}
		}
		if found {
			return minConfidence, "materialized_runtime_instances"
		}
		return 0.9, "materialized_runtime_instances"
	}
	if len(deploymentSources) > 0 {
		minConfidence := 1.0
		found := false
		for _, source := range deploymentSources {
			confidence := floatVal(source, "confidence")
			if confidence <= 0 {
				continue
			}
			found = true
			if confidence < minConfidence {
				minConfidence = confidence
			}
		}
		if found {
			return minConfidence, "canonical_deployment_sources"
		}
		return 0.75, "canonical_deployment_sources"
	}
	if len(configEnvironments) > 0 {
		return 0.45, "config_only_evidence"
	}
	return 0, "no_deployment_evidence"
}

func deploymentFactSummaryLimitations(instances []map[string]any, configEnvironments []string) []string {
	if len(instances) == 0 && len(configEnvironments) == 0 {
		return nil
	}
	limitations := []string{}
	if len(instances) == 0 && len(configEnvironments) > 0 {
		limitations = append(limitations, "config_environments_present_without_materialized_runtime_instances")
	}
	return limitations
}

func (h *ImpactHandler) fetchCloudResources(ctx context.Context, workloadID string) ([]map[string]any, error) {
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:USES]->(c:CloudResource)
		RETURN DISTINCT c.id as id, c.name as name, c.kind as kind, c.provider as provider, c.environment as environment,
		       rel.confidence as confidence, rel.reason as reason
		ORDER BY c.name
	`, map[string]any{"workload_id": workloadID})
	if err != nil {
		return nil, err
	}
	resources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, map[string]any{
			"id":          StringVal(row, "id"),
			"name":        StringVal(row, "name"),
			"kind":        StringVal(row, "kind"),
			"provider":    StringVal(row, "provider"),
			"environment": StringVal(row, "environment"),
			"confidence":  floatVal(row, "confidence"),
			"reason":      StringVal(row, "reason"),
		})
	}
	return resources, nil
}

func (h *ImpactHandler) fetchK8sResources(
	ctx context.Context,
	repoID string,
	workloadName string,
) ([]map[string]any, []string, error) {
	if h == nil || h.Content == nil || repoID == "" || workloadName == "" {
		return nil, nil, nil
	}

	rows, err := h.Content.SearchEntitiesByName(ctx, repoID, "K8sResource", workloadName, 50)
	if err != nil {
		return nil, nil, err
	}

	resources := make([]map[string]any, 0, len(rows))
	imageSet := make(map[string]struct{})
	for _, row := range rows {
		if row.EntityName != workloadName {
			continue
		}
		kind, _ := metadataNonEmptyString(row.Metadata, "kind")
		qualifiedName, _ := metadataNonEmptyString(row.Metadata, "qualified_name")
		images := metadataStringSlice(row.Metadata, "container_images")
		for _, image := range images {
			imageSet[image] = struct{}{}
		}
		resources = append(resources, map[string]any{
			"entity_id":        row.EntityID,
			"entity_name":      row.EntityName,
			"kind":             kind,
			"qualified_name":   qualifiedName,
			"relative_path":    row.RelativePath,
			"container_images": images,
		})
	}

	imageRefs := make([]string, 0, len(imageSet))
	for image := range imageSet {
		imageRefs = append(imageRefs, image)
	}
	sort.Strings(imageRefs)
	return resources, imageRefs, nil
}

func distinctSortedInstanceField(instances []map[string]any, key string) []string {
	values := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if key == "platform_name" || key == "platform_kind" {
			for _, platform := range platformTargets(instance) {
				value := safeStr(platform, key)
				if value == "" {
					continue
				}
				values[value] = struct{}{}
			}
			continue
		}
		value := safeStr(instance, key)
		if value == "" {
			continue
		}
		values[value] = struct{}{}
	}

	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func mergeDeploymentTraceRows(left []map[string]any, right []map[string]any) []map[string]any {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]map[string]any, 0, len(left)+len(right))
	for _, row := range append(append([]map[string]any{}, left...), right...) {
		key := StringVal(row, "entity_id")
		if key == "" {
			key = StringVal(row, "qualified_name") + "|" + StringVal(row, "relative_path")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, row)
	}
	sortDeploymentTraceMaps(merged)
	return merged
}
