// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// entity_content_types.go holds the entity type vocabularies and the
// content-store entity lookup helpers extracted from entity.go to keep that
// file under the 500-line cap.

package query

import "context"

func (h *EntityHandler) resolveEntityFromContent(
	ctx context.Context,
	name string,
	typeName string,
	repoID string,
	limit int,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || repoID == "" || name == "" {
		if h == nil || h.Content == nil || name == "" {
			return []map[string]any{}, nil
		}
	}

	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() || (repoID != "" && !access.allowsRepositoryID(repoID)) {
		return []map[string]any{}, nil
	}
	entityType := contentEntityTypeForResolve(typeName)
	var (
		rows []EntityContent
		err  error
	)
	if repoID != "" {
		rows, err = h.Content.SearchEntitiesByName(ctx, repoID, entityType, name, limit)
		if err != nil {
			return nil, err
		}
	} else if access.scoped() {
		for _, allowedRepoID := range access.repositorySearchIDs() {
			if len(rows) >= limit {
				break
			}
			scopedRows, searchErr := h.Content.SearchEntitiesByName(ctx, allowedRepoID, entityType, name, limit-len(rows))
			if searchErr != nil {
				return nil, searchErr
			}
			rows = append(rows, scopedRows...)
		}
	} else {
		rows, err = h.Content.SearchEntitiesByNameAnyRepo(ctx, entityType, name, limit)
		if err != nil {
			return nil, err
		}
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		results = append(results, contentEntityToMap(row))
	}
	return results, nil
}

func contentEntityTypeForResolve(typeName string) string {
	if typeName == "" {
		return ""
	}
	if entityType, ok := resolveContentBackedEntityTypes[typeName]; ok {
		return entityType
	}
	if entityType, ok := contentBackedEntityTypes[typeName]; ok {
		return entityType
	}
	if entityType, ok := graphBackedEntityTypes[typeName]; ok {
		return entityType
	}
	return typeName
}

func resolveGraphEntityType(typeName string) (string, string, string, bool) {
	if graphLabel, semanticKey, semanticValue, ok := elixirGraphSemanticEntityType(typeName); ok {
		return graphLabel, semanticKey, semanticValue, true
	}
	if graphLabel, ok := graphBackedEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	if graphLabel, ok := graphFirstContentBackedEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	return "", "", "", false
}

var resolveContentBackedEntityTypes = map[string]string{
	"analytics_model":          "AnalyticsModel",
	"annotation":               "Annotation",
	"argocd_application":       "ArgoCDApplication",
	"argocd_applicationset":    "ArgoCDApplicationSet",
	"component":                "Component",
	"cloudformation_condition": "CloudFormationCondition",
	"cloudformation_export":    "CloudFormationExport",
	"cloudformation_import":    "CloudFormationImport",
	"cloudformation_output":    "CloudFormationOutput",
	"cloudformation_parameter": "CloudFormationParameter",
	"cloudformation_resource":  "CloudFormationResource",
	"data_asset":               "DataAsset",
	"impl_block":               "ImplBlock",
	"k8s_resource":             "K8sResource",
	"kustomize_overlay":        "KustomizeOverlay",
	"protocol":                 "Protocol",
	"terraform_backend":        "TerraformBackend",
	"terraform_block":          "TerraformBlock",
	"terraform_check":          "TerraformCheck",
	"terraform_import":         "TerraformImport",
	"terraform_lock_provider":  "TerraformLockProvider",
	"terraform_moved_block":    "TerraformMovedBlock",
	"terraform_removed_block":  "TerraformRemovedBlock",
	"terragrunt_dependency":    "TerragruntDependency",
	"terragrunt_input":         "TerragruntInput",
	"terragrunt_local":         "TerragruntLocal",
	"type_alias":               "TypeAlias",
	"type_annotation":          "TypeAnnotation",
	"typedef":                  "Typedef",
	"guard":                    "guard",
	"protocol_implementation":  "ProtocolImplementation",
	"module_attribute":         "module_attribute",
}

func contentEntityToMap(entity EntityContent) map[string]any {
	result := map[string]any{
		"id":         entity.EntityID,
		"entity_id":  entity.EntityID,
		"name":       entity.EntityName,
		"labels":     []string{entity.EntityType},
		"file_path":  entity.RelativePath,
		"repo_id":    entity.RepoID,
		"language":   entity.Language,
		"start_line": entity.StartLine,
		"end_line":   entity.EndLine,
		"metadata":   entity.Metadata,
	}
	attachSemanticSummary(result)
	return result
}
