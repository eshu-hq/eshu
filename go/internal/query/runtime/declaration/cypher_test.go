// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func normalized(cypher string) string { return querycontract.NormalizeCypherWhitespace(cypher) }

func TestPageStatementWithoutFilters(t *testing.T) {
	t.Parallel()

	stmt := pageStatement(request{limit: 50}, querycontract.RepositoryAccessFilter{AllScopes: true}, 51)
	cypher := normalized(stmt.cypher)
	for _, want := range []string{
		"MATCH (d:RuntimeDeclaration)",
		"ORDER BY d.repo_id, d.relative_path, d.line_number, d.runtime, d.order_index",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("cypher missing %q:\n%s", want, cypher)
		}
	}
	for _, banned := range []string{"WHERE", "$runtime", "$repo_ids", "allowed_repository_ids", "$cursor_"} {
		if strings.Contains(cypher, banned) {
			t.Errorf("unfiltered cypher holds %q:\n%s", banned, cypher)
		}
	}
	if stmt.params["limit"] != 51 {
		t.Errorf("limit param = %v, want 51", stmt.params["limit"])
	}
}

func TestPageStatementMajorBelowKeepsIndexRangeSeek(t *testing.T) {
	t.Parallel()

	req := request{runtime: "node", sourceKind: "nvmrc", repoIDs: []string{"repository:r_a"}, bounds: []int{20}, limit: 50}
	stmt := pageStatement(req, querycontract.RepositoryAccessFilter{AllScopes: true}, 51)
	cypher := normalized(stmt.cypher)
	for _, want := range []string{
		"d.runtime = $runtime", "d.source_kind = $source_kind", "d.repo_id IN $repo_ids",
		"d.version_major < $below_0",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("cypher missing %q:\n%s", want, cypher)
		}
	}
	if stmt.params["runtime"] != "node" || stmt.params["below_0"] != 20 || stmt.params["source_kind"] != "nvmrc" {
		t.Errorf("params = %v", stmt.params)
	}
}

func TestPageStatementVersionBelowComparesEveryGivenPart(t *testing.T) {
	t.Parallel()

	two := normalized(pageStatement(request{runtime: "node", bounds: []int{20, 10}}, querycontract.RepositoryAccessFilter{AllScopes: true}, 51).cypher)
	if !strings.Contains(two, "d.version_major <= $below_0") {
		t.Errorf("two-part bound drops the index-range hint:\n%s", two)
	}
	if !strings.Contains(two, "(d.version_major = $below_0 AND d.version_minor < $below_1)") {
		t.Errorf("two-part bound does not compare minor:\n%s", two)
	}
	if where, _, _ := strings.Cut(two, "RETURN"); strings.Contains(where, "version_patch") {
		t.Errorf("two-part bound touches patch:\n%s", where)
	}
	three := normalized(pageStatement(request{runtime: "node", bounds: []int{20, 10, 3}}, querycontract.RepositoryAccessFilter{AllScopes: true}, 51).cypher)
	if !strings.Contains(three, "d.version_minor = $below_1 AND d.version_patch < $below_2") {
		t.Errorf("three-part bound does not compare patch:\n%s", three)
	}
}

func TestPageStatementKeysetCursorWalksTheFiveColumnKey(t *testing.T) {
	t.Parallel()

	key := cursorKey{Version: cursorVersion, RepoID: "repository:r_a", RelativePath: ".nvmrc", LineNumber: 1, Runtime: "node", OrderIndex: 0}
	stmt := pageStatement(request{cursor: &key}, querycontract.RepositoryAccessFilter{AllScopes: true}, 51)
	cypher := normalized(stmt.cypher)
	for _, want := range []string{
		"d.repo_id > $cursor_repo_id",
		"d.repo_id = $cursor_repo_id AND d.relative_path > $cursor_relative_path",
		"d.relative_path = $cursor_relative_path AND d.line_number > $cursor_line_number",
		"d.line_number = $cursor_line_number AND d.runtime > $cursor_runtime",
		"d.runtime = $cursor_runtime AND d.order_index > $cursor_order_index",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("cursor predicate missing %q:\n%s", want, cypher)
		}
	}
	if stmt.params["cursor_repo_id"] != "repository:r_a" || stmt.params["cursor_order_index"] != 0 {
		t.Errorf("cursor params = %v", stmt.params)
	}
}

func TestPageStatementScopedCallerFiltersBeforeLimit(t *testing.T) {
	t.Parallel()

	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repository:r_a"}}
	stmt := pageStatement(request{runtime: "node"}, access, 51)
	cypher := normalized(stmt.cypher)
	grant := strings.Index(cypher, "d.repo_id IN $allowed_repository_ids")
	limit := strings.Index(cypher, "LIMIT $limit")
	if grant < 0 || limit < 0 || grant > limit {
		t.Fatalf("grant predicate must precede LIMIT:\n%s", cypher)
	}
	if !querycontract.GraphParamContains(stmt.params, "allowed_repository_ids", "repository:r_a") {
		t.Errorf("grant params not bound: %v", stmt.params)
	}
}

func TestExcludedStatementSelectsRowsTheComparisonCannotDecide(t *testing.T) {
	t.Parallel()

	one := normalized(excludedStatement(request{runtime: "node", bounds: []int{20}}, querycontract.RepositoryAccessFilter{AllScopes: true}).cypher)
	for _, want := range []string{"d.runtime = $runtime", "coalesce(d.version_major, -1) = -1", "AS excluded_count"} {
		if !strings.Contains(one, want) {
			t.Errorf("excluded cypher missing %q:\n%s", want, one)
		}
	}
	if strings.Contains(one, "version_minor") {
		t.Errorf("a major-only comparison cannot be undecidable on minor:\n%s", one)
	}
	// A bare IS NULL on a version part matches present values on NornicDB
	// (golden-corpus differential 2026-10-26); the coalesce form above is
	// load-bearing, so any reintroduction must fail here, not in CI.
	if strings.Contains(one, "IS NULL") {
		t.Errorf("excluded cypher must test absent versions with coalesce, not bare IS NULL:\n%s", one)
	}
	three := normalized(excludedStatement(request{runtime: "node", bounds: []int{20, 10, 3}}, querycontract.RepositoryAccessFilter{AllScopes: true}).cypher)
	for _, want := range []string{
		"(d.version_major = $below_0 AND coalesce(d.version_minor, -1) = -1)",
		"(d.version_major = $below_0 AND d.version_minor = $below_1 AND coalesce(d.version_patch, -1) = -1)",
	} {
		if !strings.Contains(three, want) {
			t.Errorf("excluded cypher missing %q:\n%s", want, three)
		}
	}
	if strings.Contains(three, "$cursor_") || strings.Contains(three, "LIMIT") {
		t.Errorf("excluded count must cover the whole filter, not a page:\n%s", three)
	}
	if strings.Contains(three, "IS NULL") {
		t.Errorf("excluded cypher must test absent versions with coalesce, not bare IS NULL:\n%s", three)
	}
}
