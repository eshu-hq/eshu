package query

import (
	"path"
	"sort"
	"strings"
)

const maxDeploymentTraceServiceEntrypoints = 4

func buildDeploymentTraceArtifactLineage(
	controllerEntities []map[string]any,
	deploymentEvidence map[string]any,
	k8sResources []map[string]any,
	hostnames []map[string]any,
	_ map[string]any,
) []map[string]any {
	if len(k8sResources) == 0 {
		return nil
	}

	entrypoints := boundedDeploymentTraceHostnames(hostnames)
	lineage := make([]map[string]any, 0, len(controllerEntities)+len(traceMapSlice(deploymentEvidence, "delivery_paths")))
	seen := map[string]struct{}{}
	appendLineage := func(row map[string]any) {
		if len(row) == 0 {
			return
		}
		key := strings.Join([]string{
			traceString(row, "source_kind"),
			traceString(row, "source_path"),
			traceString(row, "artifact_kind"),
			traceString(row, "artifact_value"),
			traceString(row, "deployment_target_path"),
		}, "|")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		lineage = append(lineage, row)
	}

	for _, entity := range controllerEntities {
		sourcePath := traceString(entity, "relative_path")
		sourceRoot := traceString(entity, "source_root")
		if sourcePath == "" || sourceRoot == "" {
			continue
		}
		target := bestMatchingK8sResource(k8sResources, sourceRoot)
		if len(target) == 0 {
			continue
		}
		appendLineage(newDeploymentTraceLineageRow(
			"controller_entity",
			traceString(entity, "entity_name"),
			sourcePath,
			[]string{"argocd"},
			"controller_source_root",
			sourceRoot,
			target,
			entrypoints,
		))
	}

	for _, row := range traceMapSlice(deploymentEvidence, "delivery_paths") {
		switch traceString(row, "kind") {
		case "workflow_artifact":
			localPaths := traceStringSlice(row, "delivery_local_paths")
			if len(localPaths) == 0 {
				continue
			}
			target := bestMatchingK8sResource(k8sResources, localPaths[0])
			if len(target) == 0 {
				continue
			}
			families := traceUniqueSortedStrings(append([]string{"github_actions"}, traceStringSlice(row, "delivery_command_families")...))
			appendLineage(newDeploymentTraceLineageRow(
				"workflow_artifact",
				traceString(row, "workflow_name"),
				traceString(row, "path"),
				families,
				"delivery_local_path",
				localPaths[0],
				target,
				entrypoints,
			))
		case "controller_artifact":
			hints := traceMapSlice(row, "ansible_playbook_hints")
			if len(hints) == 0 {
				continue
			}
			playbook := traceString(hints[0], "playbook")
			target := bestMatchingK8sResource(k8sResources, playbook)
			if len(target) == 0 {
				continue
			}
			appendLineage(newDeploymentTraceLineageRow(
				"controller_artifact",
				traceString(row, "path"),
				traceString(row, "path"),
				[]string{"ansible", "jenkins"},
				"ansible_playbook_hint",
				playbook,
				target,
				entrypoints,
			))
		}
	}

	sort.SliceStable(lineage, func(i, j int) bool {
		if left, right := traceString(lineage[i], "source_kind"), traceString(lineage[j], "source_kind"); left != right {
			return left < right
		}
		return traceString(lineage[i], "source_path") < traceString(lineage[j], "source_path")
	})
	return lineage
}

func buildDeploymentTraceProvenanceOverview(
	_ []map[string]any,
	_ []map[string]any,
	deploymentEvidence map[string]any,
	lineage []map[string]any,
) map[string]any {
	families := make([]string, 0, len(lineage)*2)
	workflowCount := 0
	controllerCount := 0
	runtimeCount := 0
	for _, row := range traceMapSlice(deploymentEvidence, "delivery_paths") {
		switch traceString(row, "kind") {
		case "workflow_artifact":
			workflowCount++
			workflowFamilies := append([]string{"github_actions"}, traceStringSlice(row, "delivery_command_families")...)
			families = append(families, workflowFamilies...)
		case "controller_artifact":
			controllerCount++
			switch controllerKind := traceString(row, "controller_kind"); controllerKind {
			case "jenkins_pipeline":
				families = append(families, "jenkins")
				if len(traceMapSlice(row, "ansible_playbook_hints")) > 0 {
					families = append(families, "ansible")
				}
			case "":
			default:
				families = append(families, controllerKind)
			}
		case "runtime_artifact":
			runtimeCount++
			if family := deploymentTraceRuntimeArtifactFamily(traceString(row, "artifact_type")); family != "" {
				families = append(families, family)
			}
		}
	}
	for _, row := range lineage {
		families = append(families, traceStringSlice(row, "provenance_families")...)
	}
	families = traceUniqueSortedStrings(families)
	if len(families) == 0 && workflowCount == 0 && controllerCount == 0 && len(lineage) == 0 {
		return nil
	}

	return map[string]any{
		"families":                  families,
		"artifact_lineage_count":    len(lineage),
		"workflow_artifact_count":   workflowCount,
		"controller_artifact_count": controllerCount,
		"runtime_artifact_count":    runtimeCount,
		"delivery_path_count":       len(traceMapSlice(deploymentEvidence, "delivery_paths")),
	}
}

func buildDeploymentTraceWorkflowProvenanceStory(deploymentEvidence map[string]any) string {
	workflowSources := make([]string, 0)
	controllerSources := make([]string, 0)
	runtimeSources := make([]string, 0)
	for _, row := range traceMapSlice(deploymentEvidence, "delivery_paths") {
		switch traceString(row, "kind") {
		case "workflow_artifact":
			workflowSources = append(workflowSources, traceString(row, "path"))
		case "controller_artifact":
			controllerSources = append(controllerSources, traceString(row, "path"))
		case "runtime_artifact":
			runtimeSources = append(runtimeSources, traceString(row, "path"))
		}
	}

	parts := make([]string, 0, 2)
	if values := traceUniqueSortedStrings(workflowSources); len(values) > 0 {
		parts = append(parts, "Workflow provenance: "+strings.Join(values, ", ")+".")
	}
	if values := traceUniqueSortedStrings(controllerSources); len(values) > 0 {
		parts = append(parts, "Controller provenance: "+strings.Join(values, ", ")+".")
	}
	if values := traceUniqueSortedStrings(runtimeSources); len(values) > 0 {
		parts = append(parts, "Runtime provenance: "+strings.Join(values, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func deploymentTraceRuntimeArtifactFamily(artifactType string) string {
	switch strings.TrimSpace(artifactType) {
	case "cloudformation_serverless":
		return "cloudformation"
	case "docker_compose":
		return "docker_compose"
	case "dockerfile":
		return "docker"
	default:
		return strings.TrimSpace(artifactType)
	}
}

func newDeploymentTraceLineageRow(
	sourceKind string,
	sourceName string,
	sourcePath string,
	provenanceFamilies []string,
	artifactKind string,
	artifactValue string,
	target map[string]any,
	serviceEntrypoints []string,
) map[string]any {
	row := map[string]any{
		"source_kind":            sourceKind,
		"source_name":            sourceName,
		"source_path":            sourcePath,
		"artifact_kind":          artifactKind,
		"artifact_value":         artifactValue,
		"deployment_target_name": traceString(target, "entity_name"),
		"deployment_target_kind": traceString(target, "kind"),
		"deployment_target_path": traceString(target, "relative_path"),
		"provenance_families":    traceUniqueSortedStrings(provenanceFamilies),
	}
	if imageRefs := traceStringSlice(target, "container_images"); len(imageRefs) > 0 {
		row["image_refs"] = imageRefs
	}
	if len(serviceEntrypoints) > 0 {
		row["service_entrypoints"] = serviceEntrypoints
	}
	return row
}

func boundedDeploymentTraceHostnames(hostnames []map[string]any) []string {
	values := make([]string, 0, len(hostnames))
	seen := map[string]struct{}{}
	for _, row := range hostnames {
		if hostname := traceString(row, "hostname"); hostname != "" {
			if _, ok := seen[hostname]; ok {
				continue
			}
			seen[hostname] = struct{}{}
			values = append(values, hostname)
		}
	}
	sort.SliceStable(values, func(i, j int) bool {
		leftEnvironment := inferHostnameEnvironment(values[i])
		rightEnvironment := inferHostnameEnvironment(values[j])
		leftKnown := leftEnvironment != ""
		rightKnown := rightEnvironment != ""
		switch {
		case leftKnown && !rightKnown:
			return true
		case !leftKnown && rightKnown:
			return false
		case leftKnown && rightKnown && leftEnvironment != rightEnvironment:
			return leftEnvironment < rightEnvironment
		default:
			return values[i] < values[j]
		}
	})
	if len(values) > maxDeploymentTraceServiceEntrypoints {
		return values[:maxDeploymentTraceServiceEntrypoints]
	}
	return values
}

func bestMatchingK8sResource(k8sResources []map[string]any, artifactPath string) map[string]any {
	root := normalizeTraceDeploymentRoot(artifactPath)
	if root == "" {
		return nil
	}

	bestIndex := -1
	bestRank := -1
	bestPath := ""
	for index, row := range k8sResources {
		resourcePath := normalizeTraceDeploymentRoot(traceString(row, "relative_path"))
		if !traceDeploymentPathWithinRoot(resourcePath, root) {
			continue
		}
		rank := traceDeploymentTargetRank(row)
		path := traceString(row, "relative_path")
		if rank > bestRank || (rank == bestRank && (bestPath == "" || path < bestPath)) {
			bestIndex = index
			bestRank = rank
			bestPath = path
		}
	}
	if bestIndex < 0 {
		return nil
	}
	return k8sResources[bestIndex]
}

func traceDeploymentTargetRank(row map[string]any) int {
	score := 0
	if len(traceStringSlice(row, "container_images")) > 0 {
		score += 100
	}
	switch strings.ToLower(traceString(row, "kind")) {
	case "deployment":
		score += 80
	case "statefulset":
		score += 75
	case "daemonset":
		score += 70
	case "job":
		score += 65
	case "cronjob":
		score += 64
	case "service":
		score += 60
	case "xirsarole":
		score += 50
	case "configmap":
		score += 20
	}
	return score
}

func traceString(row map[string]any, key string) string {
	if len(row) == 0 {
		return ""
	}
	value, _ := row[key].(string)
	return strings.TrimSpace(value)
}

func traceStringSlice(row map[string]any, key string) []string {
	if len(row) == 0 {
		return nil
	}
	switch values := row[key].(type) {
	case []string:
		return traceUniqueSortedStrings(values)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return traceUniqueSortedStrings(out)
	default:
		return nil
	}
}

func traceMapSlice(row map[string]any, key string) []map[string]any {
	if len(row) == 0 {
		return nil
	}
	switch values := row[key].(type) {
	case []map[string]any:
		return values
	case []any:
		out := make([]map[string]any, 0, len(values))
		for _, value := range values {
			if entry, ok := value.(map[string]any); ok {
				out = append(out, entry)
			}
		}
		return out
	default:
		return nil
	}
}

func traceUniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func normalizeTraceDeploymentRoot(raw string) string {
	trimmed := strings.TrimSpace(strings.Trim(raw, `"'`))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	if wildcard := strings.Index(trimmed, "*"); wildcard >= 0 {
		trimmed = strings.TrimSuffix(trimmed[:wildcard], "/")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	if ext := path.Ext(cleaned); ext != "" {
		cleaned = path.Dir(cleaned)
	}
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return strings.TrimSuffix(cleaned, "/")
}

func traceDeploymentPathWithinRoot(relativePath string, root string) bool {
	normalizedRoot := normalizeTraceDeploymentRoot(root)
	if relativePath == "" || normalizedRoot == "" {
		return false
	}
	return relativePath == normalizedRoot || strings.HasPrefix(relativePath, normalizedRoot+"/")
}
