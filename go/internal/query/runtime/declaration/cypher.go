// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// statement is one Cypher text and its parameters.
type statement struct {
	cypher string
	params map[string]any
}

// pageReturn projects exactly the RuntimeDeclaration properties the response
// carries, as direct node properties. The key order is the page order and the
// keyset cursor order: it is the five-part graph key the
// runtime_declaration_identity constraint enforces (#8816).
const pageReturn = `RETURN d.repo_id AS repo_id, d.relative_path AS relative_path, d.scope_dir AS scope_dir,
       d.line_number AS line_number, d.order_index AS order_index, d.runtime AS runtime,
       d.source_kind AS source_kind, d.tool_name AS tool_name, d.raw_spec AS raw_spec,
       d.spec_kind AS spec_kind, d.version_major AS version_major, d.version_minor AS version_minor,
       d.version_patch AS version_patch, d.distribution AS distribution,
       d.range_lower AS range_lower, d.range_upper AS range_upper,
       d.range_lower_major AS range_lower_major, d.range_upper_major AS range_upper_major,
       d.range_lower_inclusive AS range_lower_inclusive, d.range_upper_inclusive AS range_upper_inclusive
ORDER BY d.repo_id, d.relative_path, d.line_number, d.runtime, d.order_index
LIMIT $limit`

// pageStatement builds the page read. fetch is the limit plus the one-row
// truncation probe. Filters are assembled into the WHERE clause only when the
// caller sent them, never as `$x IS NULL OR ...`, so the planner can seek the
// runtime_declaration_runtime_major range index on (runtime, version_major)
// instead of falling back to a label scan.
func pageStatement(req request, access querycontract.RepositoryAccessFilter, fetch int) statement {
	params := access.GraphParams(map[string]any{"limit": fetch})
	conds := baseConditions(req, access, params)
	if req.compares() {
		conds = append(conds, decidedBelowConditions(len(req.bounds))...)
		bindBounds(req, params)
	}
	if req.cursor != nil {
		conds = append(conds, cursorCondition())
		params["cursor_repo_id"] = req.cursor.RepoID
		params["cursor_relative_path"] = req.cursor.RelativePath
		params["cursor_line_number"] = req.cursor.LineNumber
		params["cursor_runtime"] = req.cursor.Runtime
		params["cursor_order_index"] = req.cursor.OrderIndex
	}
	return statement{cypher: "MATCH (d:RuntimeDeclaration)\n" + whereClause(conds) + pageReturn, params: params}
}

// excludedStatement counts, by spec_kind, the declarations the version
// comparison cannot decide: rows with no resolved major (range, alias,
// unresolved) and rows whose declared precision stops short of the bound (a
// partial "20" against version_below 20.10). It applies the same runtime,
// source_kind, repo_ids and grant filters as the page and covers the whole
// filter, not one page, so every page names the same exclusions.
func excludedStatement(req request, access querycontract.RepositoryAccessFilter) statement {
	params := access.GraphParams(map[string]any{})
	conds := baseConditions(req, access, params)
	bindBounds(req, params)
	conds = append(conds, "("+strings.Join(undecidedConditions(len(req.bounds)), " OR ")+")")
	return statement{
		cypher: "MATCH (d:RuntimeDeclaration)\n" + whereClause(conds) +
			"RETURN d.spec_kind AS spec_kind, count(d) AS excluded_count",
		params: params,
	}
}

// baseConditions renders the filters shared by the page and the excluded
// count, binding their parameters into params.
func baseConditions(req request, access querycontract.RepositoryAccessFilter, params map[string]any) []string {
	var conds []string
	if req.runtime != "" {
		conds = append(conds, "d.runtime = $runtime")
		params["runtime"] = req.runtime
	}
	if req.sourceKind != "" {
		conds = append(conds, "d.source_kind = $source_kind")
		params["source_kind"] = req.sourceKind
	}
	if len(req.repoIDs) > 0 {
		conds = append(conds, "d.repo_id IN $repo_ids")
		params["repo_ids"] = req.repoIDs
	}
	if access.Scoped() {
		// The grant binds before the LIMIT, so a page never fills with rows the
		// caller cannot see and then loses them to a post-filter.
		conds = append(conds, access.GraphConditionOnProperty("d", "repo_id"))
	}
	return conds
}

func bindBounds(req request, params map[string]any) {
	for i, bound := range req.bounds {
		params["below_"+strconv.Itoa(i)] = bound
	}
}

func whereClause(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conds, "\n  AND ") + "\n"
}

// decidedBelowConditions renders "the declared version is below the bound" for
// a bound of n components. A part the declaration does not carry (a partial
// "20" has no minor) makes the comparison null, so the row is not returned
// here and is counted by undecidedConditions instead.
//
// For more than one component a redundant `d.version_major <= $below_0` keeps
// the index range seek on version_major; the OR below it would otherwise leave
// the planner a filter over the whole runtime.
func decidedBelowConditions(n int) []string {
	switch n {
	case 1:
		return []string{"d.version_major < $below_0"}
	case 2:
		return []string{
			"d.version_major <= $below_0",
			"(d.version_major < $below_0 OR (d.version_major = $below_0 AND d.version_minor < $below_1))",
		}
	default:
		return []string{
			"d.version_major <= $below_0",
			"(d.version_major < $below_0 OR (d.version_major = $below_0 AND (d.version_minor < $below_1 OR (d.version_minor = $below_1 AND d.version_patch < $below_2))))",
		}
	}
}

// undecidedConditions renders the disjuncts for a declaration the comparison
// cannot place, for a bound of n components.
//
// The absent-version test is `coalesce(part, -1) = -1`, not a bare
// `part IS NULL`. Version parts are non-negative or absent by construction of
// the single git-core producer's digits-only parser (classify.go parseVersion:
// ASCII-digit scan, Atoi-or-reject, so no negative part is emitted), which is
// what makes the two forms canonically equivalent; the payload schema itself
// types parts integer-or-null with no minimum, so a second producer or a
// hand-built fact carrying -1 would be double-counted — see eshu-hq/eshu#9147
// for the schema-minimum-or-quarantine follow-up. The observed divergence is
// specific to the bare form (golden-corpus differential 2026-10-26, run
// 40254280683 failed differential job: identical bounded reads, digest
// 64a3e2d212f5, on both backends in both pairings, yet the bare-form excluded
// count read 1 row on NornicDB, digest 7f5ea56384b5, against 0 rows on Neo4j,
// empty-set digest e3b0c44298fc). Under the coalesce form both backends agree
// (run 40265520080: 1 row, identical digest a67d594a283b on both backends in
// both pairings). Divergence is filed as eshu-hq/eshu#9145. The -1 sentinel
// keeps the test a pure scalar comparison; the sargable `part = $below_N`
// conjuncts beside it keep their index seeks.
func undecidedConditions(n int) []string {
	conds := []string{"coalesce(d.version_major, -1) = -1"}
	if n >= 2 {
		conds = append(conds, "(d.version_major = $below_0 AND coalesce(d.version_minor, -1) = -1)")
	}
	if n >= 3 {
		conds = append(conds, "(d.version_major = $below_0 AND d.version_minor = $below_1 AND coalesce(d.version_patch, -1) = -1)")
	}
	return conds
}

// cursorCondition renders the keyset predicate over the five-part key,
// strictly after the cursor row. Tuple comparison does not exist in Cypher, so
// it is the standard OR chain, one disjunct per column.
func cursorCondition() string {
	return `(d.repo_id > $cursor_repo_id
    OR (d.repo_id = $cursor_repo_id AND d.relative_path > $cursor_relative_path)
    OR (d.repo_id = $cursor_repo_id AND d.relative_path = $cursor_relative_path AND d.line_number > $cursor_line_number)
    OR (d.repo_id = $cursor_repo_id AND d.relative_path = $cursor_relative_path AND d.line_number = $cursor_line_number AND d.runtime > $cursor_runtime)
    OR (d.repo_id = $cursor_repo_id AND d.relative_path = $cursor_relative_path AND d.line_number = $cursor_line_number AND d.runtime = $cursor_runtime AND d.order_index > $cursor_order_index))`
}
