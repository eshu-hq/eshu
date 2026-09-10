// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package story

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Direct graph reads live here, split out of
// codequery/relationship_story_graph.go (#6060). The *CodeHandler
// readers stay in codequery under their queryplan source_sha256 pins
// and call these builders through same-named forwarders.

// GraphCypher builds the direct one-direction relationship read. The
// predicate renders the entity anchor; the grant predicates bind the
// endpoints the request reaches.
func GraphCypher(
	req codemodel.RelationshipStoryRequest,
	entity *querycontract.EntityContent,
	direction string,
	predicate func(string, string) string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	relationshipType, _ := req.NormalizedRelationshipType()
	params := map[string]any{
		"entity_id": strings.TrimSpace(req.EntityID),
		"limit":     req.NormalizedLimit() + 1,
		"offset":    req.Offset,
	}
	params = AccessParams(req, access, params)
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		params["entity_id"] = strings.TrimSpace(entity.EntityID)
	}
	relPattern := ":" + relationshipType
	if direction == "incoming" {
		predicates := []string{predicate("target", "$entity_id")}
		predicates = append(predicates, RepoPredicates(req, access, "source", "target", "target")...)
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
	predicates = append(predicates, RepoPredicates(req, access, "source", "target", "source")...)
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

// AccessParams binds the parameters the story reads' repository
// predicates reference: the caller-supplied repo_id, and the grant arrays
// querycontract.RepositoryAccessFilter renders its condition against.
func AccessParams(
	req codemodel.RelationshipStoryRequest,
	access querycontract.RepositoryAccessFilter,
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

// RepoPredicates returns the predicates that decide which
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
func RepoPredicates(
	req codemodel.RelationshipStoryRequest,
	access querycontract.RepositoryAccessFilter,
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
	return append(predicates, GrantPredicates(access, sourceAlias, targetAlias)...)
}

// GrantPredicates returns the caller's grant condition on each
// alias's repo_id, and nothing at all for an unscoped caller.
//
// The class-hierarchy and override reads use this rather than the fuller
// RepoPredicates above: they never carried a repo_id predicate,
// so adding one would change what a shared-key caller reads, while the grant is
// what those reads are missing.
func GrantPredicates(access querycontract.RepositoryAccessFilter, aliases ...string) []string {
	if !access.Scoped() {
		return nil
	}
	predicates := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		predicates = append(predicates, access.GraphConditionOnProperty(alias, "repo_id"))
	}
	return predicates
}

// NextID reports the frontier id a transitive hop continues from: the
// row's far end for the direction being walked.
func NextID(row map[string]any, direction string) string {
	if direction == "incoming" {
		return querycontract.StringVal(row, "source_id")
	}
	return querycontract.StringVal(row, "target_id")
}

// InterleaveDirections merges per-direction pages round-robin so one
// leg cannot starve the other.
func InterleaveDirections(incoming []map[string]any, outgoing []map[string]any) []map[string]any {
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

// ContentRows shapes one content-store entity row into story rows for
// the content-fallback path, filtering to the requested type and
// paging by offset and limit.
func ContentRows(row map[string]any, req codemodel.RelationshipStoryRequest) []map[string]any {
	relationshipType, _ := req.NormalizedRelationshipType()
	direction, _ := req.NormalizedDirection()
	rows := make([]map[string]any, 0)
	if direction != "outgoing" {
		rows = append(rows, relationships.FilterRelationships(relationships.MapRelationships(row["incoming"]), relationshipType)...)
	}
	if direction != "incoming" {
		rows = append(rows, relationships.FilterRelationships(relationships.MapRelationships(row["outgoing"]), relationshipType)...)
	}
	limit := req.NormalizedLimit() + 1
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
