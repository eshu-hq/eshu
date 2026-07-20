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
