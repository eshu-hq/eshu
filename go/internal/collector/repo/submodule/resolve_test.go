// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestResolveRepoID(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		url        string
		wantResolv bool
	}{
		{name: "https url resolves", url: "https://github.com/example/libfoo.git", wantResolv: true},
		{name: "ssh scp-style git@ url resolves", url: "git@github.com:example/libfoo.git", wantResolv: true},
		{name: "ssh scheme url resolves", url: "ssh://git@example.com/example/libfoo.git", wantResolv: true},
		{name: "relative parent url does not resolve", url: "../libfoo.git", wantResolv: false},
		{name: "relative sibling url with dot prefix does not resolve", url: "./libfoo", wantResolv: false},
		{name: "bare local path does not resolve", url: "/abs/local/path", wantResolv: false},
		{name: "empty url does not resolve", url: "", wantResolv: false},
		{name: "whitespace-only url does not resolve", url: "   ", wantResolv: false},
		{name: "non-git@ scp style url does not resolve", url: "someuser@example.com:owner/repo.git", wantResolv: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveRepoID(testCase.url)
			if testCase.wantResolv && got == "" {
				t.Fatalf("ResolveRepoID(%q) = %q, want a non-empty resolved repo_id", testCase.url, got)
			}
			if !testCase.wantResolv && got != "" {
				t.Fatalf("ResolveRepoID(%q) = %q, want empty (unresolved)", testCase.url, got)
			}
		})
	}
}

// TestResolveRepoIDIsCanonicalWithRepositoryIdentity proves ResolveRepoID's
// result for a resolvable URL is byte-identical to what
// repositoryidentity.CanonicalRepositoryID itself would compute from the same
// raw URL, so a submodule's resolved_repo_id always joins against the same
// repo_id Eshu's own repository-identity collector would assign that
// repository (same canonicalization, same hash).
func TestResolveRepoIDIsCanonicalWithRepositoryIdentity(t *testing.T) {
	t.Parallel()

	const url = "https://github.com/example/libfoo.git"
	want, err := repositoryidentity.CanonicalRepositoryID(url, "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID(%q) unexpected error: %v", url, err)
	}

	if got := ResolveRepoID(url); got != want {
		t.Fatalf("ResolveRepoID(%q) = %q, want %q", url, got, want)
	}
}

// TestResolveRepoIDDifferentSpellingsOfSameRepoResolveIdentically proves two
// different but equivalent spellings of the same remote (HTTPS vs SCP-style
// SSH, with/without the ".git" suffix) resolve to the same repo_id, matching
// repositoryidentity.NormalizeRemoteURL's own equivalence.
func TestResolveRepoIDDifferentSpellingsOfSameRepoResolveIdentically(t *testing.T) {
	t.Parallel()

	https := ResolveRepoID("https://github.com/example/libfoo.git")
	ssh := ResolveRepoID("git@github.com:example/libfoo.git")
	noSuffix := ResolveRepoID("https://github.com/example/libfoo")

	if https == "" || ssh == "" || noSuffix == "" {
		t.Fatalf("expected all spellings to resolve: https=%q ssh=%q noSuffix=%q", https, ssh, noSuffix)
	}
	if https != ssh {
		t.Fatalf("https spelling (%q) and ssh spelling (%q) resolved to different repo_ids", https, ssh)
	}
	if https != noSuffix {
		t.Fatalf("https spelling (%q) and no-.git-suffix spelling (%q) resolved to different repo_ids", https, noSuffix)
	}
}
