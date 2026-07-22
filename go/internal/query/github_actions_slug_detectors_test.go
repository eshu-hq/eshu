// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestGithubActionsReusableWorkflowRepoRefMatchesPreConsolidationBehavior is
// issue #5526's differential proof for githubActionsReusableWorkflowRepoRef.
// Every want value was captured by running the pre-#5526 standalone
// implementation this function used to contain (before it became a one-line
// delegate to ghactionsref.ReusableWorkflowRepo) over this exact input table,
// including quoted-scalar shapes this package's YAML decode can still
// produce.
func TestGithubActionsReusableWorkflowRepoRefMatchesPreConsolidationBehavior(t *testing.T) {
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
		{
			name:  "single-quoted scalar",
			value: `'myorg/deployment-helm/.github/workflows/deploy.yaml@main'`,
			want:  "myorg/deployment-helm",
		},
		{
			name:  "double-quoted scalar",
			value: `"owner/repo/.github/workflows/x.yml@main"`,
			want:  "owner/repo",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := githubActionsReusableWorkflowRepoRef(tc.value); got != tc.want {
				t.Errorf("githubActionsReusableWorkflowRepoRef(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestGithubActionsActionRepositoryRefMatchesPreConsolidationBehavior is
// issue #5526's differential proof for githubActionsActionRepositoryRef.
// Unlike go/internal/relationships's sibling detector, this function has
// always returned a CLEAN, ref-free slug (want never carries "@ref") --
// every want value was captured by running the pre-#5526 standalone
// implementation over this exact input table.
func TestGithubActionsActionRepositoryRefMatchesPreConsolidationBehavior(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "two-segment action ref -- @ref stripped, unlike relationships' sibling detector",
			value: "hashicorp/setup-terraform@v3",
			want:  "hashicorp/setup-terraform",
		},
		{
			name:  "another two-segment action ref, same clean behavior",
			value: "peter-evans/create-pull-request@v5",
			want:  "peter-evans/create-pull-request",
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
			name:  "subdirectory (composite) action ref",
			value: "owner/repo/path/to/action@v1",
			want:  "owner/repo",
		},
		{name: "bare action ref with no @ref at all", value: "octo-org/octo-action", want: "octo-org/octo-action"},
		{name: "blank", value: "", want: ""},
		{name: "bare .github/ prefix is excluded", value: ".github/actions/local", want: ""},
		{
			name:  "subdirectory action pinned to a full SHA",
			value: "owner/repo/action.yml@sha1234567890123456789012345678901234567890",
			want:  "owner/repo",
		},
		{name: "single segment, no owner/repo at all", value: "owner-only", want: ""},
		{name: "single-quoted scalar", value: `'hashicorp/setup-terraform@v3'`, want: "hashicorp/setup-terraform"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := githubActionsActionRepositoryRef(tc.value); got != tc.want {
				t.Errorf("githubActionsActionRepositoryRef(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestGithubActionsLocalReusableWorkflowPathMatchesPreConsolidationBehavior
// is issue #5526's differential proof for the query package's
// githubActionsLocalReusableWorkflowPath (repository_workflow_artifacts.go).
// Every want value was captured by running the pre-#5526 standalone
// implementation over this exact input table.
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
		{
			name:  "single-quoted scalar",
			value: `'./.github/workflows/release.yaml'`,
			want:  ".github/workflows/release.yaml",
		},
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
