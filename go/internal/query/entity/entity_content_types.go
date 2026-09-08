// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// entity_content_types.go holds the entity type vocabularies and the
// content-store entity lookup helpers extracted from entity.go to keep that
// file under the 500-line cap.

package entity

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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

// GlobalContentEntityFilter is the content-index name filter for one global entity type. Exported for the staying name-search test; see #6060.
type GlobalContentEntityFilter struct {
	EntityType    string
	MetadataKey   string
	MetadataValue string
}

func globalContentEntityType(typeName string) (string, bool) {
	filter, ok := GlobalContentEntityNameFilter(typeName)
	return filter.EntityType, ok
}

// GlobalContentEntityNameFilter maps a global entity type name to its content-index filter. Exported for the staying name-search test; see #6060.
func GlobalContentEntityNameFilter(typeName string) (GlobalContentEntityFilter, bool) {
	if _, graphOnly := globalGraphOnlyEntityTypes[typeName]; graphOnly {
		return GlobalContentEntityFilter{}, false
	}
	if semanticType, ok := querycontract.ElixirSemanticEntityTypes[typeName]; ok {
		return GlobalContentEntityFilter{
			EntityType: semanticType.BaseType, MetadataKey: semanticType.MetadataKey, MetadataValue: semanticType.MetadataValue,
		}, true
	}
	if entityType, ok := resolveContentBackedEntityTypes[typeName]; ok {
		return GlobalContentEntityFilter{EntityType: entityType}, true
	}
	if entityType, ok := querycontract.ContentBackedEntityTypes[typeName]; ok {
		return GlobalContentEntityFilter{EntityType: entityType}, true
	}
	if entityType, ok := querycontract.GraphBackedEntityTypes[typeName]; ok {
		return GlobalContentEntityFilter{EntityType: entityType}, true
	}
	if entityType, ok := querycontract.GraphFirstContentBackedEntityTypes[typeName]; ok {
		return GlobalContentEntityFilter{EntityType: entityType}, true
	}
	return GlobalContentEntityFilter{}, false
}

func (h *EntityHandler) resolveGlobalContentEntities(ctx context.Context, name, typeName string, limit int) ([]map[string]any, error) {
	searcher, ok := h.Content.(querycontract.EntityNameSearcher)
	if !ok {
		return nil, querycontract.ErrEntityNameSearchUnavailable
	}
	filter, ok := GlobalContentEntityNameFilter(typeName)
	if !ok {
		return nil, fmt.Errorf("unsupported global entity type %q", typeName)
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	search := querycontract.EntityNameSearch{
		Name: name, Match: querycontract.EntityNameMatchExact, Scope: querycontract.EntityNameScopeAll,
		EntityType: filter.EntityType, MetadataKey: filter.MetadataKey, MetadataValue: filter.MetadataValue, Limit: limit,
	}
	if access.Scoped() {
		search.Scope = querycontract.EntityNameScopeRepositories
		search.RepositoryIDs = access.RepositorySearchIDs()
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
	req ResolveEntityRequest,
	limit int,
) bool {
	entities, handled, err := h.resolveCanonicalContentEntityID(
		r.Context(),
		req.Name,
		req.Type,
		req.RepoID,
	)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve canonical content entity: %v", err))
		return true
	}
	if !handled {
		return false
	}
	graphHydrated, err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, entities)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "code_search.exact_symbol") {
			return true
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return true
	}
	entities = normalizeResolvedEntities(entities, limit+1)
	entities, truncated := trimResolvedEntityPage(entities, limit)
	if entities == nil {
		entities = []map[string]any{}
	}
	querycontract.WriteSuccess(
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
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if access.Empty() || !access.AllowsRepositoryID(entity.RepoID) {
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

	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if access.Empty() || (repoID != "" && !access.AllowsRepositoryID(repoID)) {
		return []map[string]any{}, nil
	}
	entityType := contentEntityTypeForResolve(typeName)
	var (
		rows []querycontract.EntityContent
		err  error
	)
	if repoID != "" {
		rows, err = h.Content.SearchEntitiesByName(ctx, repoID, entityType, name, limit)
		if err != nil {
			return nil, err
		}
	} else if access.Scoped() {
		for _, allowedRepoID := range access.RepositorySearchIDs() {
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

// contentEntityTypeForResolve forwards to
// querycontract.ContentEntityTypeForResolve. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func contentEntityTypeForResolve(typeName string) string {
	return querycontract.ContentEntityTypeForResolve(typeName)
}

// ResolveGraphEntityType maps a user-facing entity type to its graph label
// plus semantic key/value. Exported for the staying root live comparison
// test via the root forwarder; the canonical implementation stays
// unexported beside the other content-type maps.
func ResolveGraphEntityType(typeName string) (string, string, string, bool) {
	return resolveGraphEntityType(typeName)
}

func resolveGraphEntityType(typeName string) (string, string, string, bool) {
	if graphLabel, semanticKey, semanticValue, ok := querycontract.ElixirGraphSemanticEntityType(typeName); ok {
		return graphLabel, semanticKey, semanticValue, true
	}
	if graphLabel, ok := querycontract.GraphBackedEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	if graphLabel, ok := resolverOnlyGraphEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	if graphLabel, ok := querycontract.GraphFirstContentBackedEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	if graphLabel, ok := graphResolvableNotLanguageQueryableEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	return "", "", "", false
}

// resolveContentBackedEntityTypes forwards to
// querycontract.ResolveContentBackedEntityTypes. The implementation moved to
// querycontract for #6060; this alias keeps root callers unchanged.
var resolveContentBackedEntityTypes = querycontract.ResolveContentBackedEntityTypes

func contentEntityToMap(entity querycontract.EntityContent) map[string]any {
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
