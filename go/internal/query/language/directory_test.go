// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestBuildDirectoryCypherWalksNoVariableLengthChain pins the #6541 removal of
// the unbounded `<-[:REPO_CONTAINS|CONTAINS*]-` walk from the Directory
// builder. That walk is what made the route cost 34.5s for a caller granted one
// repository and 2m01s for one granted fifty on the 50-repository corpus
// measured on the issue, and every candidate that fixes it reaches a
// directory's repository through a fixed-length hop instead. Every caller class
// is checked, because the scoped, repository-anchored and unscoped statements
// are separate renderings of one builder.
func TestBuildDirectoryCypherWalksNoVariableLengthChain(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		repoID string
		access querycontract.RepositoryAccessFilter
	}{
		{name: "unscoped", access: querycontract.RepositoryAccessFilter{AllScopes: true}},
		{name: "unscoped repository-anchored", repoID: "repo-a", access: querycontract.RepositoryAccessFilter{AllScopes: true}},
		{name: "scoped one repository", access: querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}},
		{name: "scoped many repositories", access: querycontract.RepositoryAccessFilter{
			AllowedRepositoryIDs: []string{"repo-a", "repo-b"},
			AllowedScopeIDs:      []string{"scope-c"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cypher, _ := BuildCypherWithSemanticFilter(
				"go", "Directory", "", tc.repoID, 50, "", "", tc.access, nil,
			)
			if strings.Contains(cypher, "*") {
				t.Fatalf("Directory statement still walks a variable-length chain:\n%s", cypher)
			}
		})
	}
}

// TestBuildDirectoryCypherBindsTheGrantAsARepositoryIDList is the Directory
// half of TestLanguageQueryBuildersBindTheGrantInTheShippedCypher, which covers
// the three builders that bind a Repository node.
//
// This statement binds none, so its grant is the repository-id list it UNWINDs,
// and the guard is that the list admits exactly the repositories the replaced
// `r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` predicate
// admitted: the union of both grant lists, deduplicated. A duplicate is a
// correctness bug and not just waste: UNWIND visits a repeated id twice, which
// returns that repository's directories twice on the pinned builds and would
// double their file_counts on a backend that aggregates the whole result at
// once. TestLiveNornicDBDirectoryLanguageQueryRepeatsRowsForARepeatedID
// measures both halves against a live backend.
func TestBuildDirectoryCypherBindsTheGrantAsARepositoryIDList(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		repoID string
		access querycontract.RepositoryAccessFilter
		want   []string
	}{
		{
			name:   "one granted repository",
			access: querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}},
			want:   []string{"repo-a"},
		},
		{
			name: "repositories and scopes are unioned",
			access: querycontract.RepositoryAccessFilter{
				AllowedRepositoryIDs: []string{"repo-a", "repo-b"},
				AllowedScopeIDs:      []string{"scope-c"},
			},
			want: []string{"repo-a", "repo-b", "scope-c"},
		},
		{
			name: "an id granted twice is bound once",
			access: querycontract.RepositoryAccessFilter{
				AllowedRepositoryIDs: []string{"repo-a"},
				AllowedScopeIDs:      []string{"repo-a"},
				Allowed:              map[string]struct{}{"repo-a": {}},
			},
			want: []string{"repo-a"},
		},
		{
			name:   "an anchored repository inside the grant",
			repoID: "repo-a",
			access: querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a", "repo-b"}},
			want:   []string{"repo-a"},
		},
		{
			name:   "an anchored repository outside the grant matches nothing",
			repoID: "repo-z",
			access: querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}},
			want:   nil,
		},
		{
			name:   "an unscoped caller anchored to one repository",
			repoID: "repo-a",
			access: querycontract.RepositoryAccessFilter{AllScopes: true},
			want:   []string{"repo-a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cypher, params := BuildCypherWithSemanticFilter(
				"go", "Directory", "", tc.repoID, 50, "", "", tc.access, nil,
			)
			got, _ := params["repo_ids"].([]string)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("params[repo_ids] = %#v, want %#v", got, tc.want)
			}
			// The seek must be the anchoring MATCH's own inline property, not
			// a WHERE: on the pinned build only the inline form is served by
			// an index seek, which is the whole point of the rewrite.
			if !strings.Contains(cypher, "MATCH (d:Directory {repo_id: rid})") {
				t.Fatalf("Directory statement does not seek by an inline repo_id:\n%s", cypher)
			}
			if strings.Contains(cypher, "$allowed_repository_ids") || strings.Contains(cypher, "$allowed_scope_ids") {
				t.Fatalf("Directory statement still carries the replaced grant predicate:\n%s", cypher)
			}
			for _, bound := range []string{"allowed_repository_ids", "allowed_scope_ids"} {
				if _, ok := params[bound]; ok {
					t.Fatalf("Directory builder bound %q, which its statement never references: %#v", bound, params)
				}
			}
		})
	}
}

// TestBuildDirectoryCypherProjectsRepoIDNotRepoName pins the column swap the
// handler depends on. The statement cannot project repo_name on either pinned
// build, so it projects repo_id and directoryRepositoryNames resolves the name;
// a rewrite that restores r.name here would return the literal string "r.name"
// to callers rather than failing.
func TestBuildDirectoryCypherProjectsRepoIDNotRepoName(t *testing.T) {
	t.Parallel()

	cypher, _ := buildLanguageCypher("go", "Directory", "", "", 50)
	if !strings.Contains(cypher, "d.repo_id as repo_id") {
		t.Fatalf("Directory statement does not project repo_id, which the name lookup is keyed on:\n%s", cypher)
	}
	if strings.Contains(cypher, "repo_name") {
		t.Fatalf("Directory statement projects repo_name, which is the literal-string defect this shape exists to avoid:\n%s", cypher)
	}
	if strings.Contains(cypher, ":Repository") {
		t.Fatalf("Directory statement binds a Repository again:\n%s", cypher)
	}
}

// TestSortAndTruncateDirectoryRowsTakesTheGlobalTopN covers the half of the row
// bound the statement cannot do on the pinned build, which applies ORDER
// BY/LIMIT once per unwound repository id. The input below is what that
// produces: two per-repository groups, each already ordered and each already
// cut to the limit. The page must be the global top-N across both.
func TestSortAndTruncateDirectoryRowsTakesTheGlobalTopN(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"name": "inner", "repo_id": "repo-a", "file_count": 3},
		{"name": "src", "repo_id": "repo-a", "file_count": 2},
		{"name": "lib", "repo_id": "repo-b", "file_count": 4},
		{"name": "cmd", "repo_id": "repo-b", "file_count": 2},
	}

	got := sortAndTruncateDirectoryRows(rows, 2)

	if len(got) != 2 {
		t.Fatalf("kept %d row(s), want 2: %#v", len(got), got)
	}
	if name := querycontract.StringVal(got[0], "name"); name != "lib" {
		t.Fatalf("first row = %q, want \"lib\"; the page is not the global top-N", name)
	}
	if name := querycontract.StringVal(got[1], "name"); name != "inner" {
		t.Fatalf("second row = %q, want \"inner\"; the page is not the global top-N", name)
	}
}

// TestSortAndTruncateDirectoryRowsBreaksTiesDeterministically pins the
// repo_id-then-name tie-break. The replaced statement ordered on file_count
// alone, so two identical requests could return different pages.
func TestSortAndTruncateDirectoryRowsBreaksTiesDeterministically(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"name": "src", "repo_id": "repo-b", "file_count": 2},
		{"name": "zeta", "repo_id": "repo-a", "file_count": 2},
		{"name": "alpha", "repo_id": "repo-a", "file_count": 2},
	}

	got := sortAndTruncateDirectoryRows(rows, 0)

	want := []string{"alpha", "zeta", "src"}
	for i, name := range want {
		if got := querycontract.StringVal(got[i], "name"); got != name {
			t.Fatalf("row %d = %q, want %q (ties order by repo_id then name): %#v", i, got, name, rows)
		}
	}
}

// TestBuildDirectoryCypherOrdersOnTheHandlersTotalOrder pins the half of the
// #6541 review's F1 fix that lives in the statement.
//
// The backend applies ORDER BY/LIMIT once per UNWOUND id, so the statement's
// ORDER BY is what decides which rows each group KEEPS. On `file_count DESC`
// alone that choice is arbitrary among ties, and no amount of re-sorting in the
// handler can recover a row the backend never sent -- so the statement must
// order on exactly the keys sortAndTruncateDirectoryRows orders on, and it must
// name them as the RETURN aliases, which is the only spelling either pinned
// build honours. TestLiveNornicDBDirectoryLanguageQueryBreaksTiesDeterministically
// is the measurement behind both halves of that sentence.
func TestBuildDirectoryCypherOrdersOnTheHandlersTotalOrder(t *testing.T) {
	t.Parallel()

	cypher, _ := buildLanguageCypher("go", "Directory", "", "", 50)

	const wantOrder = "ORDER BY file_count DESC, repo_id ASC, name ASC"
	if !strings.Contains(cypher, wantOrder) {
		t.Fatalf("Directory statement does not carry %q; a per-group bound on file_count alone drops tied rows arbitrarily:\n%s",
			wantOrder, cypher)
	}
	// The property spelling is served as if the trailing keys were absent on
	// both pinned builds, so it must never come back.
	if strings.Contains(cypher, "d.repo_id ASC") || strings.Contains(cypher, "d.name ASC") {
		t.Fatalf("Directory statement orders on d.<property>, which neither pinned build honours:\n%s", cypher)
	}
	if !strings.Contains(cypher, wantOrder+"\n") && !strings.Contains(cypher, wantOrder+"\r\n") {
		t.Fatalf("the ORDER BY must be its own clause line ahead of LIMIT:\n%s", cypher)
	}
	if strings.Index(cypher, wantOrder) > strings.Index(cypher, "LIMIT $limit") {
		t.Fatalf("ORDER BY must precede LIMIT:\n%s", cypher)
	}
}

// TestBuildDirectoryCypherWithoutResolvedIDsMatchesNothing pins the exported
// dispatcher's stated contract (#6541 review F3): an unscoped caller that names
// no repository and supplies no resolved id list gets a statement that matches
// nothing, not every repository. It is the one caller class the pure builder
// cannot resolve, and the behaviour is documented on
// BuildCypherWithSemanticFilter rather than left to be discovered.
//
// Production never reaches it: Handler.languageQueryGraphRows routes Directory
// to Handler.directoryRowsByLanguage, which reads the ids first.
func TestBuildDirectoryCypherWithoutResolvedIDsMatchesNothing(t *testing.T) {
	t.Parallel()

	cypher, params := BuildCypherWithSemanticFilter(
		"go", "Directory", "", "", 50, "", "",
		querycontract.RepositoryAccessFilter{AllScopes: true}, nil,
	)

	ids, ok := params["repo_ids"].([]string)
	if ok && len(ids) != 0 {
		t.Fatalf("params[repo_ids] = %#v, want an empty list; an unresolved unscoped caller must not widen to every repository", ids)
	}
	if params["repo_ids"] == nil && !ok {
		t.Fatalf("params[repo_ids] missing entirely: %#v; the statement UNWINDs it, so it must be bound", params)
	}
	if !strings.Contains(cypher, "UNWIND $repo_ids AS rid") {
		t.Fatalf("Directory statement does not UNWIND the id list, so the empty-list contract above says nothing:\n%s", cypher)
	}
}
