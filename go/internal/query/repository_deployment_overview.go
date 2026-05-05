package query

import (
	"sort"
	"strings"
)

// BuildRepositoryDeploymentOverview assembles the compact deployment summary for
// repository story responses from already-materialized read-model inputs.
func BuildRepositoryDeploymentOverview(
	workloads []string,
	platforms []string,
	infraFamilies []string,
	infrastructureOverview map[string]any,
) map[string]any {
	deploymentArtifacts := mapValue(infrastructureOverview, "deployment_artifacts")
	relationshipOverview := mapValue(infrastructureOverview, "relationship_overview")

	overview := map[string]any{
		"workload_count":          len(workloads),
		"platform_count":          len(platforms),
		"workloads":               workloads,
		"platforms":               platforms,
		"infrastructure_families": infraFamilies,
	}
	if len(deploymentArtifacts) > 0 {
		overview["deployment_artifacts"] = deploymentArtifacts
	}

	sharedConfigPaths := buildSharedConfigPaths(deploymentArtifacts)
	if len(sharedConfigPaths) > 0 {
		overview["shared_config_paths"] = sharedConfigPaths
	}

	deliveryPaths := buildOverviewDeliveryPaths(deploymentArtifacts)
	if len(deliveryPaths) > 0 {
		overview["delivery_paths"] = deliveryPaths
	}

	deliveryWorkflows := buildOverviewDeliveryWorkflows(deploymentArtifacts)
	if len(deliveryWorkflows) > 0 {
		overview["delivery_workflows"] = deliveryWorkflows
	}

	deliveryFamilyPaths := buildOverviewDeliveryFamilyPaths(deploymentArtifacts, relationshipOverview)
	if len(deliveryFamilyPaths) > 0 {
		overview["delivery_family_paths"] = deliveryFamilyPaths
	}
	deliveryFamilyStory := buildOverviewDeliveryFamilyStory(deliveryFamilyPaths)
	if len(deliveryFamilyStory) > 0 {
		overview["delivery_family_story"] = deliveryFamilyStory
	}

	topologyStory := buildOverviewTopologyStory(deliveryPaths, sharedConfigPaths)
	if len(topologyStory) > 0 {
		overview["topology_story"] = topologyStory
	}

	return overview
}

func buildOverviewDeliveryPaths(deploymentArtifacts map[string]any) []map[string]any {
	paths := make([]map[string]any, 0)

	for _, row := range mapSliceValue(deploymentArtifacts, "controller_artifacts") {
		path := strings.TrimSpace(StringVal(row, "path"))
		controllerKind := strings.TrimSpace(StringVal(row, "controller_kind"))
		if path == "" || controllerKind == "" {
			continue
		}
		entry := map[string]any{
			"path":            path,
			"kind":            "controller_artifact",
			"controller_kind": controllerKind,
		}
		copyStringSliceField(entry, row, "shared_libraries")
		copyStringSliceField(entry, row, "pipeline_calls")
		copyStringSliceField(entry, row, "entry_points")
		copyStringSliceField(entry, row, "ansible_inventories")
		copyStringSliceField(entry, row, "ansible_var_files")
		copyStringSliceField(entry, row, "ansible_task_entrypoints")
		copyStringSliceField(entry, row, "ansible_role_paths")
		if hints := mapSliceValue(row, "ansible_playbook_hints"); len(hints) > 0 {
			entry["ansible_playbook_hints"] = hints
		}
		paths = append(paths, entry)
	}

	for _, row := range mapSliceValue(deploymentArtifacts, "deployment_artifacts") {
		path := strings.TrimSpace(StringVal(row, "relative_path"))
		artifactType := strings.TrimSpace(StringVal(row, "artifact_type"))
		if path == "" || artifactType == "" {
			continue
		}
		entry := map[string]any{
			"path":          path,
			"kind":          "runtime_artifact",
			"artifact_type": artifactType,
		}
		if artifactName := strings.TrimSpace(StringVal(row, "artifact_name")); artifactName != "" {
			entry["artifact_name"] = artifactName
		}
		if baseImage := strings.TrimSpace(StringVal(row, "base_image")); baseImage != "" {
			entry["base_image"] = baseImage
		}
		if buildContext := strings.TrimSpace(StringVal(row, "build_context")); buildContext != "" {
			entry["build_context"] = buildContext
		}
		if serviceName := strings.TrimSpace(StringVal(row, "service_name")); serviceName != "" {
			entry["service_name"] = serviceName
		}
		if cmd := strings.TrimSpace(StringVal(row, "cmd")); cmd != "" {
			entry["cmd"] = cmd
		}
		copyStringSliceField(entry, row, "env_files")
		copyStringSliceField(entry, row, "configs")
		copyStringSliceField(entry, row, "secrets")
		if signals := stringSliceValue(row, "signals"); len(signals) > 0 {
			entry["signals"] = signals
		}
		paths = append(paths, entry)
	}

	for _, row := range mapSliceValue(deploymentArtifacts, "workflow_artifacts") {
		path := strings.TrimSpace(StringVal(row, "relative_path"))
		artifactType := strings.TrimSpace(StringVal(row, "artifact_type"))
		if path == "" || artifactType == "" {
			continue
		}
		entry := map[string]any{
			"path":          path,
			"kind":          "workflow_artifact",
			"artifact_type": artifactType,
		}
		if workflowName := strings.TrimSpace(StringVal(row, "workflow_name")); workflowName != "" {
			entry["workflow_name"] = workflowName
		}
		if commandCount := intValue(row, "command_count"); commandCount > 0 {
			entry["command_count"] = commandCount
		}
		if matrixCombinationCount := intValue(row, "matrix_combination_count"); matrixCombinationCount > 0 {
			entry["matrix_combination_count"] = matrixCombinationCount
		}
		copyStringSliceField(entry, row, "run_commands")
		copyStringSliceField(entry, row, "delivery_command_families")
		copyStringSliceField(entry, row, "delivery_local_paths")
		copyStringSliceField(entry, row, "gating_conditions")
		copyStringSliceField(entry, row, "needs_dependencies")
		copyStringSliceField(entry, row, "trigger_events")
		copyStringSliceField(entry, row, "workflow_inputs")
		copyStringSliceField(entry, row, "permission_scopes")
		copyStringSliceField(entry, row, "concurrency_groups")
		copyStringSliceField(entry, row, "environments")
		copyStringSliceField(entry, row, "job_timeout_minutes")
		copyStringSliceField(entry, row, "matrix_keys")
		copyStringSliceField(entry, row, "local_reusable_workflow_paths")
		copyStringSliceField(entry, row, "reusable_workflow_repositories")
		copyStringSliceField(entry, row, "checkout_repositories")
		copyStringSliceField(entry, row, "action_repositories")
		copyStringSliceField(entry, row, "workflow_input_repositories")
		if signals := stringSliceValue(row, "signals"); len(signals) > 0 {
			entry["signals"] = signals
		}
		paths = append(paths, entry)
	}

	for _, row := range mapSliceValue(deploymentArtifacts, "config_paths") {
		path := strings.TrimSpace(StringVal(row, "path"))
		sourceRepo := strings.TrimSpace(StringVal(row, "source_repo"))
		relativePath := strings.TrimSpace(StringVal(row, "relative_path"))
		evidenceKind := strings.TrimSpace(StringVal(row, "evidence_kind"))
		if path == "" || sourceRepo == "" || relativePath == "" || evidenceKind == "" {
			continue
		}
		paths = append(paths, map[string]any{
			"path":          path,
			"kind":          "config_artifact",
			"source_repo":   sourceRepo,
			"relative_path": relativePath,
			"evidence_kind": evidenceKind,
		})
	}

	sort.Slice(paths, func(i, j int) bool {
		leftPath := StringVal(paths[i], "path")
		rightPath := StringVal(paths[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return StringVal(paths[i], "kind") < StringVal(paths[j], "kind")
	})

	return paths
}

func buildOverviewDeliveryWorkflows(deploymentArtifacts map[string]any) []map[string]any {
	rows := mapSliceValue(deploymentArtifacts, "controller_artifacts")
	if len(rows) == 0 {
		return nil
	}

	workflows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		path := strings.TrimSpace(StringVal(row, "path"))
		controllerKind := strings.TrimSpace(StringVal(row, "controller_kind"))
		if path == "" || controllerKind == "" {
			continue
		}
		entry := map[string]any{
			"path":            path,
			"controller_kind": controllerKind,
		}
		copyStringSliceField(entry, row, "shared_libraries")
		copyStringSliceField(entry, row, "pipeline_calls")
		copyStringSliceField(entry, row, "entry_points")
		copyStringSliceField(entry, row, "shell_commands")
		copyStringSliceField(entry, row, "ansible_inventories")
		copyStringSliceField(entry, row, "ansible_var_files")
		copyStringSliceField(entry, row, "ansible_task_entrypoints")
		copyStringSliceField(entry, row, "ansible_role_paths")
		if hints := mapSliceValue(row, "ansible_playbook_hints"); len(hints) > 0 {
			entry["ansible_playbook_hints"] = hints
		}
		workflows = append(workflows, entry)
	}

	sort.Slice(workflows, func(i, j int) bool {
		return StringVal(workflows[i], "path") < StringVal(workflows[j], "path")
	})

	return workflows
}

func copyStringSliceField(dst map[string]any, src map[string]any, key string) {
	if values := stringSliceValue(src, key); len(values) > 0 {
		dst[key] = values
	}
}

func buildOverviewDeliveryFamilyPaths(
	deploymentArtifacts map[string]any,
	relationshipOverview map[string]any,
) []map[string]any {
	paths := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	appendPath := func(row map[string]any) {
		if len(row) == 0 {
			return
		}
		key := strings.Join([]string{
			StringVal(row, "family"),
			StringVal(row, "mode"),
			StringVal(row, "path"),
			StringVal(row, "target_name"),
			StringVal(row, "evidence_type"),
		}, "|")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, row)
	}

	for _, row := range mapSliceValue(deploymentArtifacts, "controller_artifacts") {
		if strings.TrimSpace(StringVal(row, "controller_kind")) != "jenkins_pipeline" {
			continue
		}
		appendPath(map[string]any{
			"family":              "jenkins",
			"tool_family":         "jenkins",
			"mode":                "controller_delivery",
			"path":                strings.TrimSpace(StringVal(row, "path")),
			"controller_kind":     "jenkins_pipeline",
			"production_evidence": true,
		})
	}

	for _, row := range mapSliceValue(deploymentArtifacts, "deployment_artifacts") {
		path := strings.TrimSpace(StringVal(row, "relative_path"))
		switch strings.TrimSpace(StringVal(row, "artifact_type")) {
		case "cloudformation_serverless":
			appendPath(map[string]any{
				"family":              "cloudformation",
				"tool_family":         "cloudformation",
				"mode":                "serverless_delivery",
				"path":                path,
				"artifact_type":       "cloudformation_serverless",
				"artifact_name":       strings.TrimSpace(StringVal(row, "artifact_name")),
				"production_evidence": true,
			})
		case "docker_compose":
			appendPath(map[string]any{
				"family":              "docker_compose",
				"tool_family":         "docker_compose",
				"mode":                "development_runtime",
				"path":                path,
				"artifact_type":       "docker_compose",
				"service_name":        strings.TrimSpace(StringVal(row, "service_name")),
				"production_evidence": false,
			})
		}
	}

	for _, row := range mapSliceValue(relationshipOverview, "controller_driven") {
		evidenceType := strings.TrimSpace(StringVal(row, "evidence_type"))
		if !strings.HasPrefix(evidenceType, "argocd_") {
			continue
		}
		appendPath(map[string]any{
			"family":              "gitops",
			"tool_family":         "argocd",
			"mode":                "gitops_delivery",
			"type":                strings.TrimSpace(StringVal(row, "type")),
			"target_name":         strings.TrimSpace(StringVal(row, "target_name")),
			"target_id":           strings.TrimSpace(StringVal(row, "target_id")),
			"evidence_type":       evidenceType,
			"production_evidence": true,
		})
	}

	sort.Slice(paths, func(i, j int) bool {
		if left, right := StringVal(paths[i], "family"), StringVal(paths[j], "family"); left != right {
			return left < right
		}
		if left, right := StringVal(paths[i], "path"), StringVal(paths[j], "path"); left != right {
			return left < right
		}
		return StringVal(paths[i], "target_name") < StringVal(paths[j], "target_name")
	})

	return paths
}

func buildSharedConfigPaths(deploymentArtifacts map[string]any) []map[string]any {
	rows := mapSliceValue(deploymentArtifacts, "config_paths")
	if len(rows) == 0 {
		return nil
	}

	type sharedConfigAggregate struct {
		sourceRepos   map[string]struct{}
		evidenceKinds map[string]struct{}
		relativePaths map[string]struct{}
	}

	grouped := map[string]*sharedConfigAggregate{}
	for _, row := range rows {
		path := strings.TrimSpace(StringVal(row, "path"))
		sourceRepo := strings.TrimSpace(StringVal(row, "source_repo"))
		if path == "" || sourceRepo == "" {
			continue
		}
		aggregate, ok := grouped[path]
		if !ok {
			aggregate = &sharedConfigAggregate{
				sourceRepos:   map[string]struct{}{},
				evidenceKinds: map[string]struct{}{},
				relativePaths: map[string]struct{}{},
			}
			grouped[path] = aggregate
		}
		aggregate.sourceRepos[sourceRepo] = struct{}{}
		if evidenceKind := strings.TrimSpace(StringVal(row, "evidence_kind")); evidenceKind != "" {
			aggregate.evidenceKinds[evidenceKind] = struct{}{}
		}
		if relativePath := strings.TrimSpace(StringVal(row, "relative_path")); relativePath != "" {
			aggregate.relativePaths[relativePath] = struct{}{}
		}
	}

	paths := make([]string, 0, len(grouped))
	for path, aggregate := range grouped {
		if len(aggregate.sourceRepos) > 1 {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	result := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		aggregate := grouped[path]
		sourceRepos := sortedSetKeys(aggregate.sourceRepos)
		if len(sourceRepos) <= 1 {
			continue
		}
		entry := map[string]any{
			"path":                path,
			"source_repositories": sourceRepos,
		}
		if evidenceKinds := sortedSetKeys(aggregate.evidenceKinds); len(evidenceKinds) > 0 {
			entry["evidence_kinds"] = evidenceKinds
		}
		if relativePaths := sortedSetKeys(aggregate.relativePaths); len(relativePaths) > 0 {
			entry["relative_paths"] = relativePaths
		}
		result = append(result, entry)
	}
	return result
}

func mapValue(value map[string]any, key string) map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	typed, ok := raw.(map[string]any)
	if !ok || len(typed) == 0 {
		return nil
	}
	return typed
}

func mapSliceValue(value map[string]any, key string) []map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]map[string]any)
	if ok {
		return items
	}
	typed, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(typed))
	for _, item := range typed {
		row, ok := item.(map[string]any)
		if ok {
			result = append(result, row)
		}
	}
	return result
}

func stringSliceValue(value map[string]any, key string) []string {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return nonEmptyStrings(typed)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	default:
		return nil
	}
}

func intValue(value map[string]any, key string) int {
	if len(value) == 0 {
		return 0
	}
	raw, ok := value[key]
	if !ok {
		return 0
	}
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func sortedSetKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
