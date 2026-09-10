// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RowLimit bounds a single symbol's direct-relationship read so the hot
// API path cannot issue an unbounded graph read for a pathological
// high-degree node (for example an incoming-CALLS hub). Direct
// relationships of one symbol and one type are far below this ceiling
// in practice; the deterministic ORDER BY makes the bound stable rather
// than arbitrary. The reads over-fetch by one row so a caller can be
// told, via the response's outgoing_truncated/incoming_truncated flags,
// when the ceiling clipped the result -- the exact-truth envelope must
// never silently drop edges.
const RowLimit = 500

// FetchLimit over-fetches one row past the ceiling so truncation is
// detectable without a second count query.
const FetchLimit = RowLimit + 1

// GraphRow resolves one entity's direct relationships on the NornicDB
// backend: the metadata row first, then one one-hop read per requested
// direction. The caller resolves the entity label up front (through the
// pinned codequery reader) and passes the grant in.
func GraphRow(
	ctx context.Context,
	graph querycontract.GraphQuery,
	entityID string,
	name string,
	repoID string,
	direction string,
	relationshipType string,
	access querycontract.RepositoryAccessFilter,
	label string,
) (map[string]any, error) {
	row, err := MetadataRow(ctx, graph, entityID, name, repoID, access, label)
	if err != nil || row == nil {
		return row, err
	}

	rowEntityID := querycontract.StringVal(row, "id")
	entityLabel := PrimaryEntityLabel(row)
	outgoing := []map[string]any{}
	outgoingTruncated := false
	if direction != "incoming" {
		var err error
		// NornicDB currently needs metadata and each requested direction as
		// separate row queries; this avoids Neo4j-style map collection shapes
		// that are not dialect-safe while preserving direct relationship truth.
		outgoing, outgoingTruncated, err = OneHopRelationships(ctx, graph, rowEntityID, "outgoing", relationshipType, entityLabel)
		if err != nil {
			return nil, err
		}
	}
	incoming := []map[string]any{}
	incomingTruncated := false
	if direction != "outgoing" {
		var err error
		incoming, incomingTruncated, err = OneHopRelationships(ctx, graph, rowEntityID, "incoming", relationshipType, entityLabel)
		if err != nil {
			return nil, err
		}
	}
	response := querycontract.CloneAnyMap(row)
	response["outgoing"] = outgoing
	response["incoming"] = incoming
	response["outgoing_truncated"] = outgoingTruncated
	response["incoming_truncated"] = incomingTruncated
	return response, nil
}

// MetadataRow reads the metadata row for one entity: name/repo lookup,
// or the uid/id loop over the pre-resolved label for an entity id.
func MetadataRow(
	ctx context.Context,
	graph querycontract.GraphQuery,
	entityID string,
	name string,
	repoID string,
	access querycontract.RepositoryAccessFilter,
	label string,
) (map[string]any, error) {
	predicate, params := MetadataPredicate(name, repoID, access)
	entityID = strings.TrimSpace(entityID)
	if predicate == "" && entityID == "" {
		return nil, nil
	}
	if entityID != "" {
		if label == "" {
			return nil, nil
		}
		params["entity_id"] = entityID
		for _, property := range []string{"uid", "id"} {
			rows, err := graph.Run(ctx, MetadataCypher(predicate, label, property), params)
			if err != nil {
				return nil, err
			}
			if len(rows) == 1 {
				return rows[0], nil
			}
			if len(rows) > 1 {
				return nil, nil
			}
		}
		return nil, nil
	}
	rows, err := graph.Run(ctx, MetadataCypher(predicate, label, ""), params)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, nil
	}
	return rows[0], nil
}

// OneHopRelationships returns a single symbol's direct relationships
// for one direction. The bool reports whether the row ceiling clipped
// the result so the caller can disclose truncation instead of
// presenting a clipped set as an exact-truth response.
func OneHopRelationships(
	ctx context.Context,
	graph querycontract.GraphQuery,
	entityID string,
	direction string,
	relationshipType string,
	entityLabel string,
) ([]map[string]any, bool, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return []map[string]any{}, false, nil
	}
	for _, property := range []string{"uid", "id"} {
		cypher, params := OneHopRelationshipsCypher(entityID, direction, relationshipType, entityLabel, property)
		rows, err := graph.Run(ctx, cypher, params)
		if err != nil {
			return nil, false, err
		}
		if len(rows) > 0 {
			truncated := len(rows) > RowLimit
			if truncated {
				rows = rows[:RowLimit]
			}
			enriched, err := EnrichRows(ctx, graph, rows, entityID, direction, relationshipType, entityLabel, property)
			if err != nil {
				return nil, false, err
			}
			return NormalizeRows(enriched), truncated, nil
		}
	}
	return []map[string]any{}, false, nil
}

// TransitiveRows walks the CALLS frontier breadth-first to maxDepth,
// resolving each hop through oneHop (the pinned codequery one-hop read
// keeps its digest by staying a method; the caller injects it here).
func TransitiveRows(
	ctx context.Context,
	entityID string,
	direction string,
	maxDepth int,
	oneHop func(ctx context.Context, entityID string, direction string) ([]map[string]any, error),
) ([]map[string]any, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" || maxDepth <= 0 {
		return []map[string]any{}, nil
	}

	frontier := []string{entityID}
	seen := map[string]struct{}{entityID: {}}
	rows := make([]map[string]any, 0)
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		next := make([]string, 0)
		for _, currentID := range frontier {
			hopRows, err := oneHop(ctx, currentID, direction)
			if err != nil {
				return nil, err
			}
			for _, hop := range hopRows {
				relationship := transitiveRow(hop, direction, depth)
				nextID := transitiveNextID(relationship, direction)
				if nextID == "" {
					continue
				}
				if _, ok := seen[nextID]; ok {
					continue
				}
				seen[nextID] = struct{}{}
				next = append(next, nextID)
				rows = append(rows, relationship)
			}
		}
		frontier = next
	}
	return rows, nil
}

func transitiveRow(row map[string]any, direction string, depth int) map[string]any {
	out := querycontract.CloneAnyMap(row)
	out["depth"] = depth
	if direction == "incoming" {
		out["target_id"] = ""
		out["target_name"] = ""
	} else {
		out["source_id"] = ""
		out["source_name"] = ""
	}
	return out
}

func transitiveNextID(row map[string]any, direction string) string {
	if direction == "incoming" {
		return querycontract.StringVal(row, "source_id")
	}
	return querycontract.StringVal(row, "target_id")
}

// OneHopRelationshipsCypher is the relationship core read. It retains
// the OPTIONAL-MATCH-free shape introduced for older NornicDB builds
// that returned literal text for type(rel), coalesce(...), and
// head(labels(...)). NornicDB v1.2.3 evaluates that historical shape
// correctly, but the split also preserves partial File-without-
// Repository metadata and bounded enrichment. See
// docs/public/reference/nornicdb-pitfalls.md. File and repository
// metadata is fetched by the separate enrichment reads in enrich.go and
// merged in Go. The extra source_entity_uid/target_entity_uid columns
// key that merge and are stripped before the response.
func OneHopRelationshipsCypher(entityID string, direction string, relationshipType string, entityLabel string, entityIDProperty string) (string, map[string]any) {
	params := map[string]any{"entity_id": entityID, "row_limit": FetchLimit}
	relPattern := NornicDBRelationshipPattern(relationshipType)
	entityPattern := NornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	if direction == "incoming" {
		return `
		MATCH ` + entityPattern + `<-[rel` + relPattern + `]-(source)
		RETURN 'incoming' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       source.name as source_name,
		       coalesce(source.id, source.uid) as source_id,
		       coalesce(source.id, source.uid) as source_entity_uid,
		       source.language as source_language,
		       head(labels(source)) as source_type,
		       source.start_line as source_start_line,
		       source.end_line as source_end_line,
		       e.name as target_name,
		       coalesce(e.id, e.uid) as target_id,
		       coalesce(e.id, e.uid) as target_entity_uid,
		       e.language as target_language,
		       head(labels(e)) as target_type,
		       e.start_line as target_start_line,
		       e.end_line as target_end_line
		ORDER BY source.uid
		LIMIT $row_limit
	`, params
	}
	return `
		MATCH ` + entityPattern + `-[rel` + relPattern + `]->(target)
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       e.name as source_name,
		       coalesce(e.id, e.uid) as source_id,
		       coalesce(e.id, e.uid) as source_entity_uid,
		       e.language as source_language,
		       head(labels(e)) as source_type,
		       e.start_line as source_start_line,
		       e.end_line as source_end_line,
		       target.name as target_name,
		       coalesce(target.id, target.uid) as target_id,
		       coalesce(target.id, target.uid) as target_entity_uid,
		       target.language as target_language,
		       head(labels(target)) as target_type,
		       target.start_line as target_start_line,
		       target.end_line as target_end_line
		ORDER BY target.uid
		LIMIT $row_limit
	`, params
}

// NornicDBLabelPattern renders one entity label as a node label
// pattern, or "" for an unknown label.
func NornicDBLabelPattern(label string) string {
	label = NornicDBGraphLabelForContentEntityType(label)
	if label == "" {
		return ""
	}
	return ":" + label
}

// NornicDBNodePattern renders an entity lookup anchored on the uid
// property.
func NornicDBNodePattern(alias string, label string, param string) string {
	return NornicDBNodePatternWithProperty(alias, label, "uid", param)
}

// NornicDBNodePatternWithProperty keeps NornicDB entity-id lookups
// anchored in the node pattern. Live dogfood showed MATCH-plus-WHERE
// id/uid predicates can scan or hang, while relationship-pattern MATCH
// keeps type(rel) populated.
func NornicDBNodePatternWithProperty(alias string, label string, property string, param string) string {
	property = strings.TrimSpace(property)
	if property == "" {
		property = "uid"
	}
	return "(" + alias + NornicDBLabelPattern(label) + " {" + property + ": " + param + "})"
}

// NornicDBRelationshipPattern renders one relationship type as a
// pattern constraint, or "" for an unknown type.
func NornicDBRelationshipPattern(relationshipType string) string {
	switch strings.ToUpper(strings.TrimSpace(relationshipType)) {
	case "CALLS", "REFERENCES", "IMPORTS", "INHERITS", "OVERRIDES", "USES_METACLASS":
		return ":" + strings.ToUpper(strings.TrimSpace(relationshipType))
	default:
		return ""
	}
}

// NormalizeRows clones relationship rows and strips the backend's
// placeholder projections: a NornicDB miss renders the projected
// expression text (for example `head(labels(source))`) instead of a
// value, so any cell still carrying expression text is dropped rather
// than presented as entity truth.
func NormalizeRows(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	normalized := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := querycontract.CloneAnyMap(row)
		removePlaceholderProperties(item)
		normalized = append(normalized, item)
	}
	return normalized
}

func removePlaceholderProperties(row map[string]any) {
	for _, key := range []string{
		"call_kind",
		"confidence",
		"reason",
		"resolution_method",
		"source_id",
		"source_name",
		"source_repo_id",
		"source_repo_name",
		"source_file_path",
		"source_language",
		"source_type",
		"source_start_line",
		"source_end_line",
		"target_id",
		"target_name",
		"target_repo_id",
		"target_repo_name",
		"target_file_path",
		"target_language",
		"target_type",
		"target_start_line",
		"target_end_line",
	} {
		removePlaceholderProperty(row, key)
	}
	removePlaceholderValue(row, "source_repo_id", "sourceRepo.id")
	removePlaceholderValue(row, "source_repo_name", "sourceRepo.name")
	removePlaceholderValue(row, "source_file_path", "sourceFile.relative_path")
	removePlaceholderValue(row, "source_language", "source.language", "sourceFile.language")
	removePlaceholderValue(row, "source_type", "head(labels(source))")
	removePlaceholderValue(row, "source_start_line", "source.start_line")
	removePlaceholderValue(row, "source_end_line", "source.end_line")
	removePlaceholderValue(row, "target_repo_id", "targetRepo.id")
	removePlaceholderValue(row, "target_repo_name", "targetRepo.name")
	removePlaceholderValue(row, "target_file_path", "targetFile.relative_path")
	removePlaceholderValue(row, "target_language", "target.language", "targetFile.language")
	removePlaceholderValue(row, "target_type", "head(labels(target))")
	removePlaceholderValue(row, "target_start_line", "target.start_line")
	removePlaceholderValue(row, "target_end_line", "target.end_line")
}

func removePlaceholderValue(row map[string]any, key string, placeholders ...string) {
	value := strings.TrimSpace(querycontract.StringVal(row, key))
	if value == "" {
		delete(row, key)
		return
	}
	for _, placeholder := range placeholders {
		if value == placeholder {
			delete(row, key)
			return
		}
	}
}

func removePlaceholderProperty(row map[string]any, key string) {
	value := strings.TrimSpace(querycontract.StringVal(row, key))
	if value == "" {
		delete(row, key)
		return
	}
	if value == key || strings.HasSuffix(value, "."+key) {
		delete(row, key)
	}
}
