// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package story

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// NornicDB story readers live here, split out of
// codequery/relationship_story_nornicdb.go (#6060). The *CodeHandler
// readers stay in codequery under their queryplan source_sha256 pins
// and call these builders through same-named forwarders. The two
// hot-manifest cypher builders stay in codequery with their query
// text intact; only their helpers moved.

// AnchorPreflightSupported reports whether the
// one-time uid-first preflight has a bounded multi-type lookup. Repository-
// scoped requests can anchor every supported entity label through the selected
// Repository and its owned File before checking entity identity. Unscoped
// requests retain the indexed Function-only path.
func AnchorPreflightSupported(
	req codemodel.RelationshipStoryRequest,
	entity *querycontract.EntityContent,
) bool {
	if entity == nil {
		return false
	}
	label := relationships.NornicDBGraphLabelForContentEntityType(entity.EntityType)
	if label == "" {
		return false
	}
	return strings.TrimSpace(req.RepoID) != "" || label == "Function"
}

// AnchorLookupCypher builds the one-row identity-preflight read for one
// entity label, id property, and scope.
func AnchorLookupCypher(
	entityLabel string,
	property string,
	repoScoped bool,
) string {
	if repoScoped {
		return "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->" +
			"(anchor:" + entityLabel + ") WHERE anchor." + property +
			" = $entity_id RETURN true AS found LIMIT 1"
	}
	return "MATCH (anchor:" + entityLabel + " {" + property +
		": $entity_id}) RETURN true AS found LIMIT 1"
}

// ProjectionCandidate names one projected column that can satisfy a
// collapsed story key, with the placeholder literals that must not
// satisfy it.
type ProjectionCandidate struct {
	key          string
	placeholders []string
}

// Projection names a projected column candidate for a collapsed story
// key.
func Projection(key string, placeholders ...string) ProjectionCandidate {
	return ProjectionCandidate{key: key, placeholders: placeholders}
}

// NormalizeRows collapses the NornicDB story projection's aliased
// columns onto the canonical story keys.
func NormalizeRows(rows []map[string]any) []map[string]any {
	normalized := relationships.NormalizeRows(rows)
	for _, row := range normalized {
		CollapseProjection(row, "source_id",
			Projection("source_legacy_id", "source.id", "anchor.id"),
			Projection("source_uid", "source.uid", "anchor.uid"),
		)
		CollapseProjection(row, "source_repo_id",
			Projection("source_node_repo_id", "source.repo_id", "anchor.repo_id"),
			Projection("source_repo_fallback_id", "sourceRepo.id"),
		)
		CollapseProjection(row, "source_language",
			Projection("source_language_value", "source.language", "anchor.language"),
			Projection("source_lang_value", "source.lang", "anchor.lang"),
			Projection("source_file_language", "sourceFile.language"),
		)
		CollapseProjection(row, "target_id",
			Projection("target_legacy_id", "target.id", "anchor.id"),
			Projection("target_uid", "target.uid", "anchor.uid"),
		)
		CollapseProjection(row, "target_repo_id",
			Projection("target_node_repo_id", "target.repo_id", "anchor.repo_id"),
			Projection("target_repo_fallback_id", "targetRepo.id"),
		)
		CollapseProjection(row, "target_language",
			Projection("target_language_value", "target.language", "anchor.language"),
			Projection("target_lang_value", "target.lang", "anchor.lang"),
			Projection("target_file_language", "targetFile.language"),
		)
		CollapseProjection(row, "method_id",
			Projection("method_legacy_id", "method.id"),
			Projection("method_uid", "method.uid"),
		)
	}
	return normalized
}

// CollapseProjection folds a projected column's candidates onto one
// story key, keeping the first non-empty non-placeholder value.
func CollapseProjection(
	row map[string]any,
	targetKey string,
	candidates ...ProjectionCandidate,
) {
	projected := false
	var selected any
	for _, candidate := range candidates {
		value, present := row[candidate.key]
		if !present {
			continue
		}
		projected = true
		delete(row, candidate.key)
		text := strings.TrimSpace(querycontract.StringVal(map[string]any{candidate.key: value}, candidate.key))
		if selected == nil && text != "" && text != candidate.key && !containsPlaceholder(text, candidate.placeholders) {
			selected = value
		}
	}
	if !projected {
		return
	}
	if selected == nil {
		delete(row, targetKey)
		return
	}
	row[targetKey] = selected
}

func containsPlaceholder(value string, placeholders []string) bool {
	for _, placeholder := range placeholders {
		if value == placeholder {
			return true
		}
	}
	return false
}

// Where joins predicates into a WHERE clause, or answers empty for
// none.
func Where(predicates []string) string {
	if len(predicates) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(predicates, " AND ")
}

// NornicDBClassMethodsCypher builds the NornicDB methods-of-a-class read for
// one identity property.
func NornicDBClassMethodsCypher(
	req codemodel.RelationshipStoryRequest,
	entityID string,
	property string,
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
	classPattern := relationships.NornicDBNodePatternWithProperty("class", "Class", property, "$entity_id")
	// Both endpoints bind: a class in grant can contain a method the projector
	// attributed to another repository, and the method row is what ships.
	predicates := GrantPredicates(access, "class", "method")
	return `
		MATCH ` + classPattern + `-[:CONTAINS]->(method:Function)
		` + Where(predicates) + `
		RETURN method.id as method_legacy_id,
		       method.uid as method_uid,
		       method.name as method_name,
		       method.path as file_path,
		       method.start_line as start_line,
		       method.end_line as end_line
		ORDER BY method.name, method.id, method.uid
		SKIP $offset
		LIMIT $limit
	`, params
}

// NornicDBInheritanceDepthCypher builds one direction of the bounded NornicDB
// INHERITS walk for one identity property.
//
// The two endpoints bind in Cypher; the classes between them are bound in Go
// by the inheritance grant filter, off this projection.
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
func NornicDBInheritanceDepthCypher(
	req codemodel.RelationshipStoryRequest,
	entityID string,
	direction string,
	property string,
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
	anchorPattern := relationships.NornicDBNodePatternWithProperty("anchor", "Class", property, "$entity_id")
	pathProjection := ""
	if access.Scoped() {
		pathProjection = ",\n\t\t       nodes(path) as path_nodes"
	}
	if direction == "incoming" {
		return fmt.Sprintf(`
		MATCH path = (source:Class)-[:INHERITS*1..%d]->%s
		`+Where(GrantPredicates(access, "source", "anchor"))+`
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
		`+Where(GrantPredicates(access, "anchor", "target"))+`
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
