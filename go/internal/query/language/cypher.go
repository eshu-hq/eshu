// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// buildLanguageCypher is the unscoped form of the dispatcher below. It binds an
// explicitly all-scopes filter rather than the zero value, whose Scoped() is
// true and would render a grant condition against unbound parameters.
func buildLanguageCypher(language, label, query, repoID string, limit int) (string, map[string]any) {
	return BuildCypherWithSemanticFilter(
		language, label, query, repoID, limit, "", "",
		querycontract.RepositoryAccessFilter{AllScopes: true}, nil,
	)
}

// BuildCypherWithSemanticFilter dispatches the route's four graph
// builders. access is the caller's repository grant: each builder appends it to
// the WHERE of the required MATCH that binds Repository -- the same WHERE that
// already carries the optional `r.id = $repo_id` anchor -- so it lands ahead of
// every WITH, ORDER BY and LIMIT, and merges the grant arrays into the params
// through GraphParams.
//
// Three of the four builders emit a single MATCH clause, so the grant lands in
// the anchoring MATCH's own WHERE in each of them. buildDirectoryCypher is the
// exception since #6541: it binds no Repository at all, and carries the grant
// as the resolved repository-id list it UNWINDs, which directoryRepoIDs
// supplies. Its doc comment carries the measurement behind that shape.
//
// directoryRepoIDs is the resolved, deduplicated repository-id list the
// Directory branch UNWINDs, and is ignored by the other three. It is a
// parameter rather than something this function derives from access because an
// unscoped admin caller's list cannot be derived at all: it has to be read from
// the graph, which needs a context and a reader this pure builder has neither
// of. Handler.languageQueryGraphRows resolves it for every caller class and is
// the only production caller; a scoped or repository-anchored caller's list IS
// derived here, so only the unscoped case depends on the argument.
//
// The Repository binding is non-optional in all four patterns, so the condition
// decides row membership rather than nulling a projection (the OPTIONAL MATCH
// trap #5167 batch 1 hit on complexityListAnchor).
//
// The language predicate of all four builders is `f.language IN $languages`,
// and nothing else: no file-extension fallback. The Directory, File and entity
// builders used to OR `f.name ENDS WITH '<ext>'` terms into the same WHERE,
// and on the pinned NornicDB build ENDS WITH (STARTS WITH too) evaluates as
// true for every row of a multi-node MATCH, so `OR true` admitted every file
// in the store whatever language was asked for (#6546). The projector writes
// `language` onto every File and semantic entity it emits, using the parser's
// own spelling, so the property carries the answer on its own once
// graphLanguageSpellings supplies those spellings. The Repository builder
// never carried the fallback, but its two equalities never reached those
// spellings either, so a csharp repository query found nothing; it binds the
// same list now. The measurement behind the shape is in
// docs/internal/evidence/6546-language-query-extension-filter.md and the live
// proof in TestLiveNornicDBLanguageQueryAdmitsOnlyTheRequestedLanguage.
//
// Performance Evidence: the #6642 move changed no Cypher text, and every
// statement below except the Directory one is still byte-identical to its
// pre-move source in package query's language_query_cypher.go, verified by the
// queryplan source_sha256 pin (see README.md) and the frozen-text tests that
// moved with this file (cypher_shipped_text_test.go). buildDirectoryCypher was
// then REWRITTEN by #6541: it replaced an unbounded
// `<-[:REPO_CONTAINS|CONTAINS*]-` walk, measured at 34.5s for a caller granted
// one repository and 2m01s for one granted fifty on a 50-repository corpus,
// with an indexed repo_id seek measured at 53ms against 4.964s for the same
// aggregation reached through a WHERE. Its correctness proof on two NornicDB
// builds and the corpus-timing recipe are in
// docs/internal/evidence/6541-directory-query-s2.md.
// Observability Evidence: the span this route emits (SpanQueryLanguageQuery)
// and its route/capability attributes are unchanged (see handler.go), and the
// Directory branch adds Handler.logDirectoryRead, which records the grant
// width, the rows the backend returned, the rows kept after truncation, and
// how many repositories the name lookup resolved.
func BuildCypherWithSemanticFilter(
	language,
	label,
	query,
	repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
	access querycontract.RepositoryAccessFilter,
	directoryRepoIDs []string,
) (string, map[string]any) {
	language = canonicalLanguage(language)
	// Only $languages and $limit are referenced by the builders below; the
	// canonical name reaches the graph through graphLanguageSpellings.
	params := map[string]any{
		"limit": limit,
	}

	switch label {
	case "Repository":
		return buildRepositoryCypher(language, query, repoID, limit, access)
	case "Directory":
		// A scoped or repository-anchored caller's list follows from the grant
		// alone, so it is derived here and directoryRepoIDs is ignored. Only an
		// unscoped caller that named no repository needs the caller's list,
		// because that one cannot be derived without reading the graph.
		ids, everyRepository := directoryRepositoryIDsForGrant(access, repoID)
		if everyRepository {
			ids = directoryRepoIDs
		}
		return buildDirectoryCypher(language, query, ids, params)
	case "File":
		return buildFileCypher(language, query, repoID, params, access)
	default:
		return buildEntityCypherWithSemanticFilter(
			language,
			label,
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
// in the given language, counted per repository.
func buildRepositoryCypher(language, query, repoID string, limit int, access querycontract.RepositoryAccessFilter) (string, map[string]any) {
	params := map[string]any{
		"languages": graphLanguageSpellings(language),
		"limit":     limit,
	}

	cypher := `
		MATCH (r:Repository)-[:REPO_CONTAINS]->(f:File)
		WHERE f.language IN $languages
	`

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
// given language, seeking each granted repository's directories by an indexed
// `repo_id` rather than walking to a Repository node at all.
//
// This is the #6541 shape, and both halves of it are forced by measurement.
//
// The seek is why it is fast. The statement it replaced walked
// `(f:File)<-[:CONTAINS]-(d:Directory)<-[:REPO_CONTAINS|CONTAINS*]-(r:Repository)`
// and cost 34.5s for a caller granted one repository and 2m01s for one granted
// fifty on the issue's 50-repository corpus, against a published 800ms budget
// and a 10s production deadline. An inline property inside a MATCH pattern is
// served by an index seek on this build while the identical predicate in a
// WHERE is not: the same aggregation measured 53ms against 4.964s. The seek
// needs the directory_repo_id index (go/internal/graph/schema_tables_indexes.go)
// to be a seek at all.
//
// The MISSING Repository join is why it is correct. Projecting `r.name` needs a
// second pattern, and on BOTH the v1.2.1 pin and v1.3.1 every way of adding one
// is wrong: a trailing `MATCH (r:Repository {id: d.repo_id})` after the
// aggregation returns the literal STRING "r.name" in that column once the
// statement carries either this WHERE or this RETURN's `labels()` and aliases,
// a comma two-part pattern collapses the grouping, and `WITH ... ORDER BY ...
// LIMIT` followed by a MATCH drops the LIMIT. So `repo_name` is filled by the
// handler instead, from a second bounded read keyed on the `repo_id` this
// statement projects (Handler.directoryRepositoryNames). The projected columns
// are unchanged.
//
// The row bound is NOT self-sufficient either, and the handler finishes it:
// this build applies `ORDER BY ... LIMIT` once per UNWOUND id rather than to
// the whole result, so the statement can return up to limit x len(repoIDs)
// rows. sortAndTruncateDirectoryRows re-sorts and truncates, which is correct
// under both that behaviour and Neo4j's global LIMIT. The measurements are in
// docs/internal/evidence/6541-directory-query-s2.md and the live proof in
// TestLiveNornicDBDirectoryLanguageQueryCountsNestedDirectories.
//
// The ORDER BY therefore carries all three keys, and they are
// sortAndTruncateDirectoryRows's total order exactly. On `file_count DESC`
// alone the per-group bound breaks ties ARBITRARILY, and a row a group dropped
// cannot be recovered by re-sorting what survived, so page MEMBERSHIP would be
// backend-arbitrary whenever ties straddle the bound. Measured on a fixture of
// five directories per repository each holding one go file, at limit 4:
// `ORDER BY file_count DESC` alone retained a5,a3,a2,a1 on the v1.2.1 pin and
// a1,a2,a4,a3 on v1.3.1 -- two different pages from the same statement on the
// same data, neither of them the total order's top four. Ordering each group by
// the same total order makes the retained set determined (a1,a2,a3,a4 on both
// builds), and the union of the per-group top-L sets then contains the global
// top-L under that order, because a row inside the global top-L has at most
// L-1 rows before it globally and therefore at most L-1 before it inside its
// own group.
//
// The keys MUST be the RETURN aliases. Written as
// `ORDER BY file_count DESC, d.repo_id ASC, d.name ASC` the trailing keys are
// not honoured on either build -- the retained set stays arbitrary -- while the
// alias form above orders correctly on both. See
// docs/public/reference/nornicdb-path-predicate-pitfalls.md and the live pin
// TestLiveNornicDBDirectoryLanguageQueryBreaksTiesDeterministically.
//
// repoIDs MUST already be deduplicated. On both pinned builds a repeated id
// returns the same directory TWICE with its file_count intact, because
// everything after the UNWIND runs once per id; a backend that aggregates the
// whole result at once would instead count that directory's files twice. Either
// way the page is wrong. entity_id and file_path stay null for every row,
// because the canonical projector writes neither `d.id` nor `d.relative_path`;
// that predates this change and is not fixed here.
func buildDirectoryCypher(language, query string, repoIDs []string, params map[string]any) (string, map[string]any) {
	params["languages"] = graphLanguageSpellings(language)
	params["repo_ids"] = repoIDs

	cypher := `
		UNWIND $repo_ids AS rid
		MATCH (d:Directory {repo_id: rid})-[:CONTAINS]->(f:File)
		WHERE f.language IN $languages
	`

	if query != "" {
		cypher += " AND d.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		WITH d, count(f) as file_count
		RETURN d.id as entity_id, d.name as name, labels(d) as labels,
		       d.relative_path as file_path,
		       d.repo_id as repo_id,
		       file_count
		ORDER BY file_count DESC, repo_id ASC, name ASC
		LIMIT $limit
	`
	return cypher, params
}

// buildFileCypher returns a query for files in the given language.
func buildFileCypher(language, query, repoID string, params map[string]any, access querycontract.RepositoryAccessFilter) (string, map[string]any) {
	params["languages"] = graphLanguageSpellings(language)

	cypher := `
		MATCH (f:File)<-[:REPO_CONTAINS]-(r:Repository)
		WHERE f.language IN $languages
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

// buildEntityCypherWithSemanticFilter returns a query for semantic entities of
// one label in the given language. The language is read from the entity first
// and from its file second, because a few entity kinds are projected without
// their own `language` while their File always carries one.
func buildEntityCypherWithSemanticFilter(
	language, label, query, repoID string,
	params map[string]any,
	semanticFilterKey string,
	semanticFilterValue string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params["languages"] = graphLanguageSpellings(language)

	cypher := fmt.Sprintf(`
		MATCH (e:%s)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		WHERE (e.language IN $languages OR f.language IN $languages)
	`, label)

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
