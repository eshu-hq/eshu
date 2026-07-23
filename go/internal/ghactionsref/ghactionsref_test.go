// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsref_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
)

// TestParse proves the single ref-splitting implementation absorbs the exact
// behavior of the two duplicated implementations it replaces:
// relationships.parseGitHubRefParts and the per-package @-index logic in
// query/content_relationships_github_actions.go and
// query/repository_workflow_artifacts.go. Every row here is a real GitHub
// Actions `uses:` shape.
func TestParse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		raw      string
		wantRepo string
		wantPath string
		wantRef  string
	}{
		{
			name:     "action pinned to a branch",
			raw:      "actions/checkout@v4",
			wantRepo: "actions/checkout",
			wantRef:  "v4",
		},
		{
			name:     "action pinned to a full-length 40-hex commit SHA",
			raw:      "octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			wantRepo: "octo-org/octo-action",
			wantRef:  "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		},
		{
			name:     "reusable workflow with an in-repo path",
			raw:      "octo-org/octo-repo/.github/workflows/build.yml@main",
			wantRepo: "octo-org/octo-repo",
			wantPath: ".github/workflows/build.yml",
			wantRef:  "main",
		},
		{
			name:     "local reusable workflow has no repo, only a path",
			raw:      "./.github/workflows/build.yml@main",
			wantPath: ".github/workflows/build.yml",
			wantRef:  "main",
		},
		{
			name: "no ref at all -- honest absence",
			raw:  "octo-org/octo-action",
			// A GitHub Actions "uses:" without an @ segment is unusual (GitHub
			// requires a ref for a marketplace action) but must still degrade
			// safely: no ref means an empty ref, never a fabricated one.
			wantRepo: "octo-org/octo-action",
		},
		{
			name: "blank input",
			raw:  "",
		},
		{
			name: "whitespace-only input",
			raw:  "   ",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotRepo, gotPath, gotRef := ghactionsref.Parse(tc.raw)
			if gotRepo != tc.wantRepo {
				t.Errorf("Parse(%q) repo = %q, want %q", tc.raw, gotRepo, tc.wantRepo)
			}
			if gotPath != tc.wantPath {
				t.Errorf("Parse(%q) path = %q, want %q", tc.raw, gotPath, tc.wantPath)
			}
			if gotRef != tc.wantRef {
				t.Errorf("Parse(%q) ref = %q, want %q", tc.raw, gotRef, tc.wantRef)
			}
		})
	}
}

// TestPinned proves the full-hex-only truth table: only a full-length (40 or
// 64 hex character) commit SHA is ever classified as pinned. Everything
// shorter -- including an abbreviated SHA a human might assume is "close
// enough" -- is conservatively unpinned, because a short SHA is not
// statically distinguishable from a mutable ref and GitHub's own hardening
// guidance requires the FULL commit SHA specifically (a short SHA can still
// collide or be reassigned across a mirror/fork).
func TestPinned(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ref  string
		want bool
	}{
		{name: "branch name", ref: "main", want: false},
		{name: "tag-shaped ref (v4) -- statically indistinguishable from a branch", ref: "v4", want: false},
		{name: "semver tag", ref: "v1.2.3", want: false},
		{
			name: "full 40-hex commit SHA",
			ref:  "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			want: true,
		},
		{
			name: "full 40-hex commit SHA, uppercase",
			ref:  "A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2",
			want: true,
		},
		{
			name: "full 64-hex SHA-256 commit id (future-proofing)",
			ref:  "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			want: true,
		},
		{
			name: "abbreviated 7-hex short SHA -- conservatively NOT pinned",
			ref:  "a1b2c3d",
			want: false,
		},
		{
			name: "abbreviated 12-hex short SHA -- conservatively NOT pinned",
			ref:  "a1b2c3d4e5f6",
			want: false,
		},
		{
			name: "40 characters but not all hex -- not a real SHA",
			ref:  "abcdefghij0123456789abcdefghij0123456789",
			want: false,
		},
		{name: "empty ref -- no fabrication", ref: "", want: false},
		{name: "whitespace-only ref", ref: "   ", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ghactionsref.Pinned(tc.ref); got != tc.want {
				t.Errorf("Pinned(%q) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}

// TestReusableWorkflowRepo proves the single remote-reusable-workflow slug
// detector issue #5526 consolidates. Every row is a real or realistic GitHub
// Actions job-level `uses:` value; the "./repo/..." row is the edge case that
// distinguishes this function's `./`-prefix guard from a naive
// Parse-and-check-the-path re-derivation (see the function's doc comment).
func TestReusableWorkflowRepo(t *testing.T) {
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
		{name: "./-prefixed value whose remainder is not .github -- still local, not a slug", value: "./repo/.github/workflows/build.yml@main", want: ""},
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
			if got := ghactionsref.ReusableWorkflowRepo(tc.value); got != tc.want {
				t.Errorf("ReusableWorkflowRepo(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestActionRepo proves the single third-party-action slug detector issue
// #5526 consolidates, INCLUDING the preserved ref-retention quirk for the
// plain two-segment "owner/repo@ref" shape (see the function's doc comment
// for why this is kept, not fixed, by a behavior-preserving refactor).
func TestActionRepo(t *testing.T) {
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
			if got := ghactionsref.ActionRepo(tc.value); got != tc.want {
				t.Errorf("ActionRepo(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestLocalReusableWorkflowPath proves the single local-reusable-workflow
// path detector issue #5526 consolidates.
func TestLocalReusableWorkflowPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "conventional ./ prefix", value: "./.github/workflows/release.yaml", want: ".github/workflows/release.yaml"},
		{name: "no ./ prefix -- GitHub accepts this shape too", value: ".github/workflows/verify.yaml@main", want: ".github/workflows/verify.yaml"},
		{
			name:  "remote reusable workflow shape is excluded -- ReusableWorkflowRepo's job, not this one",
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
			if got := ghactionsref.LocalReusableWorkflowPath(tc.value); got != tc.want {
				t.Errorf("LocalReusableWorkflowPath(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
