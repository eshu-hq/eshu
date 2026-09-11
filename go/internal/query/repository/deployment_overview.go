// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// BuildRepositoryDeploymentOverview assembles the compact deployment summary for
// repository story responses from already-materialized read-model inputs.
func BuildRepositoryDeploymentOverview(
	workloads []string,
	platforms []string,
	infraFamilies []string,
	infrastructureOverview map[string]any,
) map[string]any {
	deploymentArtifacts := querycontract.MapValue(infrastructureOverview, "deployment_artifacts")
	relationshipOverview := querycontract.MapValue(infrastructureOverview, "relationship_overview")

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

	for _, row := range querycontract.MapSliceValue(deploymentArtifacts, "controller_artifacts") {
		path := strings.TrimSpace(querycontract.StringVal(row, "path"))
		controllerKind := strings.TrimSpace(querycontract.StringVal(row, "controller_kind"))
		if path == "" || controllerKind == "" {
			continue
		}
		entry := map[string]any{
			"path":            path,
			"kind":            "controller_artifact",
			"controller_kind": controllerKind,
		}
		querycontract.CopyStringSliceField(entry, row, "shared_libraries")
		querycontract.CopyStringSliceField(entry, row, "pipeline_calls")
		querycontract.CopyStringSliceField(entry, row, "entry_points")
		querycontract.CopyStringSliceField(entry, row, "ansible_inventories")
		querycontract.CopyStringSliceField(entry, row, "ansible_var_files")
		querycontract.CopyStringSliceField(entry, row, "ansible_task_entrypoints")
		querycontract.CopyStringSliceField(entry, row, "ansible_role_paths")
		if hints := querycontract.MapSliceValue(row, "ansible_playbook_hints"); len(hints) > 0 {
			entry["ansible_playbook_hints"] = hints
		}
		paths = append(paths, entry)
	}

	for _, row := range querycontract.MapSliceValue(deploymentArtifacts, "deployment_artifacts") {
		path := strings.TrimSpace(querycontract.StringVal(row, "relative_path"))
		artifactType := strings.TrimSpace(querycontract.StringVal(row, "artifact_type"))
		if path == "" || artifactType == "" {
			continue
		}
		entry := map[string]any{
			"path":          path,
			"kind":          "runtime_artifact",
			"artifact_type": artifactType,
		}
		if artifactName := strings.TrimSpace(querycontract.StringVal(row, "artifact_name")); artifactName != "" {
			entry["artifact_name"] = artifactName
		}
		if baseImage := strings.TrimSpace(querycontract.StringVal(row, "base_image")); baseImage != "" {
			entry["base_image"] = baseImage
		}
		if buildContext := strings.TrimSpace(querycontract.StringVal(row, "build_context")); buildContext != "" {
			entry["build_context"] = buildContext
		}
		if serviceName := strings.TrimSpace(querycontract.StringVal(row, "service_name")); serviceName != "" {
			entry["service_name"] = serviceName
		}
		if cmd := strings.TrimSpace(querycontract.StringVal(row, "cmd")); cmd != "" {
			entry["cmd"] = cmd
		}
		querycontract.CopyStringSliceField(entry, row, "env_files")
		querycontract.CopyStringSliceField(entry, row, "configs")
		querycontract.CopyStringSliceField(entry, row, "secrets")
		if signals := querycontract.StringSliceValue(row, "signals"); len(signals) > 0 {
			entry["signals"] = signals
		}
		paths = append(paths, entry)
	}

	for _, row := range querycontract.MapSliceValue(deploymentArtifacts, "workflow_artifacts") {
		path := strings.TrimSpace(querycontract.StringVal(row, "relative_path"))
		artifactType := strings.TrimSpace(querycontract.StringVal(row, "artifact_type"))
		if path == "" || artifactType == "" {
			continue
		}
		entry := map[string]any{
			"path":          path,
			"kind":          "workflow_artifact",
			"artifact_type": artifactType,
		}
		if workflowName := strings.TrimSpace(querycontract.StringVal(row, "workflow_name")); workflowName != "" {
			entry["workflow_name"] = workflowName
		}
		if commandCount := intValue(row, "command_count"); commandCount > 0 {
			entry["command_count"] = commandCount
		}
		if matrixCombinationCount := intValue(row, "matrix_combination_count"); matrixCombinationCount > 0 {
			entry["matrix_combination_count"] = matrixCombinationCount
		}
		querycontract.CopyStringSliceField(entry, row, "run_commands")
		querycontract.CopyStringSliceField(entry, row, "delivery_command_families")
		querycontract.CopyStringSliceField(entry, row, "delivery_local_paths")
		querycontract.CopyStringSliceField(entry, row, "gating_conditions")
		querycontract.CopyStringSliceField(entry, row, "needs_dependencies")
		querycontract.CopyStringSliceField(entry, row, "trigger_events")
		querycontract.CopyStringSliceField(entry, row, "workflow_inputs")
		querycontract.CopyStringSliceField(entry, row, "permission_scopes")
		querycontract.CopyStringSliceField(entry, row, "concurrency_groups")
		querycontract.CopyStringSliceField(entry, row, "environments")
		querycontract.CopyStringSliceField(entry, row, "job_timeout_minutes")
		querycontract.CopyStringSliceField(entry, row, "matrix_keys")
		querycontract.CopyStringSliceField(entry, row, "local_reusable_workflow_paths")
		querycontract.CopyStringSliceField(entry, row, "reusable_workflow_repositories")
		querycontract.CopyStringSliceField(entry, row, "checkout_repositories")
		querycontract.CopyStringSliceField(entry, row, "action_repositories")
		querycontract.CopyStringSliceField(entry, row, "workflow_input_repositories")
		if signals := querycontract.StringSliceValue(row, "signals"); len(signals) > 0 {
			entry["signals"] = signals
		}
		paths = append(paths, entry)
	}

	for _, row := range querycontract.MapSliceValue(deploymentArtifacts, "config_paths") {
		path := strings.TrimSpace(querycontract.StringVal(row, "path"))
		sourceRepo := strings.TrimSpace(querycontract.StringVal(row, "source_repo"))
		relativePath := strings.TrimSpace(querycontract.StringVal(row, "relative_path"))
		evidenceKind := strings.TrimSpace(querycontract.StringVal(row, "evidence_kind"))
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
		leftPath := querycontract.StringVal(paths[i], "path")
		rightPath := querycontract.StringVal(paths[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return querycontract.StringVal(paths[i], "kind") < querycontract.StringVal(paths[j], "kind")
	})

	return paths
}

func buildOverviewDeliveryWorkflows(deploymentArtifacts map[string]any) []map[string]any {
	rows := querycontract.MapSliceValue(deploymentArtifacts, "controller_artifacts")
	if len(rows) == 0 {
		return nil
	}

	workflows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		path := strings.TrimSpace(querycontract.StringVal(row, "path"))
		controllerKind := strings.TrimSpace(querycontract.StringVal(row, "controller_kind"))
		if path == "" || controllerKind == "" {
			continue
		}
		entry := map[string]any{
			"path":            path,
			"controller_kind": controllerKind,
		}
		querycontract.CopyStringSliceField(entry, row, "shared_libraries")
		querycontract.CopyStringSliceField(entry, row, "pipeline_calls")
		querycontract.CopyStringSliceField(entry, row, "entry_points")
		querycontract.CopyStringSliceField(entry, row, "shell_commands")
		querycontract.CopyStringSliceField(entry, row, "ansible_inventories")
		querycontract.CopyStringSliceField(entry, row, "ansible_var_files")
		querycontract.CopyStringSliceField(entry, row, "ansible_task_entrypoints")
		querycontract.CopyStringSliceField(entry, row, "ansible_role_paths")
		if hints := querycontract.MapSliceValue(row, "ansible_playbook_hints"); len(hints) > 0 {
			entry["ansible_playbook_hints"] = hints
		}
		workflows = append(workflows, entry)
	}

	sort.Slice(workflows, func(i, j int) bool {
		return querycontract.StringVal(workflows[i], "path") < querycontract.StringVal(workflows[j], "path")
	})

	return workflows
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
			querycontract.StringVal(row, "family"),
			querycontract.StringVal(row, "mode"),
			querycontract.StringVal(row, "path"),
			querycontract.StringVal(row, "target_name"),
			querycontract.StringVal(row, "evidence_type"),
		}, "|")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, row)
	}

	for _, row := range querycontract.MapSliceValue(deploymentArtifacts, "controller_artifacts") {
		if strings.TrimSpace(querycontract.StringVal(row, "controller_kind")) != "jenkins_pipeline" {
			continue
		}
		appendPath(map[string]any{
			"family":              "jenkins",
			"tool_family":         "jenkins",
			"mode":                "controller_delivery",
			"path":                strings.TrimSpace(querycontract.StringVal(row, "path")),
			"controller_kind":     "jenkins_pipeline",
			"production_evidence": true,
		})
	}

	for _, row := range querycontract.MapSliceValue(deploymentArtifacts, "deployment_artifacts") {
		path := strings.TrimSpace(querycontract.StringVal(row, "relative_path"))
		switch strings.TrimSpace(querycontract.StringVal(row, "artifact_type")) {
		case "cloudformation_serverless":
			appendPath(map[string]any{
				"family":              "cloudformation",
				"tool_family":         "cloudformation",
				"mode":                "serverless_delivery",
				"path":                path,
				"artifact_type":       "cloudformation_serverless",
				"artifact_name":       strings.TrimSpace(querycontract.StringVal(row, "artifact_name")),
				"production_evidence": true,
			})
		case "docker_compose":
			appendPath(map[string]any{
				"family":              "docker_compose",
				"tool_family":         "docker_compose",
				"mode":                "development_runtime",
				"path":                path,
				"artifact_type":       "docker_compose",
				"service_name":        strings.TrimSpace(querycontract.StringVal(row, "service_name")),
				"production_evidence": false,
			})
		}
	}

	for _, row := range querycontract.MapSliceValue(relationshipOverview, "controller_driven") {
		evidenceType := strings.TrimSpace(querycontract.StringVal(row, "evidence_type"))
		if !strings.HasPrefix(evidenceType, "argocd_") {
			continue
		}
		appendPath(map[string]any{
			"family":              "gitops",
			"tool_family":         "argocd",
			"mode":                "gitops_delivery",
			"type":                strings.TrimSpace(querycontract.StringVal(row, "type")),
			"target_name":         strings.TrimSpace(querycontract.StringVal(row, "target_name")),
			"target_id":           strings.TrimSpace(querycontract.StringVal(row, "target_id")),
			"evidence_type":       evidenceType,
			"production_evidence": true,
		})
	}

	sort.Slice(paths, func(i, j int) bool {
		if left, right := querycontract.StringVal(paths[i], "family"), querycontract.StringVal(paths[j], "family"); left != right {
			return left < right
		}
		if left, right := querycontract.StringVal(paths[i], "path"), querycontract.StringVal(paths[j], "path"); left != right {
			return left < right
		}
		return querycontract.StringVal(paths[i], "target_name") < querycontract.StringVal(paths[j], "target_name")
	})

	return paths
}

func buildSharedConfigPaths(deploymentArtifacts map[string]any) []map[string]any {
	rows := querycontract.MapSliceValue(deploymentArtifacts, "config_paths")
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
		path := strings.TrimSpace(querycontract.StringVal(row, "path"))
		sourceRepo := strings.TrimSpace(querycontract.StringVal(row, "source_repo"))
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
		if evidenceKind := strings.TrimSpace(querycontract.StringVal(row, "evidence_kind")); evidenceKind != "" {
			aggregate.evidenceKinds[evidenceKind] = struct{}{}
		}
		if relativePath := strings.TrimSpace(querycontract.StringVal(row, "relative_path")); relativePath != "" {
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
		sourceRepos := querycontract.SortedSetKeys(aggregate.sourceRepos)
		if len(sourceRepos) <= 1 {
			continue
		}
		entry := map[string]any{
			"path":                path,
			"source_repositories": sourceRepos,
		}
		if evidenceKinds := querycontract.SortedSetKeys(aggregate.evidenceKinds); len(evidenceKinds) > 0 {
			entry["evidence_kinds"] = evidenceKinds
		}
		if relativePaths := querycontract.SortedSetKeys(aggregate.relativePaths); len(relativePaths) > 0 {
			entry["relative_paths"] = relativePaths
		}
		result = append(result, entry)
	}
	return result
}

// mapValue extracts a nested map from a map value. The implementation moved
// to querycontract for #6060; this wrapper keeps root callers unchanged.

// mapSliceValue extracts a []map[string]any from a map value. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.

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
