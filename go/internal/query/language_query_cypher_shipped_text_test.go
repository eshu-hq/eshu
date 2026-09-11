// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestLanguageQueryBuildersBindTheGrantInTheShippedCypher is the same guard for
// the four Cypher builders behind buildLanguageCypherWithSemanticFilter. The
// condition has to appear in the anchoring MATCH's own WHERE, ahead of every
// WITH, ORDER BY and LIMIT.
func TestLanguageQueryBuildersBindTheGrantInTheShippedCypher(t *testing.T) {
	t.Parallel()

	scoped := repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	want := "(r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids)"

	for _, label := range []string{"Repository", "Directory", "File", "Function"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			cypher, params := buildLanguageCypherWithSemanticFilter("go", label, "", "", 50, "", "", scoped)
			normalized := querycontract.NormalizeCypherWhitespace(cypher)
			if !strings.Contains(normalized, want) {
				t.Fatalf("%s builder missing %q:\n%s", label, want, normalized)
			}
			if !slices.Contains(querytestutil.RepositoryGoverningPredicatesForAlias(cypher, "r"), want) {
				t.Fatalf("%s builder puts the grant outside the Repository binding's own WHERE, so it does not decide row membership:\n%s", label, normalized)
			}
			// The governing-predicates assertion above already proves the
			// grant sits in the anchoring MATCH's own WHERE, which is ahead
			// of any WITH. These pin the rest of the ordering directly.
			for _, clause := range []string{" RETURN ", " ORDER BY ", " LIMIT "} {
				at := strings.Index(normalized, clause)
				if at >= 0 && strings.Index(normalized, want) > at {
					t.Fatalf("%s builder emits the grant after %q, so the page is taken before the grant applies:\n%s", label, strings.TrimSpace(clause), normalized)
				}
			}
			if got, ok := params["allowed_repository_ids"].([]string); !ok || !slices.Equal(got, []string{codeGrantGrantedRepo}) {
				t.Fatalf("params[allowed_repository_ids] = %#v, want the caller's granted ids; an unbound parameter fails at execution", params["allowed_repository_ids"])
			}

			unscopedCypher, unscopedParams := buildLanguageCypher("go", label, "", "", 50)
			if strings.Contains(unscopedCypher, "$allowed_repository_ids") {
				t.Fatalf("%s builder carries a grant condition for an unscoped caller:\n%s", label, querycontract.NormalizeCypherWhitespace(unscopedCypher))
			}
			if _, ok := unscopedParams["allowed_repository_ids"]; ok {
				t.Fatalf("%s builder bound grant params for an unscoped caller: %#v", label, unscopedParams)
			}
		})
	}
}

// TestLanguageQueryUnscopedCypherTextIsFrozen is the byte-identity guard for
// the unscoped text of the four builders behind
// buildLanguageCypherWithSemanticFilter. TestLanguageQueryBuildersBindTheGrantInTheShippedCypher
// above pins the ABSENCE of grant artifacts from an unscoped statement, which a
// wholesale rewrite would pass; this one compares the entire statement,
// whitespace included.
//
// The grant work appended access.GraphPredicate("r") and
// access.GraphParams(params) to every builder and changed nothing else; both
// are empty for an unscoped caller, so the baselines carry no grant text.
//
// The four are frozen to text that has since moved for backend reasons, each
// measured on the pinned NornicDB build. buildDirectoryCypher's two MATCH
// clauses collapsed into one, because the two-clause shape drops every row
// there (the reasoning is on buildDirectoryCypher, the measurement in
// TestLiveNornicDBLanguageQueryDirectoryTwoClauseShapeReturnsNothing). Then
// #6546 replaced the language predicate of the Directory, File and entity
// builders -- two equalities OR-ed with one `f.name ENDS WITH` term per
// extension -- with `f.language IN $languages`, because ENDS WITH is true for
// every row of a multi-node MATCH on that build and admitted every file, and
// moved buildRepositoryCypher's two equalities onto the same spelling list so
// a `csharp` or `typescript` repository query reaches the `c_sharp` and `tsx`
// rows the parsers write. Each is frozen to its NEW text, so an accidental
// revert of any fails here.
//
// The shared semantic-metadata projection is spliced from
// graphSemanticMetadataProjection() rather than copied into the baseline: eight
// statements in this package share that helper, so a frozen copy would fail on
// every unrelated addition to the list while proving nothing about this route.
// Everything around it -- the MATCH, the WHERE, the RETURN's own columns, the
// ORDER BY and the LIMIT -- is frozen.
func TestLanguageQueryUnscopedCypherTextIsFrozen(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		label string
		want  string
	}{
		{label: "Repository", want: frozenUnscopedRepositoryCypher},
		{label: "Directory", want: frozenUnscopedDirectoryCypher},
		{label: "File", want: frozenUnscopedFileCypher},
		{
			label: "Function",
			want: frozenUnscopedEntityCypherHead +
				graphSemanticMetadataProjection() +
				frozenUnscopedEntityCypherTail,
		},
	} {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()

			got, _ := buildLanguageCypher("go", tc.label, "", "", 50)
			if got != tc.want {
				t.Fatalf("%s builder's unscoped text moved off its frozen baseline.\n got: %q\nwant: %q\n"+
					"An unscoped caller's statement is not supposed to change. If it must, "+
					"re-measure the route and update this baseline and the claims in "+
					"docs/internal/evidence/5167-code-family-batch-2.md and "+
					"docs/internal/evidence/6546-language-query-extension-filter.md together.", tc.label, got, tc.want)
			}
		})
	}
}

// frozenCypherLines joins a frozen baseline's lines with "\n". The baselines
// are written one quoted line at a time rather than as a single raw literal
// because these statements carry lines holding nothing but a tab -- the seam
// where the builder concatenates two raw literals -- and a raw literal
// reproducing them byte for byte is trailing whitespace that
// `git diff --check` rejects.
func frozenCypherLines(lines ...string) string {
	return strings.Join(lines, "\n")
}

var frozenUnscopedRepositoryCypher = frozenCypherLines(
	"",
	"\t\tMATCH (r:Repository)-[:REPO_CONTAINS]->(f:File)",
	"\t\tWHERE f.language IN $languages",
	"\t",
	"\t\tWITH r, count(f) as file_count",
	"\t\tRETURN r.id as id, r.name as name,",
	"\t\t       coalesce(r.local_path, r.path) as local_path,",
	"\t\t       r.remote_url as remote_url,",
	"\t\t       file_count",
	"\t\tORDER BY file_count DESC",
	"\t\tLIMIT $limit",
	"\t",
)

var frozenUnscopedDirectoryCypher = frozenCypherLines(
	"",
	"\t\tMATCH (f:File)<-[:CONTAINS]-(d:Directory)<-[:REPO_CONTAINS|CONTAINS*]-(r:Repository)",
	"\t\tWHERE f.language IN $languages",
	"\t",
	"\t\tWITH d, r, count(f) as file_count",
	"\t\tRETURN d.id as entity_id, d.name as name, labels(d) as labels,",
	"\t\t       d.relative_path as file_path,",
	"\t\t       r.id as repo_id, r.name as repo_name,",
	"\t\t       file_count",
	"\t\tORDER BY file_count DESC",
	"\t\tLIMIT $limit",
	"\t",
)

var frozenUnscopedFileCypher = frozenCypherLines(
	"",
	"\t\tMATCH (f:File)<-[:REPO_CONTAINS]-(r:Repository)",
	"\t\tWHERE f.language IN $languages",
	"\t",
	"\t\tRETURN f.id as entity_id, f.name as name, labels(f) as labels,",
	"\t\t       f.relative_path as file_path,",
	"\t\t       r.id as repo_id, r.name as repo_name,",
	"\t\t       f.language as language",
	"\t\tORDER BY f.relative_path",
	"\t\tLIMIT $limit",
	"\t",
)

var frozenUnscopedEntityCypherHead = frozenCypherLines(
	"",
	"\t\tMATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)",
	"\t\tWHERE (e.language IN $languages OR f.language IN $languages)",
	"\t",
	"\t\tRETURN e.id as entity_id, e.name as name, labels(e) as labels,",
	"\t\t       f.relative_path as file_path,",
	"\t\t       r.id as repo_id, r.name as repo_name,",
	"\t\t       coalesce(e.language, f.language) as language,",
	"\t\t       e.start_line as start_line, e.end_line as end_line,",
	"",
)

var frozenUnscopedEntityCypherTail = frozenCypherLines(
	"",
	"\t\tORDER BY f.relative_path, e.name",
	"\t\tLIMIT $limit",
	"\t",
)
