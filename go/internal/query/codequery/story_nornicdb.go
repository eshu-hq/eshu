// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The NornicDB story readers live here. The pinned ones stay
// byte-identical under their queryplan source_sha256 pins and the hot
// cypher builders keep their query text intact; the pure builders they
// resolve moved to the relationships/story leaf and are reached through
// same-named forwarders in story_forwarders.go.

func (h *CodeHandler) nornicDBRelationshipStoryGraphRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
) ([]map[string]any, error) {
	entityID := strings.TrimSpace(req.EntityID)
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		entityID = strings.TrimSpace(entity.EntityID)
	}
	entityLabel := ""
	if entity != nil {
		entityLabel = nornicDBGraphLabelForContentEntityType(entity.EntityType)
	}
	access := codeGrantAccessFilter(ctx)
	properties := []string{"uid", "id"}
	if req.GraphAnchorPropertyResolved {
		if req.GraphAnchorProperty == "" {
			return []map[string]any{}, nil
		}
		properties = []string{req.GraphAnchorProperty}
	}
	for _, property := range properties {
		cypher, params := nornicDBRelationshipStoryGraphCypher(req, entityID, entityLabel, property, direction, access)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return normalizeNornicDBRelationshipStoryRows(rows), nil
		}
	}
	return []map[string]any{}, nil
}

func (h *CodeHandler) resolveNornicDBRelationshipStoryAnchorProperty(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) (relationshipStoryRequest, error) {
	if !nornicDBRelationshipStoryAnchorPreflightSupported(req, entity) {
		return req, nil
	}
	entityID := strings.TrimSpace(req.EntityID)
	entityLabel := ""
	if entity != nil {
		if strings.TrimSpace(entity.EntityID) != "" {
			entityID = strings.TrimSpace(entity.EntityID)
		}
		entityLabel = nornicDBGraphLabelForContentEntityType(entity.EntityType)
	}
	if entityID == "" || entityLabel == "" {
		return req, nil
	}
	params := map[string]any{"entity_id": entityID}
	repoScoped := strings.TrimSpace(req.RepoID) != ""
	if repoScoped {
		params["repo_id"] = strings.TrimSpace(req.RepoID)
	}
	uidRow, err := h.Neo4j.RunSingle(
		ctx,
		nornicDBRelationshipStoryAnchorLookupCypher(entityLabel, "uid", repoScoped),
		params,
	)
	if err != nil {
		return req, err
	}
	if len(uidRow) > 0 {
		req.GraphAnchorPropertyResolved = true
		req.GraphAnchorProperty = "uid"
		return req, nil
	}
	idRow, err := h.Neo4j.RunSingle(
		ctx,
		nornicDBRelationshipStoryAnchorLookupCypher(entityLabel, "id", repoScoped),
		params,
	)
	if err != nil {
		return req, err
	}
	req.GraphAnchorPropertyResolved = true
	if len(idRow) > 0 {
		req.GraphAnchorProperty = "id"
	}
	return req, nil
}

func nornicDBRelationshipStoryGraphCypher(
	req relationshipStoryRequest,
	entityID string,
	entityLabel string,
	property string,
	direction string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	relationshipType, _ := req.NormalizedRelationshipType()
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.NormalizedLimit() + 1,
		"offset":    req.Offset,
	}
	params = relationshipStoryAccessParams(req, access, params)
	relPattern := ":" + relationshipType
	entityPattern := relationships.NornicDBNodePatternWithProperty("anchor", entityLabel, property, "$entity_id")
	if direction == "incoming" {
		predicates := relationshipStoryRepoPredicates(req, access, "source", "anchor", "anchor")
		return `
		MATCH ` + entityPattern + `<-[rel` + relPattern + `]-(source)
		` + nornicDBRelationshipStoryWhere(predicates) + `
		OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:File)
		OPTIONAL MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		OPTIONAL MATCH (anchor)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN 'incoming' as direction,
		       '` + relationshipType + `' as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       source.id as source_legacy_id,
		       source.uid as source_uid,
		       source.name as source_name,
		       source.repo_id as source_node_repo_id,
		       sourceRepo.id as source_repo_fallback_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       source.language as source_language_value,
		       source.lang as source_lang_value,
		       sourceFile.language as source_file_language,
		       anchor.id as target_legacy_id,
		       anchor.uid as target_uid,
		       anchor.name as target_name,
		       anchor.repo_id as target_node_repo_id,
		       targetRepo.id as target_repo_fallback_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       anchor.language as target_language_value,
		       anchor.lang as target_lang_value,
		       targetFile.language as target_file_language
		ORDER BY source.name, source.id, source.uid
		SKIP $offset
		LIMIT $limit
	`, params
	}
	predicates := relationshipStoryRepoPredicates(req, access, "anchor", "target", "anchor")
	return `
		MATCH ` + entityPattern + `-[rel` + relPattern + `]->(target)
		` + nornicDBRelationshipStoryWhere(predicates) + `
		OPTIONAL MATCH (anchor)<-[:CONTAINS]-(sourceFile:File)
		OPTIONAL MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN 'outgoing' as direction,
		       '` + relationshipType + `' as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       anchor.id as source_legacy_id,
		       anchor.uid as source_uid,
		       anchor.name as source_name,
		       anchor.repo_id as source_node_repo_id,
		       sourceRepo.id as source_repo_fallback_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       anchor.language as source_language_value,
		       anchor.lang as source_lang_value,
		       sourceFile.language as source_file_language,
		       target.id as target_legacy_id,
		       target.uid as target_uid,
		       target.name as target_name,
		       target.repo_id as target_node_repo_id,
		       targetRepo.id as target_repo_fallback_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       target.language as target_language_value,
		       target.lang as target_lang_value,
		       targetFile.language as target_file_language
		ORDER BY target.name, target.id, target.uid
		SKIP $offset
		LIMIT $limit
	`, params
}

func (h *CodeHandler) nornicDBRelationshipStoryClassMethods(
	ctx context.Context,
	req relationshipStoryRequest,
	entityID string,
) ([]map[string]any, error) {
	access := codeGrantAccessFilter(ctx)
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBRelationshipStoryClassMethodsCypher(req, entityID, property, access)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return normalizeNornicDBRelationshipStoryRows(rows), nil
		}
	}
	return []map[string]any{}, nil
}

func (h *CodeHandler) nornicDBRelationshipStoryInheritanceDepthRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entityID string,
	direction string,
) ([]map[string]any, int, error) {
	access := codeGrantAccessFilter(ctx)
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBRelationshipStoryInheritanceDepthCypher(req, entityID, direction, property, access)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, 0, err
		}
		// The raw count decides which id property anchors the walk, before the
		// grant filter runs. Filtering first would let a walk whose every row is
		// out of grant look like "this property did not match" and fall through
		// to the next property, which is a different question.
		if len(rows) > 0 {
			// The raw count is also what the caller's truncation signal must be
			// computed from. The statement binds LIMIT normalizedLimit()+1, so a
			// full page means "there is more"; measuring that after the grant
			// filter would report a page thinned to exactly `limit` as complete
			// when granted rows beyond it were never fetched.
			//
			// Say the rest of it plainly, because the raw count makes this
			// honest without removing it: the filter runs AFTER the LIMIT, and
			// rows past that LIMIT were never read. So a page whose first
			// limit+1 rows carry out-of-grant interiors comes back with FEWER
			// than `limit` in-grant ancestors even though the caller is
			// entitled to more, and the truncated flag is what tells them the
			// page is incomplete. Filling it would need an over-fetch -- read
			// more than limit+1 and stop once limit+1 rows survive the filter --
			// which is a bounded-read change with its own performance
			// argument, not a change to make while closing #6548.
			// TestNornicDBInheritanceWalkPageCanBeThinnerThanTheLimit pins the
			// shape so it is a known cost rather than a surprise.
			return normalizeNornicDBRelationshipStoryRows(nornicDBInheritanceRowsInGrant(rows, access)), len(rows), nil
		}
	}
	return []map[string]any{}, 0, nil
}

// nornicDBInheritanceRowsInGrant drops every inheritance row whose path
// crosses a class the caller was not granted. It forwards to the
// relationships leaf; the pinned inheritance-depth reader above names
// this spelling, so the forwarder keeps that digest byte-identical.
func nornicDBInheritanceRowsInGrant(rows []map[string]any, access querycontract.RepositoryAccessFilter) []map[string]any {
	return relationships.NornicDBInheritanceRowsInGrant(rows, access)
}

func nornicDBRelationshipStoryInheritanceDepthCypher(
	req relationshipStoryRequest,
	entityID string,
	direction string,
	property string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	maxDepth := normalizedRelationshipStoryMaxDepth(req.MaxDepth)
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.NormalizedLimit() + 1,
	}
	if access.Scoped() {
		params = access.GraphParams(params)
	}
	anchorPattern := relationships.NornicDBNodePatternWithProperty("anchor", "Class", property, "$entity_id")
	// The two endpoints bind in Cypher; the classes between them are bound in Go
	// by nornicDBInheritanceRowsInGrant, off this projection.
	//
	// No all(node IN nodes(path) ...) predicate can do it on the pinned build,
	// and the reason is stronger than the one recorded before #6548: the list
	// form never filters AND the scalar form filters everything out, including a
	// chain on which every node is granted. Both directions measured, including
	// on the shortestPath shape the pitfalls page tabulates -- see the
	// path-predicate table in docs/public/reference/nornicdb-path-predicate-pitfalls.md.
	// A list comprehension over nodes(path) is no good either: it comes back as
	// literal expression text. Raw nodes(path) does come back, with real per-hop
	// properties, so the filter reads that.
	pathProjection := ""
	if access.Scoped() {
		pathProjection = ",\n\t\t       nodes(path) as path_nodes"
	}
	if direction == "incoming" {
		return fmt.Sprintf(`
		MATCH path = (source:Class)-[:INHERITS*1..%d]->%s
		`+nornicDBRelationshipStoryWhere(relationshipStoryGrantPredicates(access, "source", "anchor"))+`
		RETURN 'incoming' as direction,
		       source.id as source_legacy_id,
		       source.uid as source_uid,
		       source.name as source_name,
		       anchor.id as target_legacy_id,
		       anchor.uid as target_uid,
		       anchor.name as target_name,
		       length(path) as depth`+pathProjection+`
		ORDER BY depth DESC, source.name, source.id, source.uid
		LIMIT $limit
	`, maxDepth, anchorPattern), params
	}
	return fmt.Sprintf(`
		MATCH path = %s-[:INHERITS*1..%d]->(target:Class)
		`+nornicDBRelationshipStoryWhere(relationshipStoryGrantPredicates(access, "anchor", "target"))+`
		RETURN 'outgoing' as direction,
		       anchor.id as source_legacy_id,
		       anchor.uid as source_uid,
		       anchor.name as source_name,
		       target.id as target_legacy_id,
		       target.uid as target_uid,
		       target.name as target_name,
		       length(path) as depth`+pathProjection+`
		ORDER BY depth DESC, target.name, target.id, target.uid
		LIMIT $limit
	`, anchorPattern, maxDepth), params
}

// NornicDBRelationshipStoryGraphCypher exposes nornicDBRelationshipStoryGraphCypher
// for the cross-family queryplan manifest binding test, which must stay in root
// package query because it binds entity, impact, and infra handlers alongside
// the code builders.
func NornicDBRelationshipStoryGraphCypher(
	req relationshipStoryRequest,
	entityID string,
	entityLabel string,
	property string,
	direction string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return nornicDBRelationshipStoryGraphCypher(req, entityID, entityLabel, property, direction, access)
}

// NornicDBRelationshipStoryInheritanceDepthCypher exposes
// nornicDBRelationshipStoryInheritanceDepthCypher for the cross-family queryplan
// manifest binding test in root package query.
func NornicDBRelationshipStoryInheritanceDepthCypher(
	req relationshipStoryRequest,
	entityID string,
	direction string,
	property string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return nornicDBRelationshipStoryInheritanceDepthCypher(req, entityID, direction, property, access)
}

// NornicDBRelationshipStoryAnchorLookupCypher exposes
// nornicDBRelationshipStoryAnchorLookupCypher for the cross-family queryplan
// manifest binding test in root package query.
func NornicDBRelationshipStoryAnchorLookupCypher(entityLabel string, property string, repoScoped bool) string {
	return nornicDBRelationshipStoryAnchorLookupCypher(entityLabel, property, repoScoped)
}
