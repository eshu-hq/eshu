package query

import "sort"

func buildRepositoryInfrastructureOverview(
	infrastructure []map[string]any,
	files []FileContent,
) map[string]any {
	entityFamilyCounts := map[string]int{}
	entityTypeCounts := map[string]int{}
	artifactFamilyCounts := map[string]int{}

	for _, item := range infrastructure {
		entityType := StringVal(item, "type")
		if entityType == "" {
			continue
		}
		entityTypeCounts[entityType]++
		if family := infraFamilyForEntityType(entityType); family != "" {
			entityFamilyCounts[family]++
		}
	}

	for _, file := range files {
		if family := infraFamilyForArtifactType(file.ArtifactType); family != "" {
			artifactFamilyCounts[family]++
		}
	}

	families := sortedFamilyKeys(entityFamilyCounts, artifactFamilyCounts)
	if len(families) == 0 && len(entityTypeCounts) == 0 {
		return nil
	}

	return map[string]any{
		"families":               families,
		"entity_family_counts":   entityFamilyCounts,
		"entity_type_counts":     entityTypeCounts,
		"artifact_family_counts": artifactFamilyCounts,
	}
}

func infraFamilyForEntityType(entityType string) string {
	switch entityType {
	case "ArgoCDApplication", "ArgoCDApplicationSet":
		return "argocd"
	case "HelmChart", "HelmValues":
		return "helm"
	case "KustomizeOverlay":
		return "kustomize"
	case "CrossplaneXRD", "CrossplaneComposition", "CrossplaneClaim":
		return "crossplane"
	case "TerraformModule", "TerraformResource", "TerraformVariable", "TerraformOutput",
		"TerraformDataSource", "TerraformProvider", "TerraformLocal", "TerraformBackend",
		"TerraformImport", "TerraformMovedBlock", "TerraformRemovedBlock", "TerraformCheck",
		"TerraformLockProvider", "TerraformBlock":
		return "terraform"
	case "TerragruntConfig", "TerragruntDependency", "TerragruntLocal", "TerragruntInput":
		return "terragrunt"
	case "CloudFormationResource", "CloudFormationParameter", "CloudFormationOutput",
		"CloudFormationCondition", "CloudFormationImport", "CloudFormationExport":
		return "cloudformation"
	case "K8sResource":
		return "kubernetes"
	default:
		return ""
	}
}

func infraFamilyForArtifactType(artifactType string) string {
	switch artifactType {
	case "ansible_playbook", "ansible_inventory", "ansible_role", "ansible_vars", "ansible_task_entrypoint":
		return "ansible"
	case "github_actions_workflow":
		return "github_actions"
	case "dockerfile", "docker_compose":
		return "docker"
	default:
		return ""
	}
}

func sortedFamilyKeys(maps ...map[string]int) []string {
	seen := map[string]struct{}{}
	keys := make([]string, 0)
	for _, counts := range maps {
		for key, count := range counts {
			if count <= 0 {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
