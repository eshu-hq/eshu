// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package story

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Class-hierarchy and override reads live here, split out of
// codequery/relationship_story_class.go (#6060). The *CodeHandler
// readers stay in codequery under their queryplan source_sha256 pins
// and call these builders through same-named forwarders.

// SplitClassHierarchyRelationships partitions INHERITS rows into
// parents (outgoing) and children (incoming).
func SplitClassHierarchyRelationships(rows []map[string]any) ([]map[string]any, []map[string]any) {
	parents := make([]map[string]any, 0)
	children := make([]map[string]any, 0)
	for _, row := range rows {
		if !strings.EqualFold(querycontract.StringVal(row, "type"), "INHERITS") {
			continue
		}
		switch querycontract.StringVal(row, "direction") {
		case "outgoing":
			parents = append(parents, row)
		case "incoming":
			children = append(children, row)
		}
	}
	return parents, children
}

// ClassMethodsCypher builds the methods-of-a-class read. The predicate
// renders the entity anchor; the grant predicates bind both endpoints.
func ClassMethodsCypher(
	req codemodel.RelationshipStoryRequest,
	entityID string,
	predicate func(string, string) string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.NormalizedLimit() + 1,
		"offset":    req.Offset,
	}
	if access.Scoped() {
		params = access.GraphParams(params)
	}
	// Both endpoints bind: a class in grant can contain a method the projector
	// attributed to another repository, and the method row is what ships.
	predicates := append([]string{predicate("class", "$entity_id")},
		GrantPredicates(access, "class", "method")...)
	return `
		MATCH (class)-[:CONTAINS]->(method:Function)
		WHERE ` + strings.Join(predicates, " AND ") + `
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

// InheritanceDepthCypher builds one direction of the bounded INHERITS
// walk. This is the Neo4j-compat builder -- the NornicDB backend takes
// the pinned NornicDB reader before this function is reached -- and on
// Neo4j all(node IN nodes(path) WHERE node.repo_id IN $ids) does
// evaluate, which is why the compat call-chain bounds its interior with
// exactly that shape (chain.PathHopPredicates, chain/cypher.go). The
// inertness of the list form is a fact about the pinned NornicDB build
// only, and it is the sibling builder that carries it and says so.
//
// The clause is rendered by the grant contract rather than written out, so a
// change to how it renders moves this predicate with the endpoint ones. The
// bare property is right: a null repo_id makes the membership test null,
// all() over a null yields null, and WHERE null drops the row, so an
// unattributable interior class still fails closed.
func InheritanceDepthCypher(
	req codemodel.RelationshipStoryRequest,
	entityID string,
	direction string,
	predicate func(string, string) string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	maxDepth := NormalizeMaxDepth(req.MaxDepth)
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.NormalizedLimit() + 1,
	}
	if access.Scoped() {
		params = access.GraphParams(params)
	}
	inheritancePredicates := func(anchor string) string {
		predicates := append([]string{predicate(anchor, "$entity_id")},
			GrantPredicates(access, "source", "target")...)
		if access.Scoped() {
			predicates = append(predicates,
				"all(node IN nodes(path) WHERE "+access.GraphConditionOnProperty("node", "repo_id")+")")
		}
		return strings.Join(predicates, " AND ")
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
	`, maxDepth, inheritancePredicates("target")), params
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
	`, maxDepth, inheritancePredicates("source")), params
}

// EntityID resolves the entity id a story read anchors on: the resolved
// entity's when present, else the request's own.
func EntityID(req codemodel.RelationshipStoryRequest, entity *querycontract.EntityContent) string {
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		return strings.TrimSpace(entity.EntityID)
	}
	return strings.TrimSpace(req.EntityID)
}

// MethodRowsWithHandles bounds method rows to the page and attaches
// entity handles.
func MethodRowsWithHandles(rows []map[string]any, limit int) []map[string]any {
	rows = LimitRows(rows, limit)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := querycontract.CloneAnyMap(row)
		if methodID := querycontract.StringVal(item, "method_id"); methodID != "" {
			item["method_handle"] = "entity:" + methodID
		}
		out = append(out, item)
	}
	return out
}

// DepthSummary reports the deepest inheritance hop in each
// direction and whether the caller's page was full.
//
// The truncated flags are computed from ancestorsRaw/descendantsRaw -- the row
// counts the BACKEND returned -- not from the slices beside them. On NornicDB
// those slices have already had out-of-grant paths removed in Go
// (nornicDBInheritanceRowsInGrant), so a page the statement filled to its
// LIMIT of normalizedLimit()+1 can arrive here at exactly `limit` rows. Reading
// the flag off the filtered slice would then report a full page as complete and
// tell the caller it had seen everything while granted rows beyond the page
// were never fetched. This is the rule the pitfalls page states for every
// project-and-filter-in-Go read on this backend.
//
// The depths are read from the FILTERED rows on purpose: a depth measured
// through a class the caller cannot read is the disclosure #6548 closed, so it
// must not come back through this summary either.
func DepthSummary(
	ancestors []map[string]any,
	descendants []map[string]any,
	ancestorsRaw int,
	descendantsRaw int,
	limit int,
) map[string]any {
	return map[string]any{
		"max_parent_depth": maxDepth(ancestors),
		"max_child_depth":  maxDepth(descendants),
		"parent_truncated": ancestorsRaw > limit,
		"child_truncated":  descendantsRaw > limit,
	}
}

func maxDepth(rows []map[string]any) int {
	deepest := 0
	for _, row := range rows {
		if depth := querycontract.IntVal(row, "depth"); depth > deepest {
			deepest = depth
		}
	}
	return deepest
}

// OverrideData shapes OVERRIDES rows into the override story envelope.
func OverrideData(req codemodel.RelationshipStoryRequest, rows []map[string]any) map[string]any {
	limit := req.NormalizedLimit()
	overrides := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(querycontract.StringVal(row, "type"), "OVERRIDES") {
			overrides = append(overrides, row)
		}
	}
	return map[string]any{
		"overrides": RowsWithHandles(LimitRows(overrides, limit)),
		"truncated": len(overrides) > limit,
	}
}

// LimitRows bounds rows to the page, answering empty for a non-positive
// limit.
func LimitRows(rows []map[string]any, limit int) []map[string]any {
	if limit <= 0 {
		return []map[string]any{}
	}
	if len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

// OverrideRowsCypher builds the repo-anchored OVERRIDES read.
func OverrideRowsCypher(
	req codemodel.RelationshipStoryRequest,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{
		"repo_id":         strings.TrimSpace(req.RepoID),
		"limit":           req.NormalizedLimit() + 1,
		"offset":          req.Offset,
		"override_labels": OverrideNodeLabels(),
	}
	if access.Scoped() {
		params = access.GraphParams(params)
	}
	languagePredicate := ""
	if language := strings.TrimSpace(req.Language); language != "" {
		params["language"] = language
		languagePredicate = `
		  AND source.language = $language
		  AND target.language = $language`
	}
	// source is already anchored through the granted repository's own File, so
	// the grant on it is defense in depth. target is the one that was open: an
	// OVERRIDES edge can leave the repository, and the row names the target's
	// id, name and labels.
	grantPredicate := ""
	if predicates := GrantPredicates(access, "source", "target"); len(predicates) > 0 {
		grantPredicate = `
		  AND ` + strings.Join(predicates, `
		  AND `)
	}
	return `
		MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(file:File)-[:CONTAINS]->(source)-[rel:OVERRIDES]->(target)
		WHERE any(label IN labels(source) WHERE label IN $override_labels)
		  AND any(label IN labels(target) WHERE label IN $override_labels)` + languagePredicate + grantPredicate + `
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

// OverrideNodeLabels lists the entity labels that can participate in an
// OVERRIDES edge.
func OverrideNodeLabels() []string {
	return []string{"Function", "Class", "Interface", "Trait", "Struct", "Enum", "Protocol"}
}
