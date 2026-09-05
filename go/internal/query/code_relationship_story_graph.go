// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

// relationshipStoryRelationships resolves the relationship rows for a request,
// fanning out over relationship_types when more than one type is requested. Each
// type reuses the same bounded single-type query path and the rows are merged in
// requested-type order, so the additive multi-type filter introduces no new
// graph query shape.
func (h *CodeHandler) relationshipStoryRelationships(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, string, TruthBasis, error) {
	types, err := req.normalizedRelationshipTypes()
	if err != nil {
		return nil, "", "", err
	}
	if len(types) > 1 && !req.graphAnchorPropertyResolved && h != nil && h.Neo4j != nil && h.graphBackend() == GraphBackendNornicDB &&
		!req.IncludeTransitive && nornicDBRelationshipStoryAnchorPreflightSupported(req, entity) {
		resolvedReq, err := h.resolveNornicDBRelationshipStoryAnchorProperty(ctx, req, entity)
		if err != nil {
			return nil, "", "", err
		}
		req = resolvedReq
	}
	if len(types) <= 1 {
		return h.relationshipStoryRelationshipsForType(ctx, req, entity)
	}
	var (
		backend string
		basis   TruthBasis
		merged  []map[string]any
	)
	for _, relationshipType := range types {
		sub := req
		sub.RelationshipType = relationshipType
		sub.RelationshipTypes = nil
		rows, rowsBackend, rowsBasis, rowsErr := h.relationshipStoryRelationshipsForType(ctx, sub, entity)
		if rowsErr != nil {
			return nil, "", "", rowsErr
		}
		backend = rowsBackend
		basis = rowsBasis
		merged = append(merged, rows...)
	}
	return merged, backend, basis, nil
}

func (h *CodeHandler) relationshipStoryRelationshipsForType(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, string, TruthBasis, error) {
	if h != nil && h.Neo4j != nil {
		rows, err := h.relationshipStoryGraphRows(ctx, req, entity)
		if err != nil {
			return nil, "", "", err
		}
		return rows, "graph", TruthBasisAuthoritativeGraph, nil
	}
	if h != nil && h.Content != nil && entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		row, err := h.relationshipsFromEntity(ctx, *entity)
		if err != nil {
			return nil, "", "", err
		}
		return relationshipStoryContentRows(row, req), "postgres_content_store", TruthBasisContentIndex, nil
	}
	return nil, "", "", errSymbolBackendUnavailable
}

func (h *CodeHandler) relationshipStoryGraphRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	if req.IncludeTransitive {
		return h.relationshipStoryTransitiveGraphRows(ctx, req, entity)
	}
	direction, _ := req.normalizedDirection()
	if direction != "both" {
		return h.relationshipStoryGraphRowsForDirection(ctx, req, entity, direction)
	}

	type directionResult struct {
		direction string
		rows      []map[string]any
		err       error
	}
	results := make(chan directionResult, 2)
	for _, current := range []string{"incoming", "outgoing"} {
		go func(direction string) {
			rows, err := h.relationshipStoryGraphRowsForDirection(ctx, req, entity, direction)
			results <- directionResult{direction: direction, rows: rows, err: err}
		}(current)
	}
	byDirection := map[string][]map[string]any{}
	for range 2 {
		result := <-results
		if result.err != nil {
			return nil, result.err
		}
		byDirection[result.direction] = result.rows
	}
	return interleaveRelationshipStoryDirections(byDirection["incoming"], byDirection["outgoing"]), nil
}

func interleaveRelationshipStoryDirections(incoming []map[string]any, outgoing []map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, len(incoming)+len(outgoing))
	for index := 0; index < len(incoming) || index < len(outgoing); index++ {
		if index < len(incoming) {
			rows = append(rows, incoming[index])
		}
		if index < len(outgoing) {
			rows = append(rows, outgoing[index])
		}
	}
	return rows
}

func (h *CodeHandler) relationshipStoryTransitiveGraphRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	direction, _ := req.normalizedDirection()
	limit := req.normalizedLimit() + 1
	rootID := strings.TrimSpace(req.EntityID)
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		rootID = strings.TrimSpace(entity.EntityID)
	}
	if rootID == "" {
		return []map[string]any{}, nil
	}

	frontier := []string{rootID}
	seen := map[string]struct{}{rootID: {}}
	rows := make([]map[string]any, 0, limit)
	for depth := 1; depth <= normalizedRelationshipStoryMaxDepth(req.MaxDepth) && len(frontier) > 0 && len(rows) < limit; depth++ {
		next := make([]string, 0)
		for _, currentID := range frontier {
			hopReq := req
			hopReq.EntityID = currentID
			hopReq.Offset = 0
			hopReq.Limit = limit - len(rows)
			hopReq.IncludeTransitive = false
			hopRows, err := h.relationshipStoryGraphRowsForDirection(
				ctx,
				hopReq,
				&EntityContent{EntityID: currentID},
				direction,
			)
			if err != nil {
				return nil, err
			}
			for _, hop := range hopRows {
				nextID := relationshipStoryNextID(hop, direction)
				if nextID == "" {
					continue
				}
				item := cloneQueryAnyMap(hop)
				item["depth"] = depth
				rows = append(rows, item)
				if _, ok := seen[nextID]; !ok {
					seen[nextID] = struct{}{}
					next = append(next, nextID)
				}
				if len(rows) >= limit {
					break
				}
			}
			if len(rows) >= limit {
				break
			}
		}
		frontier = next
	}
	return rows, nil
}

func relationshipStoryNextID(row map[string]any, direction string) string {
	if direction == "incoming" {
		return StringVal(row, "source_id")
	}
	return StringVal(row, "target_id")
}

func (h *CodeHandler) relationshipStoryGraphRowsForDirection(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
) ([]map[string]any, error) {
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipStoryGraphRows(ctx, req, entity, direction)
	}
	cypher, params := relationshipStoryGraphCypher(
		req,
		entity,
		direction,
		graphEntityIDPredicate,
		codeGrantAccessFilter(ctx),
	)
	return h.Neo4j.Run(ctx, cypher, params)
}

func relationshipStoryGraphCypher(
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
	predicate func(string, string) string,
	access repositoryAccessFilter,
) (string, map[string]any) {
	relationshipType, _ := req.normalizedRelationshipType()
	params := map[string]any{
		"entity_id": strings.TrimSpace(req.EntityID),
		"limit":     req.normalizedLimit() + 1,
		"offset":    req.Offset,
	}
	params = relationshipStoryAccessParams(req, access, params)
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		params["entity_id"] = strings.TrimSpace(entity.EntityID)
	}
	relPattern := ":" + relationshipType
	if direction == "incoming" {
		predicates := []string{predicate("target", "$entity_id")}
		predicates = append(predicates, relationshipStoryRepoPredicates(req, access, "source", "target", "target")...)
		return `
		MATCH (source)-[rel` + relPattern + `]->(target)
		WHERE ` + strings.Join(predicates, " AND ") + `
		OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:File)<-[:REPO_CONTAINS]-(sourceRepo:Repository)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)<-[:REPO_CONTAINS]-(targetRepo:Repository)
		RETURN 'incoming' as direction,
		       type(rel) as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       sourceRepo.id as source_repo_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       coalesce(source.language, sourceFile.language) as source_language,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name,
		       targetRepo.id as target_repo_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       coalesce(target.language, targetFile.language) as target_language
		ORDER BY source.name, source_id
		SKIP $offset
		LIMIT $limit
	`, params
	}
	predicates := []string{predicate("source", "$entity_id")}
	predicates = append(predicates, relationshipStoryRepoPredicates(req, access, "source", "target", "source")...)
	return `
		MATCH (source)-[rel` + relPattern + `]->(target)
		WHERE ` + strings.Join(predicates, " AND ") + `
		OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:File)<-[:REPO_CONTAINS]-(sourceRepo:Repository)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)<-[:REPO_CONTAINS]-(targetRepo:Repository)
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       sourceRepo.id as source_repo_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       coalesce(source.language, sourceFile.language) as source_language,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name,
		       targetRepo.id as target_repo_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       coalesce(target.language, targetFile.language) as target_language
		ORDER BY target.name, target_id
		SKIP $offset
		LIMIT $limit
	`, params
}

// relationshipStoryAccessParams binds the parameters the story reads' repository
// predicates reference: the caller-supplied repo_id, and the grant arrays
// querycontract.RepositoryAccessFilter renders its condition against.
func relationshipStoryAccessParams(
	req relationshipStoryRequest,
	access repositoryAccessFilter,
	params map[string]any,
) map[string]any {
	if strings.TrimSpace(req.RepoID) != "" {
		params["repo_id"] = strings.TrimSpace(req.RepoID)
	}
	if access.Scoped() {
		params = access.GraphParams(params)
	}
	return params
}

// relationshipStoryRepoPredicates returns the predicates that decide which
// relationship rows the caller may see, written against the ENTITY nodes' own
// repo_id rather than against the Repository aliases the projection binds.
//
// The alias choice is the whole fix, and it is a measured one (#5167 batch 2b,
// docs/internal/evidence/5167-code-family-batch-2b.md). Both story builders
// bind sourceRepo/targetRepo through OPTIONAL MATCH clauses, and a WHERE that
// follows an OPTIONAL MATCH constrains the optional pattern rather than the
// driving row set -- so the predicates this function used to emit on those
// aliases dropped no row at all. They only nulled the last optional pattern's
// variables, and normalizeNornicDBRelationshipStoryRows then filled the
// repository column back in from the node property, so the out-of-grant
// repository id reached the response anyway.
//
// repo_id, the property the canonical node writer sets on every entity it
// projects (canonicalEntityProperties in internal/storage/cypher), is available
// on the driving row itself, so the predicate can sit in the anchoring MATCH's
// own WHERE and the OPTIONAL MATCH clauses can stay optional and stay purely
// projection. An entity the graph cannot attribute to a repository has no
// repo_id, so it fails the predicate and is dropped -- fail-closed, matching
// what batch 1 landed for complexityListAnchor.
//
// anchorAlias is the endpoint the request itself anchors on. A cross_repo story
// deliberately reaches out of its repository, so repo_id constrains only that
// end; the grant still constrains both, because a caller may not read an
// out-of-grant neighbour's identity even on a cross-repository question.
func relationshipStoryRepoPredicates(
	req relationshipStoryRequest,
	access repositoryAccessFilter,
	sourceAlias string,
	targetAlias string,
	anchorAlias string,
) []string {
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(req.RepoID) != "" {
		if req.CrossRepo {
			predicates = append(predicates, anchorAlias+".repo_id = $repo_id")
		} else {
			predicates = append(predicates, sourceAlias+".repo_id = $repo_id")
			predicates = append(predicates, targetAlias+".repo_id = $repo_id")
		}
	}
	return append(predicates, relationshipStoryGrantPredicates(access, sourceAlias, targetAlias)...)
}

// relationshipStoryGrantPredicates returns the caller's grant condition on each
// alias's repo_id, and nothing at all for an unscoped caller.
//
// The class-hierarchy and override reads use this rather than the fuller
// relationshipStoryRepoPredicates above: they never carried a repo_id predicate,
// so adding one would change what a shared-key caller reads, while the grant is
// what those reads are missing.
func relationshipStoryGrantPredicates(access repositoryAccessFilter, aliases ...string) []string {
	if !access.Scoped() {
		return nil
	}
	predicates := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		predicates = append(predicates, access.GraphConditionOnProperty(alias, "repo_id"))
	}
	return predicates
}

func relationshipStoryContentRows(row map[string]any, req relationshipStoryRequest) []map[string]any {
	relationshipType, _ := req.normalizedRelationshipType()
	direction, _ := req.normalizedDirection()
	rows := make([]map[string]any, 0)
	if direction != "outgoing" {
		rows = append(rows, filterRelationships(mapRelationships(row["incoming"]), relationshipType)...)
	}
	if direction != "incoming" {
		rows = append(rows, filterRelationships(mapRelationships(row["outgoing"]), relationshipType)...)
	}
	limit := req.normalizedLimit() + 1
	if len(rows) > req.Offset {
		rows = rows[req.Offset:]
	} else {
		rows = []map[string]any{}
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}
