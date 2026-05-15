package query

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const deploymentConfigInfluenceCapability = "platform_impact.deployment_config_influence"

type deploymentConfigInfluenceRequest struct {
	ServiceName string `json:"service_name"`
	WorkloadID  string `json:"workload_id"`
	Environment string `json:"environment"`
	Limit       int    `json:"limit"`
}

func (h *ImpactHandler) investigateDeploymentConfigInfluence(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), deploymentConfigInfluenceCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"deployment configuration influence requires authoritative platform truth",
			"unsupported_capability",
			deploymentConfigInfluenceCapability,
			h.profile(),
			requiredProfile(deploymentConfigInfluenceCapability),
		)
		return
	}

	var req deploymentConfigInfluenceRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ServiceName = strings.TrimSpace(req.ServiceName)
	req.WorkloadID = strings.TrimSpace(req.WorkloadID)
	req.Environment = strings.TrimSpace(req.Environment)
	if req.ServiceName == "" && req.WorkloadID == "" {
		WriteError(w, http.StatusBadRequest, "service_name or workload_id is required")
		return
	}

	selector := req.ServiceName
	if selector == "" {
		selector = req.WorkloadID
	}
	ctx, err := fetchServiceTraceContext(r.Context(), h.Neo4j, h.Content, h.Logger, selector, traceEnrichmentConfig{maxDepth: 4})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if ctx == nil {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if workloadID := safeStr(ctx, "id"); workloadID != "" {
		if err := h.enrichDeploymentConfigInfluenceContext(r.Context(), ctx); err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		buildDeploymentConfigInfluenceResponse(req, ctx),
		BuildTruthEnvelope(h.profile(), deploymentConfigInfluenceCapability, TruthBasisHybrid, "resolved from service deployment evidence, topology, and runtime artifacts"),
	)
}

func (h *ImpactHandler) enrichDeploymentConfigInfluenceContext(ctx context.Context, workload map[string]any) error {
	workloadID := safeStr(workload, "id")
	repoID := safeStr(workload, "repo_id")
	sourceCh := make(chan deploymentConfigSourcesResult, 1)
	k8sCh := make(chan deploymentConfigK8sResult, 1)

	go func() {
		deploymentSources, err := h.fetchDeploymentSources(ctx, workloadID, repoID)
		sourceCh <- deploymentConfigSourcesResult{rows: deploymentSources, err: err}
	}()
	go func() {
		k8sResources, imageRefs, err := h.fetchK8sResources(ctx, repoID, safeStr(workload, "name"))
		k8sCh <- deploymentConfigK8sResult{resources: k8sResources, imageRefs: imageRefs, err: err}
	}()

	sourceResult := <-sourceCh
	k8sResult := <-k8sCh
	if sourceResult.err != nil {
		return fmt.Errorf("query deployment sources: %w", sourceResult.err)
	}
	if k8sResult.err != nil {
		return fmt.Errorf("query k8s resources: %w", k8sResult.err)
	}
	controllerEntities, deploymentRepoK8s, deploymentRepoImages, err := h.fetchDeploymentSourceGitOps(ctx, safeStr(workload, "name"), sourceResult.rows)
	if err != nil {
		return fmt.Errorf("query deployment source gitops evidence: %w", err)
	}
	workload["deployment_sources"] = sourceResult.rows
	workload["k8s_resources"] = mergeDeploymentTraceRows(k8sResult.resources, deploymentRepoK8s)
	workload["image_refs"] = uniqueSortedStrings(append(append([]string{}, k8sResult.imageRefs...), deploymentRepoImages...))
	workload["controller_entities"] = controllerEntities
	return nil
}

type deploymentConfigSourcesResult struct {
	rows []map[string]any
	err  error
}

type deploymentConfigK8sResult struct {
	resources []map[string]any
	imageRefs []string
	err       error
}

func buildDeploymentConfigInfluenceResponse(req deploymentConfigInfluenceRequest, workload map[string]any) map[string]any {
	limit := normalizeDeploymentConfigInfluenceLimit(req.Limit)
	serviceName := canonicalServiceName(firstNonEmptyString(req.ServiceName, req.WorkloadID), workload)
	environment := strings.TrimSpace(req.Environment)
	artifacts := deploymentConfigArtifactsForEnvironment(mapSliceValue(mapValue(workload, "deployment_evidence"), "artifacts"), environment)
	deploymentSources := mapSliceValue(workload, "deployment_sources")
	k8sResources := mapSliceValue(workload, "k8s_resources")
	controllerEntities := mapSliceValue(workload, "controller_entities")
	imageRefs := StringSliceVal(workload, "image_refs")

	valuesLayers := filterDeploymentConfigRows(artifacts, deploymentConfigValuesLayer)
	imageTagSources := filterDeploymentConfigRows(artifacts, deploymentConfigImageTag)
	resourceLimitSources := filterDeploymentConfigRows(artifacts, deploymentConfigResourceLimit)
	runtimeSettingSources := filterDeploymentConfigRows(artifacts, deploymentConfigRuntimeSetting)
	if len(imageTagSources) == 0 {
		imageTagSources = imageReferenceRows(imageRefs)
	}
	renderedTargets := renderedDeploymentConfigTargets(k8sResources, controllerEntities, environment)
	influencingRepos := deploymentConfigInfluencingRepositories(workload, deploymentSources, artifacts)
	readFirstFiles := deploymentConfigReadFirstFiles(artifacts)
	if len(readFirstFiles) == 0 {
		readFirstFiles = deploymentConfigReadFirstFiles(valuesLayers)
	}

	truncated := false
	valuesLayers, truncated = capRows(valuesLayers, limit, truncated)
	imageTagSources, truncated = capRows(imageTagSources, limit, truncated)
	resourceLimitSources, truncated = capRows(resourceLimitSources, limit, truncated)
	runtimeSettingSources, truncated = capRows(runtimeSettingSources, limit, truncated)
	renderedTargets, truncated = capRows(renderedTargets, limit, truncated)
	influencingRepos, truncated = capRows(influencingRepos, limit, truncated)
	readFirstFiles, truncated = capRows(readFirstFiles, limit, truncated)

	limitations := deploymentConfigLimitations(environment, artifacts, valuesLayers, renderedTargets)
	return map[string]any{
		"service_name":             serviceName,
		"workload_id":              safeStr(workload, "id"),
		"environment":              environment,
		"subject":                  deploymentConfigSubject(workload, serviceName, environment),
		"story":                    deploymentConfigStory(serviceName, environment, valuesLayers, imageTagSources, runtimeSettingSources, resourceLimitSources, renderedTargets),
		"influencing_repositories": influencingRepos,
		"values_layers":            valuesLayers,
		"image_tag_sources":        imageTagSources,
		"runtime_setting_sources":  runtimeSettingSources,
		"resource_limit_sources":   resourceLimitSources,
		"rendered_targets":         renderedTargets,
		"read_first_files":         readFirstFiles,
		"recommended_next_calls":   deploymentConfigNextCalls(readFirstFiles, serviceName, environment),
		"limitations":              limitations,
		"coverage": map[string]any{
			"query_shape":                "deployment_config_influence_story",
			"limit":                      limit,
			"truncated":                  truncated,
			"artifact_candidate_count":   len(artifacts),
			"deployment_source_count":    len(deploymentSources),
			"rendered_target_count":      len(renderedTargets),
			"environment":                environment,
			"portable_file_handles":      len(readFirstFiles),
			"uses_file_content_payloads": false,
		},
	}
}

func normalizeDeploymentConfigInfluenceLimit(limit int) int {
	if limit <= 0 {
		return 25
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func capRows(rows []map[string]any, limit int, alreadyTruncated bool) ([]map[string]any, bool) {
	capped, truncated := capMapRows(rows, limit)
	return capped, alreadyTruncated || truncated
}

func deploymentConfigArtifactsForEnvironment(artifacts []map[string]any, environment string) []map[string]any {
	rows := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !deploymentConfigEnvironmentMatches(artifact, environment) {
			continue
		}
		row := deploymentConfigPortableRow(artifact)
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	sortDeploymentConfigRows(rows)
	return rows
}

func deploymentConfigEnvironmentMatches(row map[string]any, environment string) bool {
	if environment == "" {
		return true
	}
	rowEnvironment := StringVal(row, "environment")
	if rowEnvironment == "" {
		return true
	}
	return strings.EqualFold(rowEnvironment, environment)
}

func deploymentConfigPortableRow(row map[string]any) map[string]any {
	relativePath := firstNonEmptyString(StringVal(row, "relative_path"), StringVal(row, "path"), StringVal(row, "file_path"), StringVal(row, "config_path"))
	relativePath = strings.TrimPrefix(relativePath, "/")
	repoID := firstNonEmptyString(StringVal(row, "source_repo_id"), StringVal(row, "repo_id"))
	repoName := firstNonEmptyString(StringVal(row, "source_repo_name"), StringVal(row, "repo_name"))
	result := map[string]any{}
	for _, key := range []string{"artifact_family", "evidence_kind", "matched_alias", "matched_value", "environment", "resolved_id"} {
		if value := StringVal(row, key); value != "" {
			result[key] = value
		}
	}
	if repoID != "" {
		result["repo_id"] = repoID
	}
	if repoName != "" {
		result["repo_name"] = repoName
	}
	if relativePath != "" {
		result["relative_path"] = relativePath
	}
	if line := IntVal(row, "start_line"); line > 0 {
		result["start_line"] = line
	}
	if line := IntVal(row, "end_line"); line > 0 {
		result["end_line"] = line
	}
	return result
}

type deploymentConfigPredicate func(map[string]any) bool

func filterDeploymentConfigRows(rows []map[string]any, predicate deploymentConfigPredicate) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if predicate(row) {
			result = append(result, row)
		}
	}
	return result
}

func deploymentConfigValuesLayer(row map[string]any) bool {
	text := deploymentConfigSearchText(row)
	return strings.Contains(text, "helm") || strings.Contains(text, "kustom") ||
		strings.Contains(text, "argocd") || strings.Contains(text, "terraform") ||
		strings.Contains(text, "values") || strings.Contains(text, "application")
}

func deploymentConfigImageTag(row map[string]any) bool {
	text := deploymentConfigSearchText(row)
	return strings.Contains(text, "image") || strings.Contains(text, "tag")
}

func deploymentConfigResourceLimit(row map[string]any) bool {
	text := deploymentConfigSearchText(row)
	return strings.Contains(text, "resource") || strings.Contains(text, "limit") ||
		strings.Contains(text, "request") || strings.Contains(text, "cpu") ||
		strings.Contains(text, "memory")
}

func deploymentConfigRuntimeSetting(row map[string]any) bool {
	text := deploymentConfigSearchText(row)
	return strings.Contains(text, "runtime") || strings.Contains(text, "env.") ||
		strings.Contains(text, "config") || strings.Contains(text, "secret") ||
		strings.Contains(text, "replica") || strings.Contains(text, "probe") ||
		strings.Contains(text, "command") || strings.Contains(text, "args")
}

func deploymentConfigSearchText(row map[string]any) string {
	return strings.ToLower(strings.Join([]string{
		StringVal(row, "artifact_family"),
		StringVal(row, "evidence_kind"),
		StringVal(row, "matched_alias"),
		StringVal(row, "matched_value"),
		StringVal(row, "relative_path"),
	}, " "))
}

func imageReferenceRows(imageRefs []string) []map[string]any {
	rows := make([]map[string]any, 0, len(imageRefs))
	for _, imageRef := range imageRefs {
		if imageRef == "" {
			continue
		}
		rows = append(rows, map[string]any{"image_ref": imageRef, "source": "kubernetes_resource_image"})
	}
	sortDeploymentConfigRows(rows)
	return rows
}

func renderedDeploymentConfigTargets(k8sResources []map[string]any, controllerEntities []map[string]any, environment string) []map[string]any {
	rows := make([]map[string]any, 0, len(k8sResources)+len(controllerEntities))
	for _, resource := range k8sResources {
		if !deploymentConfigEnvironmentMatches(resource, environment) {
			continue
		}
		rows = append(rows, deploymentConfigTargetRow(resource, "kubernetes_resource"))
	}
	for _, controller := range controllerEntities {
		if !deploymentConfigEnvironmentMatches(controller, environment) {
			continue
		}
		rows = append(rows, deploymentConfigTargetRow(controller, "controller"))
	}
	sortDeploymentConfigRows(rows)
	return rows
}

func deploymentConfigTargetRow(row map[string]any, source string) map[string]any {
	result := map[string]any{"source": source}
	for _, key := range []string{"id", "entity_id", "name", "entity_name", "kind", "namespace", "environment"} {
		if value := StringVal(row, key); value != "" {
			result[key] = value
		}
	}
	return result
}

func deploymentConfigInfluencingRepositories(workload map[string]any, deploymentSources []map[string]any, artifacts []map[string]any) []map[string]any {
	seen := map[string]map[string]any{}
	addRepo := func(repoID string, repoName string, role string) {
		key := firstNonEmptyString(repoID, repoName)
		if key == "" {
			return
		}
		row := seen[key]
		if row == nil {
			row = map[string]any{"repo_id": repoID, "repo_name": repoName}
			seen[key] = row
		}
		addUniqueStringField(row, "roles", role)
	}
	addRepo(safeStr(workload, "repo_id"), safeStr(workload, "repo_name"), "service_owner")
	for _, source := range deploymentSources {
		addRepo(StringVal(source, "repo_id"), StringVal(source, "repo_name"), "deployment_source")
	}
	for _, artifact := range artifacts {
		addRepo(StringVal(artifact, "repo_id"), StringVal(artifact, "repo_name"), "configuration_artifact")
	}
	rows := make([]map[string]any, 0, len(seen))
	for _, row := range seen {
		sortStringFields(row, "roles")
		rows = append(rows, row)
	}
	sortDeploymentConfigRows(rows)
	return rows
}

func deploymentConfigReadFirstFiles(artifacts []map[string]any) []map[string]any {
	seen := map[string]map[string]any{}
	for _, artifact := range artifacts {
		repoID := StringVal(artifact, "repo_id")
		relativePath := StringVal(artifact, "relative_path")
		if repoID == "" || relativePath == "" {
			continue
		}
		key := repoID + "\x00" + relativePath
		row := seen[key]
		if row == nil {
			row = map[string]any{
				"repo_id":       repoID,
				"repo_name":     StringVal(artifact, "repo_name"),
				"relative_path": relativePath,
				"next_call":     "get_file_lines",
				"reason":        "configuration influence evidence",
			}
			seen[key] = row
		}
		if line := IntVal(artifact, "start_line"); line > 0 {
			row["start_line"] = line
		}
		if line := IntVal(artifact, "end_line"); line > 0 {
			row["end_line"] = line
		}
		addUniqueStringField(row, "evidence_kinds", StringVal(artifact, "evidence_kind"))
	}
	rows := make([]map[string]any, 0, len(seen))
	for _, row := range seen {
		sortStringFields(row, "evidence_kinds")
		rows = append(rows, row)
	}
	sortDeploymentConfigRows(rows)
	return rows
}

func deploymentConfigSubject(workload map[string]any, serviceName string, environment string) map[string]any {
	return map[string]any{"type": "service", "id": safeStr(workload, "id"), "name": serviceName, "environment": environment}
}

func deploymentConfigStory(serviceName string, environment string, valuesLayers []map[string]any, imageSources []map[string]any, runtimeSources []map[string]any, resourceSources []map[string]any, targets []map[string]any) string {
	scope := serviceName
	if environment != "" {
		scope += " in " + environment
	}
	return fmt.Sprintf("%s is influenced by %d values layer(s), %d image tag source(s), %d runtime setting source(s), and %d resource limit source(s). The trace includes %d rendered or controller target(s). Read the returned file handles first, then drill into relationship evidence for exact provenance.",
		scope, len(valuesLayers), len(imageSources), len(runtimeSources), len(resourceSources), len(targets))
}

func deploymentConfigLimitations(environment string, artifacts []map[string]any, valuesLayers []map[string]any, targets []map[string]any) []string {
	limitations := []string{}
	if environment != "" {
		limitations = append(limitations, "Rows without explicit environment are retained because shared Helm or ArgoCD layers can still apply to the requested environment.")
	}
	if len(artifacts) == 0 {
		limitations = append(limitations, "No deployment configuration artifacts were materialized for this service.")
	}
	if len(valuesLayers) == 0 {
		limitations = append(limitations, "No Helm, Kustomize, ArgoCD, or Terraform values layer was found in the indexed evidence.")
	}
	if len(targets) == 0 {
		limitations = append(limitations, "No rendered Kubernetes or controller target was found in the indexed evidence.")
	}
	return limitations
}

func deploymentConfigNextCalls(readFirstFiles []map[string]any, serviceName string, environment string) []string {
	calls := []string{"trace_deployment_chain(service_name: " + serviceName + ")"}
	if environment != "" {
		calls = append(calls, "investigate_deployment_config(service_name: "+serviceName+", environment: "+environment+")")
	}
	if len(readFirstFiles) > 0 {
		first := readFirstFiles[0]
		calls = append(calls, "get_file_lines(repo_id: "+StringVal(first, "repo_id")+", relative_path: "+StringVal(first, "relative_path")+")")
	}
	return calls
}

func sortDeploymentConfigRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := firstNonEmptyString(StringVal(rows[i], "repo_id"), StringVal(rows[i], "repo_name")) + "\x00" +
			firstNonEmptyString(StringVal(rows[i], "relative_path"), StringVal(rows[i], "name"), StringVal(rows[i], "entity_name"), StringVal(rows[i], "image_ref"))
		right := firstNonEmptyString(StringVal(rows[j], "repo_id"), StringVal(rows[j], "repo_name")) + "\x00" +
			firstNonEmptyString(StringVal(rows[j], "relative_path"), StringVal(rows[j], "name"), StringVal(rows[j], "entity_name"), StringVal(rows[j], "image_ref"))
		return left < right
	})
}
