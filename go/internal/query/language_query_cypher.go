// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// buildLanguageCypher is the unscoped form of the dispatcher below. It binds an
// explicitly all-scopes filter rather than the zero value, whose Scoped() is
// true and would render a grant condition against unbound parameters.
func buildLanguageCypher(language, label, query, repoID string, limit int) (string, map[string]any) {
	return buildLanguageCypherWithSemanticFilter(
		language, label, query, repoID, limit, "", "",
		repositoryAccessFilter{AllScopes: true},
	)
}

// buildLanguageCypherWithSemanticFilter dispatches the route's four graph
// builders. access is the caller's repository grant: each builder appends it to
// the WHERE of the required MATCH that binds Repository -- the same WHERE that
// already carries the optional `r.id = $repo_id` anchor -- so it lands ahead of
// every WITH, ORDER BY and LIMIT, and merges the grant arrays into the params
// through GraphParams.
//
// All four builders now emit a single MATCH clause, so the grant lands in the
// anchoring MATCH's own WHERE in every one of them. buildDirectoryCypher used
// to be the exception -- two MATCH clauses with the WHERE on the second while
// `r` was bound in the first -- and it was rewritten to one clause for a
// backend reason of its own, described on that function.
//
// The Repository binding is non-optional in all four patterns, so the condition
// decides row membership rather than nulling a projection (the OPTIONAL MATCH
// trap #5167 batch 1 hit on complexityListAnchor).
func buildLanguageCypherWithSemanticFilter(
	language,
	label,
	query,
	repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
	access repositoryAccessFilter,
) (string, map[string]any) {
	language = canonicalLanguage(language)
	params := map[string]any{
		"language": language,
		"limit":    limit,
	}

	// Build the extension filter for this language.
	exts := languageFileExtensions[language]
	extFilter := buildExtensionFilter(exts)

	switch label {
	case "Repository":
		return buildRepositoryCypher(language, query, repoID, limit, access)
	case "Directory":
		return buildDirectoryCypher(language, extFilter, query, repoID, params, access)
	case "File":
		return buildFileCypher(language, extFilter, query, repoID, params, access)
	default:
		return buildEntityCypherWithSemanticFilter(
			language,
			label,
			extFilter,
			query,
			repoID,
			params,
			semanticFilterKey,
			semanticFilterValue,
			access,
		)
	}
}

// buildRepositoryCypher returns a query for repositories that contain files
// in the given language.
func buildRepositoryCypher(language, query, repoID string, limit int, access repositoryAccessFilter) (string, map[string]any) {
	params := map[string]any{
		"language": language,
		"limit":    limit,
	}

	cypher := `
		MATCH (r:Repository)-[:REPO_CONTAINS]->(f:File)
		WHERE (f.language = $language OR f.language = $language_title)
	`
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("r")
	params = access.GraphParams(params)
	if query != "" {
		cypher += " AND r.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		WITH r, count(f) as file_count
		RETURN r.id as id, r.name as name,
		       coalesce(r.local_path, r.path) as local_path,
		       r.remote_url as remote_url,
		       file_count
		ORDER BY file_count DESC
		LIMIT $limit
	`
	return cypher, params
}

// buildDirectoryCypher returns a query for directories containing files in the
// given language.
//
// The three-node join is written as ONE linear pattern rather than the two
// MATCH clauses it used to be, and that is a correctness fix, not style. On the
// pinned NornicDB build a read with two MATCH clauses followed by a
// `WITH ... count(...)` aggregation returns ZERO rows as soon as the RETURN
// projects anything richer than a plain property or a literal -- `labels(d)`
// here, but `coalesce(...)` and a list construction do it too. It is a row
// drop, not an error, so this route answered `entity_type: "directory"` with an
// empty list on the default backend for every caller and said nothing about it.
// One MATCH clause evaluates the identical join correctly. The reproduction and
// the nine-probe bisection are in
// TestLiveNornicDBLanguageQueryDirectoryTwoClauseShapeReturnsNothing and in
// docs/public/reference/nornicdb-query-pitfalls.md.
//
// The direction is anchored at File deliberately. Writing the same single
// clause forward from Repository --
// `(r:Repository)-[:REPO_CONTAINS|CONTAINS*]->(d:Directory)-[:CONTAINS]->(f:File)`
// -- was measured on the same build and returns WRONG counts: a nested
// directory's file is folded into its parent's `file_count` and the nested
// directory disappears from the answer. Anchoring at File keeps the last
// CONTAINS hop out of the variable-length chain, so `d` binds to the directory
// that directly holds each file, which is what `count(f)` has to mean.
func buildDirectoryCypher(language, extFilter, query, repoID string, params map[string]any, access repositoryAccessFilter) (string, map[string]any) {
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	cypher := `
		MATCH (f:File)<-[:CONTAINS]-(d:Directory)<-[:REPO_CONTAINS|CONTAINS*]-(r:Repository)
		WHERE (f.language = $language OR f.language = $language_title` + extFilter + `)
	`

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("r")
	params = access.GraphParams(params)
	if query != "" {
		cypher += " AND d.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		WITH d, r, count(f) as file_count
		RETURN d.id as entity_id, d.name as name, labels(d) as labels,
		       d.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       file_count
		ORDER BY file_count DESC
		LIMIT $limit
	`
	return cypher, params
}

// buildFileCypher returns a query for files in the given language.
func buildFileCypher(language, extFilter, query, repoID string, params map[string]any, access repositoryAccessFilter) (string, map[string]any) {
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	cypher := `
		MATCH (f:File)<-[:REPO_CONTAINS]-(r:Repository)
		WHERE (f.language = $language OR f.language = $language_title` + extFilter + `)
	`

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("r")
	params = access.GraphParams(params)
	if query != "" {
		cypher += " AND f.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		RETURN f.id as entity_id, f.name as name, labels(f) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       f.language as language
		ORDER BY f.relative_path
		LIMIT $limit
	`
	return cypher, params
}

func buildEntityCypherWithSemanticFilter(
	language, label, extFilter, query, repoID string,
	params map[string]any,
	semanticFilterKey string,
	semanticFilterValue string,
	access repositoryAccessFilter,
) (string, map[string]any) {
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	cypher := fmt.Sprintf(`
		MATCH (e:%s)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		WHERE (e.language = $language OR e.language = $language_title
		       OR f.language = $language OR f.language = $language_title%s)
	`, label, extFilter)

	if semanticFilterKey != "" {
		cypher += fmt.Sprintf(" AND coalesce(e.%s, '') = $semantic_filter", semanticFilterKey)
		params["semantic_filter"] = semanticFilterValue
	}

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("r")
	params = access.GraphParams(params)
	if query != "" {
		cypher += " AND e.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		RETURN e.id as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line, e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY f.relative_path, e.name
		LIMIT $limit
	`
	return cypher, params
}

// buildExtensionFilter returns a Cypher OR clause fragment that matches common
// file extensions for a language. Returns an empty string when no extensions
// are registered.
func buildExtensionFilter(exts []string) string {
	if len(exts) == 0 {
		return ""
	}
	clauses := make([]string, 0, len(exts))
	for _, ext := range exts {
		clauses = append(clauses, fmt.Sprintf("f.name ENDS WITH '%s'", ext))
	}
	return " OR " + strings.Join(clauses, " OR ")
}

// buildLanguageResult converts a Neo4j result row into the response shape.
// joinKeys returns a sorted comma-separated list of map keys.
func joinKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Sort for deterministic output.
	sortStrings(keys)
	return strings.Join(keys, ", ")
}

// sortStrings sorts a string slice in place (insertion sort for small slices).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// SupportedLanguages returns the set of language names with query support.
func SupportedLanguages() []string {
	return mapKeys(supportedLanguages)
}

// SupportedEntityTypes returns the set of entity type names with query support.
func SupportedEntityTypes() []string {
	return mapKeys(allSupportedEntityTypes())
}

// mapKeys returns sorted keys from a map.
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}
