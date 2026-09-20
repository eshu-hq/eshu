// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
		want []Rule
	}{
		{
			name: "single owner",
			body: "*.go @octocat\n",
			want: []Rule{{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0}},
		},
		{
			name: "multiple owners",
			body: "/services/payments/ @org/payments-team @octocat\n",
			want: []Rule{{
				Pattern:    "/services/payments/",
				Owners:     []string{"@org/payments-team", "@octocat"},
				OrderIndex: 0,
			}},
		},
		{
			name: "blank lines are skipped",
			body: "*.go @octocat\n\n\n*.md @writer\n",
			want: []Rule{
				{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0},
				{Pattern: "*.md", Owners: []string{"@writer"}, OrderIndex: 1},
			},
		},
		{
			name: "whitespace-only line is skipped",
			body: "*.go @octocat\n   \t  \n*.md @writer\n",
			want: []Rule{
				{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0},
				{Pattern: "*.md", Owners: []string{"@writer"}, OrderIndex: 1},
			},
		},
		{
			name: "full-line comment is skipped",
			body: "# top-level owners\n*.go @octocat\n",
			want: []Rule{{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0}},
		},
		{
			name: "indented comment is skipped",
			body: "   # indented comment\n*.go @octocat\n",
			want: []Rule{{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0}},
		},
		{
			name: "inline extra whitespace between tokens is collapsed",
			body: "*.go    @octocat   @org/team\n",
			want: []Rule{{
				Pattern:    "*.go",
				Owners:     []string{"@octocat", "@org/team"},
				OrderIndex: 0,
			}},
		},
		{
			name: "leading and trailing whitespace on a rule line is trimmed",
			body: "  *.go @octocat  \n",
			want: []Rule{{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0}},
		},
		{
			name: "user org-team and email owner tokens are carried verbatim",
			body: "*.go @octocat @org/team-name docs@example.com\n",
			want: []Rule{{
				Pattern:    "*.go",
				Owners:     []string{"@octocat", "@org/team-name", "docs@example.com"},
				OrderIndex: 0,
			}},
		},
		{
			name: "pattern-only line with zero owners is dropped",
			body: "*.go @octocat\n*.md\n*.rb @writer\n",
			want: []Rule{
				{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0},
				// *.md has no owners, so it carries no ownership claim and is
				// not emitted. OrderIndex is the ordinal among EMITTED rules,
				// so *.rb becomes index 1, not 2.
				{Pattern: "*.rb", Owners: []string{"@writer"}, OrderIndex: 1},
			},
		},
		{
			name: "section header line is skipped",
			body: "[Section-name]\n*.go @octocat\n",
			want: []Rule{{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0}},
		},
		{
			name: "optional section header with caret prefix is skipped",
			body: "^[Section-name][2]\n*.go @octocat\n",
			want: []Rule{{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0}},
		},
		{
			name: "section header with default owners on the same line is skipped as a non-rule",
			body: "[Section-name] @default-owner\n*.go @octocat\n",
			want: []Rule{{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0}},
		},
		{
			name: "crlf line endings are handled",
			body: "*.go @octocat\r\n*.md @writer\r\n",
			want: []Rule{
				{Pattern: "*.go", Owners: []string{"@octocat"}, OrderIndex: 0},
				{Pattern: "*.md", Owners: []string{"@writer"}, OrderIndex: 1},
			},
		},
		{
			name: "empty body yields no rules",
			body: "",
			want: nil,
		},
		{
			name: "only comments and blank lines yields no rules",
			body: "# just a comment\n\n   \n",
			want: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := Parse(testCase.body)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", testCase.body, got, testCase.want)
			}
		})
	}
}
