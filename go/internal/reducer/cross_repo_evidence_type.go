// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	relationships.EvidenceKindFluxGitRepositorySource:              "flux_git_repository_source",
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
	relationships.EvidenceKindPuppetModuleReference:                "puppet_module_reference",
	relationships.EvidenceKindChefCookbookDependency:               "chef_cookbook_dependency",
	relationships.EvidenceKindSaltFormulaReference:                 "salt_formula_reference",
	relationships.EvidenceKindGCPCloudRelationship:                 "gcp_cloud_relationship",
	relationships.EvidenceKindHelmTemplateValueReference:           "helm_template_value_reference",
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

// sourceToolUnknown is the explicit token stamped when a Tier-2 edge carries an
// evidence kind that is not classified into a tool. It is preferred over an
// absent value so a coverage gap is visible in the graph (and fails the #4002
// drift gate) rather than silently passing.
const sourceToolUnknown = "unknown"

// evidenceKindToSourceTool collapses each persisted EvidenceKind to its canonical
// lowercase source_tool token, per
// docs/public/reference/edge-source-tool-provenance.md (#3998). Unlike
// evidenceKindToType (which keeps each sub-kind distinct, e.g. terraform_app_repo
// vs terraform_config_path), this map folds a tool's whole family to one token
// (terraform). It is additive: EvidenceKind constants are persisted and never
// renamed; source_tool is derived from them at write time.
var evidenceKindToSourceTool = map[relationships.EvidenceKind]string{
	relationships.EvidenceKindTerraformAppRepo:       "terraform",
	relationships.EvidenceKindTerraformAppName:       "terraform",
	relationships.EvidenceKindTerraformGitHubRepo:    "terraform",
	relationships.EvidenceKindTerraformGitHubActions: "terraform",
	relationships.EvidenceKindTerraformConfigPath:    "terraform",
	relationships.EvidenceKindTerraformIAMPermission: "terraform",
	// TERRAFORM_MODULE_SOURCE is shared by Terraform and Terragrunt module
	// sources (models.go); the kind alone cannot distinguish them, so it defaults
	// to terraform (#3998 note 1). Splitting it needs a second discriminator,
	// tracked as a precision follow-up.
	relationships.EvidenceKindTerraformModuleSource:                "terraform",
	relationships.EvidenceKindTerragruntDependencyConfigPath:       "terragrunt",
	relationships.EvidenceKindTerragruntConfigAssetPath:            "terragrunt",
	relationships.EvidenceKindHelmChart:                            "helm",
	relationships.EvidenceKindHelmValues:                           "helm",
	relationships.EvidenceKindKustomizeResource:                    "kustomize",
	relationships.EvidenceKindKustomizeHelmChart:                   "kustomize",
	relationships.EvidenceKindKustomizeImage:                       "kustomize",
	relationships.EvidenceKindArgoCDAppSource:                      "argocd",
	relationships.EvidenceKindFluxGitRepositorySource:              "flux",
	relationships.EvidenceKindArgoCDApplicationSetDiscovery:        "argocd",
	relationships.EvidenceKindArgoCDApplicationSetDeploySource:     "argocd",
	relationships.EvidenceKindArgoCDDestinationPlatform:            "argocd",
	relationships.EvidenceKindGitHubActionsReusableWorkflow:        "github_actions",
	relationships.EvidenceKindGitHubActionsLocalReusableWorkflow:   "github_actions",
	relationships.EvidenceKindGitHubActionsCheckoutRepository:      "github_actions",
	relationships.EvidenceKindGitHubActionsWorkflowInputRepository: "github_actions",
	relationships.EvidenceKindGitHubActionsActionRepository:        "github_actions",
	relationships.EvidenceKindJenkinsSharedLibrary:                 "jenkins",
	relationships.EvidenceKindJenkinsGitHubRepository:              "jenkins",
	relationships.EvidenceKindDockerComposeBuildContext:            "docker_compose",
	relationships.EvidenceKindDockerComposeImage:                   "docker_compose",
	relationships.EvidenceKindDockerComposeDependsOn:               "docker_compose",
	relationships.EvidenceKindDockerfileSourceLabel:                "docker",
	relationships.EvidenceKindAnsibleRoleReference:                 "ansible",
	relationships.EvidenceKindPuppetModuleReference:                "puppet",
	relationships.EvidenceKindChefCookbookDependency:               "chef",
	relationships.EvidenceKindSaltFormulaReference:                 "salt",
	relationships.EvidenceKindGCPCloudRelationship:                 "gcp",
}

// sourceToolPrefixFallback classifies generated/runtime EvidenceKinds that are
// not named constants by their family prefix. The Terraform schema extractor
// synthesizes per-resource kinds at runtime (terraform_schema.go:
// "TERRAFORM_"+resourceType, e.g. TERRAFORM_ECS_SERVICE, TERRAFORM_WAFV2_WEB_ACL,
// TERRAFORM_PAGERDUTY_SERVICE) that the named-constant map cannot enumerate, so a
// valid Terraform cross-repo edge would otherwise fall through to "unknown".
// Prefixes are full family tokens (DOCKER_COMPOSE_ vs DOCKERFILE_) so none nests
// inside another, and TERRAGRUNT_ is a distinct string from TERRAFORM_ — the
// named-constant map is consulted first, so the Terragrunt constants keep their
// terragrunt token and only non-constant kinds reach this fallback.
var sourceToolPrefixFallback = []struct {
	prefix string
	tool   string
}{
	{"TERRAGRUNT_", "terragrunt"},
	{"TERRAFORM_", "terraform"},
	{"KUSTOMIZE_", "kustomize"},
	{"HELM_", "helm"},
	{"ARGOCD_", "argocd"},
	{"GITHUB_ACTIONS_", "github_actions"},
	{"JENKINS_", "jenkins"},
	{"DOCKER_COMPOSE_", "docker_compose"},
	{"DOCKERFILE_", "docker"},
	{"ANSIBLE_", "ansible"},
	{"PUPPET_", "puppet"},
	{"CHEF_", "chef"},
	{"GCP_", "gcp"},
	// CONTAINER_IMAGE_IDENTITY_ is not a relationships-package EvidenceKind
	// family at all -- it is the golden-corpus-gate evidence_kinds token
	// go/internal/storage/cypher/provenance_edge_writer.go stamps on BUILT_FROM
	// edges (issue #5457). sourceToolForEvidenceKind is reused generically by
	// the snapshot source_tool-consistency check
	// (cross_repo_source_tool_snapshot_test.go) for ANY narrowed required
	// correlation, not only cross-repo resolver edges, so registering the
	// prefix here is what lets that check derive "oci" for rc-165 statically.
	{"CONTAINER_IMAGE_IDENTITY_", "oci"},
}

// sourceToolForEvidenceKind returns the canonical source_tool token for a single
// EvidenceKind string. It consults the named-constant map first, then the
// family-prefix fallback for generated/runtime kinds, and finally returns "" when
// the kind is unknown/unmapped so the caller can choose between an absent value
// and the explicit sourceToolUnknown token.
func sourceToolForEvidenceKind(kind string) string {
	trimmed := strings.TrimSpace(kind)
	if mapped, ok := evidenceKindToSourceTool[relationships.EvidenceKind(trimmed)]; ok {
		return mapped
	}
	upper := strings.ToUpper(trimmed)
	for _, fam := range sourceToolPrefixFallback {
		if strings.HasPrefix(upper, fam.prefix) {
			return fam.tool
		}
	}
	return ""
}

// resolvedRelationshipSourceTool derives the normalized source_tool for a
// resolved Tier-2 edge. It selects the same primary evidence kind as
// resolvedRelationshipEvidenceType (the preview kind first, then the first
// evidence_kinds entry) so source_tool and evidence_type always describe the
// same evidence. A present-but-unmapped primary kind yields the explicit
// sourceToolUnknown token; an edge with no evidence kind at all yields "" and is
// not stamped.
func resolvedRelationshipSourceTool(r relationships.ResolvedRelationship) string {
	primary := firstEvidenceKindFromPreview(r.Details)
	if primary == "" {
		if kinds := stringSliceDetail(r.Details, "evidence_kinds"); len(kinds) > 0 {
			primary = kinds[0]
		}
	}
	if primary == "" {
		return ""
	}
	if tool := sourceToolForEvidenceKind(primary); tool != "" {
		return tool
	}
	return sourceToolUnknown
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
