// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

const (
	secretLinesSelectPrefix = "SELECT s.repo_id, s.relative_path, s.language, s.line_number, s.line_text, s.finding_kind " +
		"FROM content_file_secret_lines s WHERE TRUE"
	secretLinesOrderSuffix = "ORDER BY s.repo_id, s.relative_path, s.line_number, s.finding_kind"
)

// TestHardcodedSecretInvestigationQueryShapePerBranch pins the exact per-branch
// statement the side-table reader builds (#7125): Go assembles the WHERE, so no
// "$n OR" generic-plan coupling reaches the planner, and the ORDER BY is the
// side table's primary-key order plus the inert finding_kind tiebreak.
func TestHardcodedSecretInvestigationQueryShapePerBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       codequery.HardcodedSecretInvestigationRequest
		wantWhere string
		wantArgs  []any
	}{
		{
			name:      "unscoped default hides suppressed rows",
			req:       codequery.HardcodedSecretInvestigationRequest{Limit: 26},
			wantWhere: "AND NOT s.suppressed",
			wantArgs:  []any{26, 0},
		},
		{
			name:      "include suppressed drops the predicate",
			req:       codequery.HardcodedSecretInvestigationRequest{Limit: 26, Offset: 50, IncludeSuppressed: true},
			wantWhere: "",
			wantArgs:  []any{26, 50},
		},
		{
			name:      "repo scope is trimmed and filtered before limit",
			req:       codequery.HardcodedSecretInvestigationRequest{RepoID: " repo-1 ", Limit: 5},
			wantWhere: "AND s.repo_id = $1 AND NOT s.suppressed",
			wantArgs:  []any{"repo-1", 5, 0},
		},
		{
			name: "grant scope applies when no repo is named",
			req: codequery.HardcodedSecretInvestigationRequest{
				AllowedRepositoryIDs: []string{"repo-1", "repo-2"}, Limit: 5,
			},
			wantWhere: "AND s.repo_id = ANY($1) AND NOT s.suppressed",
			wantArgs:  []any{array.Of([]string{"repo-1", "repo-2"}), 5, 0},
		},
		{
			name: "repo wins over grant",
			req: codequery.HardcodedSecretInvestigationRequest{
				RepoID: "repo-1", AllowedRepositoryIDs: []string{"repo-2"}, Limit: 5,
			},
			wantWhere: "AND s.repo_id = $1 AND NOT s.suppressed",
			wantArgs:  []any{"repo-1", 5, 0},
		},
		{
			name:      "language is trimmed against the coalesced column",
			req:       codequery.HardcodedSecretInvestigationRequest{Language: " go ", Limit: 5},
			wantWhere: "AND s.language = $1 AND NOT s.suppressed",
			wantArgs:  []any{"go", 5, 0},
		},
		{
			name: "finding kinds use the unit separator array",
			req: codequery.HardcodedSecretInvestigationRequest{
				FindingKinds: []string{"api_token", "slack_token"}, Limit: 5,
			},
			wantWhere: "AND s.finding_kind = ANY(string_to_array($1, E'\\x1f')) AND NOT s.suppressed",
			wantArgs:  []any{"api_token\x1fslack_token", 5, 0},
		},
		{
			name: "every filter together keeps argument order",
			req: codequery.HardcodedSecretInvestigationRequest{
				RepoID: "repo-1", Language: "python", FindingKinds: []string{"secret_literal"},
				IncludeSuppressed: true, Limit: 7, Offset: 3,
			},
			wantWhere: "AND s.repo_id = $1 AND s.language = $2 AND s.finding_kind = ANY(string_to_array($3, E'\\x1f'))",
			wantArgs:  []any{"repo-1", "python", "secret_literal", 7, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, args := hardcodedSecretInvestigationQuery(tt.req)
			got := collapseSQLSpace(query)
			limitArg := len(tt.wantArgs) - 1 // 1-based index of the LIMIT placeholder
			want := collapseSQLSpace(secretLinesSelectPrefix + " " + tt.wantWhere + " " + secretLinesOrderSuffix +
				" LIMIT $" + strconv.Itoa(limitArg) + " OFFSET $" + strconv.Itoa(limitArg+1))
			if got != want {
				t.Fatalf("query =\n%s\nwant\n%s", got, want)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tt.wantArgs)
			}
		})
	}
}

// TestHardcodedSecretInvestigationQueryNeverScansContent is the hermetic
// regression guard against returning to the corpus scan: the read must touch
// only the side table (#7125).
func TestHardcodedSecretInvestigationQueryNeverScansContent(t *testing.T) {
	t.Parallel()

	for _, req := range []codequery.HardcodedSecretInvestigationRequest{
		{Limit: 26},
		{RepoID: "repo-1", Language: "go", FindingKinds: []string{"api_token"}, IncludeSuppressed: true, Limit: 5},
		{AllowedRepositoryIDs: []string{"repo-1"}, Limit: 5},
	} {
		query, _ := hardcodedSecretInvestigationQuery(req)
		for _, forbidden := range []string{"content_files", "regexp_split_to_table", "~*", "candidate_files", "candidate_lines", "content_file_references"} {
			if strings.Contains(query, forbidden) {
				t.Fatalf("query references %q, which reintroduces the corpus scan:\n%s", forbidden, query)
			}
		}
		if !strings.Contains(query, "FROM content_file_secret_lines s") {
			t.Fatalf("query does not read the side table:\n%s", query)
		}
	}
}
