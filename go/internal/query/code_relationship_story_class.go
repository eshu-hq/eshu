package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

func (h *CodeHandler) handleRepoScopedOverrideStory(
	w http.ResponseWriter,
	r *http.Request,
	req relationshipStoryRequest,
) {
	if strings.TrimSpace(req.RepoID) == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required for repo-scoped overrides")
		return
	}
	rows, sourceBackend, basis, err := h.relationshipStoryOverrideRows(r.Context(), req)
	if err != nil {
		if err == errSymbolBackendUnavailable {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resolution := relationshipStoryResolution{
		Status:   "repo_scoped",
		RepoID:   strings.TrimSpace(req.RepoID),
		Language: strings.TrimSpace(req.Language),
	}
	data := relationshipStoryData(req, resolution, rows)
	data["source_backend"] = sourceBackend
	data["override_story"] = relationshipStoryOverrideData(req, rows)
	markRelationshipStoryRepoOverrideCoverage(data)
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), relationshipStoryCapability, basis, "resolved from bounded override story lookup"),
	)
}

func (h *CodeHandler) relationshipStoryClassHierarchy(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	relationships []map[string]any,
) (map[string]any, error) {
	var methods []map[string]any
	var ancestorDepthRows []map[string]any
	var descendantDepthRows []map[string]any
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		rows, err := h.relationshipStoryClassMethods(ctx, req, entity)
		if err != nil {
			errs <- err
			return
		}
		methods = rows
	}()
	go func() {
		defer wg.Done()
		rows, err := h.relationshipStoryInheritanceDepthRows(ctx, req, entity, "outgoing")
		if err != nil {
			errs <- err
			return
		}
		ancestorDepthRows = rows
	}()
	go func() {
		defer wg.Done()
		rows, err := h.relationshipStoryInheritanceDepthRows(ctx, req, entity, "incoming")
		if err != nil {
			errs <- err
			return
		}
		descendantDepthRows = rows
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return nil, err
		}
	}

	parents, children := splitClassHierarchyRelationships(relationships)
	return map[string]any{
		"methods":           relationshipStoryMethodRowsWithHandles(methods, req.normalizedLimit()),
		"methods_truncated": len(methods) > req.normalizedLimit(),
		"parents":           relationshipStoryRowsWithHandles(limitRelationshipStoryRows(parents, req.normalizedLimit())),
		"children":          relationshipStoryRowsWithHandles(limitRelationshipStoryRows(children, req.normalizedLimit())),
		"depth_summary":     relationshipStoryDepthSummary(ancestorDepthRows, descendantDepthRows, req.normalizedLimit()),
	}, nil
}

func splitClassHierarchyRelationships(rows []map[string]any) ([]map[string]any, []map[string]any) {
	parents := make([]map[string]any, 0)
	children := make([]map[string]any, 0)
	for _, row := range rows {
		if !strings.EqualFold(StringVal(row, "type"), "INHERITS") {
			continue
		}
		switch StringVal(row, "direction") {
		case "outgoing":
			parents = append(parents, row)
		case "incoming":
			children = append(children, row)
		}
	}
	return parents, children
}

func (h *CodeHandler) relationshipStoryClassMethods(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return []map[string]any{}, nil
	}
	entityID := relationshipStoryEntityID(req, entity)
	if entityID == "" {
		return []map[string]any{}, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipStoryClassMethods(ctx, req, entityID)
	}
	cypher, params := relationshipStoryClassMethodsCypher(req, entityID, graphEntityIDPredicate)
	return h.Neo4j.Run(ctx, cypher, params)
}

func relationshipStoryClassMethodsCypher(
	req relationshipStoryRequest,
	entityID string,
	predicate func(string, string) string,
) (string, map[string]any) {
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.normalizedLimit() + 1,
		"offset":    req.Offset,
	}
	return `
		MATCH (class)-[:CONTAINS]->(method:Function)
		WHERE ` + predicate("class", "$entity_id") + `
		RETURN coalesce(method.id, method.uid) as method_id,
		       method.name as method_name,
		       method.path as file_path,
		       method.start_line as start_line,
		       method.end_line as end_line
		ORDER BY method.name, method_id
		SKIP $offset
		LIMIT $limit
	`, params
}

func (h *CodeHandler) relationshipStoryInheritanceDepthRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return []map[string]any{}, nil
	}
	entityID := relationshipStoryEntityID(req, entity)
	if entityID == "" {
		return []map[string]any{}, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipStoryInheritanceDepthRows(ctx, req, entityID, direction)
	}
	cypher, params := relationshipStoryInheritanceDepthCypher(req, entityID, direction, graphEntityIDPredicate)
	return h.Neo4j.Run(ctx, cypher, params)
}

func relationshipStoryInheritanceDepthCypher(
	req relationshipStoryRequest,
	entityID string,
	direction string,
	predicate func(string, string) string,
) (string, map[string]any) {
	maxDepth := normalizedRelationshipStoryMaxDepth(req.MaxDepth)
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.normalizedLimit() + 1,
	}
	if direction == "incoming" {
		return fmt.Sprintf(`
		MATCH path = (source:Class)-[:INHERITS*1..%d]->(target:Class)
		WHERE %s
		RETURN 'incoming' as direction,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name,
		       length(path) as depth
		ORDER BY depth DESC, source.name, source_id
		LIMIT $limit
	`, maxDepth, predicate("target", "$entity_id")), params
	}
	return fmt.Sprintf(`
		MATCH path = (source:Class)-[:INHERITS*1..%d]->(target:Class)
		WHERE %s
		RETURN 'outgoing' as direction,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name,
		       length(path) as depth
		ORDER BY depth DESC, target.name, target_id
		LIMIT $limit
	`, maxDepth, predicate("source", "$entity_id")), params
}

func relationshipStoryEntityID(req relationshipStoryRequest, entity *EntityContent) string {
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		return strings.TrimSpace(entity.EntityID)
	}
	return strings.TrimSpace(req.EntityID)
}

func relationshipStoryMethodRowsWithHandles(rows []map[string]any, limit int) []map[string]any {
	rows = limitRelationshipStoryRows(rows, limit)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := cloneQueryAnyMap(row)
		if methodID := StringVal(item, "method_id"); methodID != "" {
			item["method_handle"] = "entity:" + methodID
		}
		out = append(out, item)
	}
	return out
}

func relationshipStoryDepthSummary(
	ancestors []map[string]any,
	descendants []map[string]any,
	limit int,
) map[string]any {
	return map[string]any{
		"max_parent_depth": maxRelationshipStoryDepth(ancestors),
		"max_child_depth":  maxRelationshipStoryDepth(descendants),
		"parent_truncated": len(ancestors) > limit,
		"child_truncated":  len(descendants) > limit,
	}
}

func maxRelationshipStoryDepth(rows []map[string]any) int {
	maxDepth := 0
	for _, row := range rows {
		if depth := IntVal(row, "depth"); depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth
}

func relationshipStoryOverrideData(req relationshipStoryRequest, rows []map[string]any) map[string]any {
	limit := req.normalizedLimit()
	overrides := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(StringVal(row, "type"), "OVERRIDES") {
			overrides = append(overrides, row)
		}
	}
	return map[string]any{
		"overrides": relationshipStoryRowsWithHandles(limitRelationshipStoryRows(overrides, limit)),
		"truncated": len(overrides) > limit,
	}
}

func limitRelationshipStoryRows(rows []map[string]any, limit int) []map[string]any {
	if limit <= 0 {
		return []map[string]any{}
	}
	if len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

func (h *CodeHandler) relationshipStoryOverrideRows(
	ctx context.Context,
	req relationshipStoryRequest,
) ([]map[string]any, string, TruthBasis, error) {
	if h == nil || h.Neo4j == nil {
		return nil, "", "", errSymbolBackendUnavailable
	}
	cypher, params := relationshipStoryOverrideRowsCypher(req)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, "", "", err
	}
	return rows, "graph", TruthBasisAuthoritativeGraph, nil
}

func relationshipStoryOverrideRowsCypher(req relationshipStoryRequest) (string, map[string]any) {
	params := map[string]any{
		"repo_id":         strings.TrimSpace(req.RepoID),
		"limit":           req.normalizedLimit() + 1,
		"offset":          req.Offset,
		"override_labels": relationshipStoryOverrideNodeLabels(),
	}
	languagePredicate := ""
	if language := strings.TrimSpace(req.Language); language != "" {
		params["language"] = language
		languagePredicate = `
		  AND source.language = $language
		  AND target.language = $language`
	}
	return `
		MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(file:File)-[:CONTAINS]->(source)-[rel:OVERRIDES]->(target)
		WHERE any(label IN labels(source) WHERE label IN $override_labels)
		  AND any(label IN labels(target) WHERE label IN $override_labels)` + languagePredicate + `
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       rel.reason as reason,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       labels(source) as source_labels,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name,
		       labels(target) as target_labels,
		       file.relative_path as file_path
		ORDER BY source.name, target.name, source_id, target_id
		SKIP $offset
		LIMIT $limit
	`, params
}

func relationshipStoryOverrideNodeLabels() []string {
	return []string{"Function", "Class", "Interface", "Trait", "Struct", "Enum", "Protocol"}
}

func markRelationshipStoryRepoOverrideCoverage(data map[string]any) {
	if coverage, ok := data["coverage"].(map[string]any); ok {
		coverage["query_shape"] = "repo_anchor_override_story"
		coverage["relationship_types"] = []string{"OVERRIDES"}
	}
}
