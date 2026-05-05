package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

var evidenceKindToType = map[relationships.EvidenceKind]string{
	relationships.EvidenceKindTerraformAppRepo:                     "terraform_app_repo",
	relationships.EvidenceKindTerraformAppName:                     "terraform_app_name",
	relationships.EvidenceKindTerraformGitHubRepo:                  "terraform_github_repository",
	relationships.EvidenceKindTerraformGitHubActions:               "terraform_github_actions_repository",
	relationships.EvidenceKindTerraformConfigPath:                  "terraform_config_path",
	relationships.EvidenceKindTerraformIAMPermission:               "terraform_iam_permission",
	relationships.EvidenceKindTerraformModuleSource:                "terraform_module_source",
	relationships.EvidenceKindTerragruntDependencyConfigPath:       "terragrunt_dependency_config_path",
	relationships.EvidenceKindTerragruntConfigAssetPath:            "terragrunt_config_asset_path",
	relationships.EvidenceKindHelmChart:                            "helm_chart_reference",
	relationships.EvidenceKindHelmValues:                           "helm_values_reference",
	relationships.EvidenceKindArgoCDAppSource:                      "argocd_application_source",
	relationships.EvidenceKindArgoCDApplicationSetDiscovery:        "argocd_applicationset_discovery",
	relationships.EvidenceKindArgoCDApplicationSetDeploySource:     "argocd_applicationset_deploy_source",
	relationships.EvidenceKindArgoCDDestinationPlatform:            "argocd_destination_platform",
	relationships.EvidenceKindGitHubActionsReusableWorkflow:        "github_actions_reusable_workflow_ref",
	relationships.EvidenceKindGitHubActionsLocalReusableWorkflow:   "github_actions_local_reusable_workflow_ref",
	relationships.EvidenceKindGitHubActionsCheckoutRepository:      "github_actions_checkout_repository",
	relationships.EvidenceKindGitHubActionsWorkflowInputRepository: "github_actions_workflow_input_repository",
	relationships.EvidenceKindGitHubActionsActionRepository:        "github_actions_action_repository",
	relationships.EvidenceKindJenkinsSharedLibrary:                 "jenkins_shared_library",
	relationships.EvidenceKindJenkinsGitHubRepository:              "jenkins_github_repository",
	relationships.EvidenceKindDockerComposeBuildContext:            "docker_compose_build_context",
	relationships.EvidenceKindDockerComposeImage:                   "docker_compose_image",
	relationships.EvidenceKindDockerComposeDependsOn:               "docker_compose_depends_on",
	relationships.EvidenceKindDockerfileSourceLabel:                "dockerfile_source_label",
	relationships.EvidenceKindKustomizeResource:                    "kustomize_resource_reference",
	relationships.EvidenceKindKustomizeHelmChart:                   "kustomize_helm_chart_reference",
	relationships.EvidenceKindKustomizeImage:                       "kustomize_image_reference",
	relationships.EvidenceKindAnsibleRoleReference:                 "ansible_role_reference",
}

func resolvedRelationshipEvidenceType(r relationships.ResolvedRelationship) string {
	if kind := firstEvidenceKindFromPreview(r.Details); kind != "" {
		return normalizeEvidenceKind(kind)
	}
	if kinds := stringSliceDetail(r.Details, "evidence_kinds"); len(kinds) > 0 {
		return normalizeEvidenceKind(kinds[0])
	}
	return ""
}

func firstEvidenceKindFromPreview(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}
	if items, ok := details["evidence_preview"].([]map[string]any); ok {
		for _, item := range items {
			if kind := strings.TrimSpace(anyString(item["kind"])); kind != "" {
				return kind
			}
		}
	}
	items, ok := details["evidence_preview"].([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind := strings.TrimSpace(anyString(row["kind"])); kind != "" {
			return kind
		}
	}
	return ""
}

func stringSliceDetail(details map[string]any, key string) []string {
	if len(details) == 0 {
		return nil
	}
	if values, ok := details[key].([]string); ok {
		return values
	}
	items, ok := details[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(anyString(item))
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func normalizeEvidenceKind(raw string) string {
	kind := relationships.EvidenceKind(strings.TrimSpace(raw))
	if kind == "" {
		return ""
	}
	if mapped, ok := evidenceKindToType[kind]; ok {
		return mapped
	}
	return strings.ToLower(string(kind))
}

func anyString(value any) string {
	text, _ := value.(string)
	return text
}
