// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// entity_content_types.go holds the entity type vocabularies and the
// content-store entity lookup helpers extracted from entity.go to keep that
// file under the 500-line cap.

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const contentEntityIDPrefix = "content-entity:"

var resolverOnlyGraphEntityTypes = map[string]string{
	"workload": "Workload",
}

// graphResolvableNotLanguageQueryableEntityTypes maps entity types that
// resolve_entity filters by graph label but that are deliberately excluded from
// the language-query entity_type enum: their entities carry language "yaml",
// which supportedLanguages does not accept, so no language/entity_type pair
// could ever return rows. They are NOT in resolverOnlyGraphEntityTypes because
// these types ARE content-backed — that map's resolveEntityFromContent
// short-circuit would break their content fallback. Keep each entry paired with
// a resolveContentBackedEntityTypes entry (entity_content_types_atlantis_test.go).
var graphResolvableNotLanguageQueryableEntityTypes = map[string]string{
	"atlantis_project":  "AtlantisProject",
	"atlantis_workflow": "AtlantisWorkflow",
}

var globalGraphOnlyEntityTypes = map[string]struct{}{
	"repository": {},
	"directory":  {},
	"file":       {},
}

func knownResolveEntityType(typeName string) bool {
	if _, ok := globalGraphOnlyEntityTypes[typeName]; ok {
		return true
	}
	_, ok := globalContentEntityType(typeName)
	return ok
}

type globalContentEntityFilter struct {
	EntityType    string
	MetadataKey   string
	MetadataValue string
}

func globalContentEntityType(typeName string) (string, bool) {
	filter, ok := globalContentEntityNameFilter(typeName)
	return filter.EntityType, ok
}

func globalContentEntityNameFilter(typeName string) (globalContentEntityFilter, bool) {
	if _, graphOnly := globalGraphOnlyEntityTypes[typeName]; graphOnly {
		return globalContentEntityFilter{}, false
	}
	if semanticType, ok := elixirSemanticEntityTypes[typeName]; ok {
		return globalContentEntityFilter{
			EntityType: semanticType.baseType, MetadataKey: semanticType.metadataKey, MetadataValue: semanticType.metadataValue,
		}, true
	}
	if entityType, ok := resolveContentBackedEntityTypes[typeName]; ok {
		return globalContentEntityFilter{EntityType: entityType}, true
	}
	if entityType, ok := contentBackedEntityTypes[typeName]; ok {
		return globalContentEntityFilter{EntityType: entityType}, true
	}
	if entityType, ok := graphBackedEntityTypes[typeName]; ok {
		return globalContentEntityFilter{EntityType: entityType}, true
	}
	if entityType, ok := graphFirstContentBackedEntityTypes[typeName]; ok {
		return globalContentEntityFilter{EntityType: entityType}, true
	}
	return globalContentEntityFilter{}, false
}

func (h *EntityHandler) resolveGlobalContentEntities(ctx context.Context, name, typeName string, limit int) ([]map[string]any, error) {
	searcher, ok := h.Content.(EntityNameSearcher)
	if !ok {
		return nil, errEntityNameSearchUnavailable
	}
	filter, ok := globalContentEntityNameFilter(typeName)
	if !ok {
		return nil, fmt.Errorf("unsupported global entity type %q", typeName)
	}
	access := repositoryAccessFilterFromContext(ctx)
	search := EntityNameSearch{
		Name: name, Match: EntityNameMatchExact, Scope: EntityNameScopeAll,
		EntityType: filter.EntityType, MetadataKey: filter.MetadataKey, MetadataValue: filter.MetadataValue, Limit: limit,
	}
	if access.scoped() {
		search.Scope = EntityNameScopeRepositories
		search.RepositoryIDs = access.repositorySearchIDs()
	}
	rows, err := searcher.SearchEntityNames(ctx, search)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		results = append(results, contentEntityToMap(row))
	}
	return results, nil
}

func (h *EntityHandler) writeCanonicalContentEntityResolution(
	w http.ResponseWriter,
	r *http.Request,
	req resolveEntityRequest,
	limit int,
) bool {
	entities, handled, err := h.resolveCanonicalContentEntityID(
		r.Context(),
		req.Name,
		req.Type,
		req.RepoID,
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve canonical content entity: %v", err))
		return true
	}
	if !handled {
		return false
	}
	graphHydrated, err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, entities)
	if err != nil {
		if WriteGraphReadError(w, r, err, "code_search.exact_symbol") {
			return true
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return true
	}
	entities = normalizeResolvedEntities(entities, limit+1)
	entities, truncated := trimResolvedEntityPage(entities, limit)
	if entities == nil {
		entities = []map[string]any{}
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		resolvedEntityResponse(entities, limit, truncated),
		canonicalContentEntityResolveTruthEnvelope(h.profile(), graphHydrated),
	)
	return true
}

func (h *EntityHandler) resolveCanonicalContentEntityID(
	ctx context.Context,
	name string,
	typeName string,
	repoID string,
) ([]map[string]any, bool, error) {
	entityID := strings.TrimSpace(name)
	if h == nil || h.Content == nil || !strings.HasPrefix(entityID, contentEntityIDPrefix) {
		return nil, false, nil
	}
	entity, err := h.Content.GetEntityContent(ctx, entityID)
	if err != nil {
		return nil, true, err
	}
	if entity == nil {
		return []map[string]any{}, true, nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() || !access.allowsRepositoryID(entity.RepoID) {
		return []map[string]any{}, true, nil
	}
	if repoID != "" && entity.RepoID != repoID {
		return []map[string]any{}, true, nil
	}
	wantType := contentEntityTypeForResolve(strings.ToLower(strings.TrimSpace(typeName)))
	if wantType != "" && !strings.EqualFold(entity.EntityType, wantType) {
		return []map[string]any{}, true, nil
	}
	return []map[string]any{contentEntityToMap(*entity)}, true, nil
}

func (h *EntityHandler) resolveEntityFromContent(
	ctx context.Context,
	name string,
	typeName string,
	repoID string,
	limit int,
) ([]map[string]any, error) {
	if _, graphOnly := resolverOnlyGraphEntityTypes[strings.ToLower(strings.TrimSpace(typeName))]; graphOnly {
		return []map[string]any{}, nil
	}
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
	if graphLabel, ok := resolverOnlyGraphEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	if graphLabel, ok := graphFirstContentBackedEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	if graphLabel, ok := graphResolvableNotLanguageQueryableEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	return "", "", "", false
}

var resolveContentBackedEntityTypes = map[string]string{
	"analytics_model":          "AnalyticsModel",
	"annotation":               "Annotation",
	"argocd_application":       "ArgoCDApplication",
	"argocd_applicationset":    "ArgoCDApplicationSet",
	"atlantis_project":         "AtlantisProject",
	"atlantis_workflow":        "AtlantisWorkflow",
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
	"variable":                 "Variable",
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
		"repo_name":  entity.RepoName,
		"language":   entity.Language,
		"start_line": entity.StartLine,
		"end_line":   entity.EndLine,
		"metadata":   entity.Metadata,
	}
	attachSemanticSummary(result)
	return result
}
