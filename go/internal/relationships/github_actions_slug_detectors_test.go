// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "testing"

// TestReusableWorkflowRepoRefMatchesPreConsolidationBehavior is issue #5526's
// differential proof for reusableWorkflowRepoRef: every want value below was
// captured by running the pre-#5526 standalone implementation this function
// used to contain (before it became a one-line delegate to
// ghactionsref.ReusableWorkflowRepo) over this exact input table. A failure
// here means the delegated path silently diverged from the edge-target slug
// this package emitted before the refactor.
func TestReusableWorkflowRepoRefMatchesPreConsolidationBehavior(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "remote reusable workflow", value: "myorg/deployment-helm/.github/workflows/deploy.yaml@main", want: "myorg/deployment-helm"},
		{name: "remote reusable workflow, another repo", value: "octo-org/octo-repo/.github/workflows/build.yml@main", want: "octo-org/octo-repo"},
		{name: "bare .github with no further path segment", value: "owner/repo/.github@ref", want: "owner/repo"},
		{name: "path not under .github is rejected", value: "owner/repo/somepath/build.yml@main", want: ""},
		{name: "local reusable workflow (./ prefix)", value: "./.github/workflows/build.yml@main", want: ""},
		{name: "./-prefixed value whose remainder is not .github", value: "./repo/.github/workflows/build.yml@main", want: ""},
		{name: "bare owner/repo with no path segment", value: "owner/repo@main", want: ""},
		{name: "blank", value: "", want: ""},
		{name: "whitespace only", value: "   ", want: ""},
		{name: "bare .github path with no owner/repo", value: ".github/workflows/verify.yaml@main", want: ""},
		{name: "false .github prefix trap (.githubfoo)", value: "owner/repo/.githubfoo/workflows/x.yml@main", want: ""},
		{name: "two-segment action ref is not reusable-workflow-shaped", value: "actions/checkout@v4", want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reusableWorkflowRepoRef(tc.value); got != tc.want {
				t.Errorf("reusableWorkflowRepoRef(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestGithubActionsActionRepoRefMatchesPreConsolidationBehavior is issue
// #5526's differential proof for githubActionsActionRepoRef, INCLUDING the
// preserved ref-retention quirk for a plain two-segment "owner/repo@ref"
// value (want carries the "@ref" suffix verbatim -- see
// ghactionsref.ActionRepo's doc comment for why this is kept, not fixed, by
// a behavior-preserving refactor). Every want value was captured by running
// the pre-#5526 standalone implementation over this exact input table.
func TestGithubActionsActionRepoRefMatchesPreConsolidationBehavior(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "two-segment action ref keeps its @ref suffix -- preserved quirk",
			value: "hashicorp/setup-terraform@v3",
			want:  "hashicorp/setup-terraform@v3",
		},
		{
			name:  "another two-segment action ref, same preserved quirk",
			value: "peter-evans/create-pull-request@v5",
			want:  "peter-evans/create-pull-request@v5",
		},
		{name: "actions/checkout is excluded", value: "actions/checkout@v4", want: ""},
		{name: "local action (./ prefix) is excluded", value: "./.github/actions/local-helper", want: ""},
		{name: "docker action is excluded", value: "docker://alpine:3.18", want: ""},
		{
			name:  "reusable-workflow-shaped value is excluded",
			value: "myorg/deployment-helm/.github/workflows/deploy.yaml@main",
			want:  "",
		},
		{
			name:  "subdirectory (composite) action ref -- @ref lives in the trailing segment, slug stays clean",
			value: "owner/repo/path/to/action@v1",
			want:  "owner/repo",
		},
		{name: "bare action ref with no @ref at all", value: "octo-org/octo-action", want: "octo-org/octo-action"},
		{name: "blank", value: "", want: ""},
		{name: "bare .github/ prefix is excluded", value: ".github/actions/local", want: ""},
		{
			name:  "subdirectory action pinned to a full SHA -- slug still clean",
			value: "owner/repo/action.yml@sha1234567890123456789012345678901234567890",
			want:  "owner/repo",
		},
		{name: "single segment, no owner/repo at all", value: "owner-only", want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := githubActionsActionRepoRef(tc.value); got != tc.want {
				t.Errorf("githubActionsActionRepoRef(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestGithubActionsLocalReusableWorkflowPathMatchesPreConsolidationBehavior
// is issue #5526's differential proof for
// githubActionsLocalReusableWorkflowPath. Every want value was captured by
// running the pre-#5526 standalone implementation over this exact input
// table.
func TestGithubActionsLocalReusableWorkflowPathMatchesPreConsolidationBehavior(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "conventional ./ prefix", value: "./.github/workflows/release.yaml", want: ".github/workflows/release.yaml"},
		{name: "no ./ prefix -- GitHub accepts this shape too", value: ".github/workflows/verify.yaml@main", want: ".github/workflows/verify.yaml"},
		{
			name:  "remote reusable workflow shape is excluded",
			value: "owner/repo/.github/workflows/x.yml@main",
			want:  "",
		},
		{name: "blank", value: "", want: ""},
		{name: "path not under .github/workflows/ is excluded", value: "./workflows/x.yml", want: ""},
		{name: "leading slash is stripped", value: "/.github/workflows/x.yml@main", want: ".github/workflows/x.yml"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := githubActionsLocalReusableWorkflowPath(tc.value); got != tc.want {
				t.Errorf("githubActionsLocalReusableWorkflowPath(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
