// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "testing"

// TestDiscoverChefEvidencePerDeclaration locks the per-`cookbook` block parser: a
// Supermarket-only cookbook that precedes a git-backed cookbook must not steal
// the later cookbook's git source (the cross-line-bleed regression), the git
// cookbook's name must be attributed correctly, and a Supermarket-only cookbook
// must never produce an edge.
func TestDiscoverChefEvidencePerDeclaration(t *testing.T) {
	t.Parallel()

	content := "source 'https://supermarket.chef.io'\n\n" +
		"cookbook 'nginx', '~> 12.0'\n\n" +
		"cookbook 'acme-base',\n  git: 'https://github.com/acme/deployable-source',\n  branch: 'main'\n"

	matcher := newCatalogMatcher([]CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	})
	evidence := discoverChefEvidence("chef-cookbooks", "Berksfile", content,
		matcher, map[evidenceKey]struct{}{})

	if len(evidence) != 1 {
		t.Fatalf("expected exactly 1 evidence fact (supermarket-only cookbook must not resolve), got %d: %+v", len(evidence), evidence)
	}
	got := evidence[0]
	if got.EvidenceKind != EvidenceKindChefCookbookDependency {
		t.Fatalf("evidence kind = %q, want %q", got.EvidenceKind, EvidenceKindChefCookbookDependency)
	}
	if got.TargetRepoID != "deployable-source" {
		t.Fatalf("target = %q, want deployable-source", got.TargetRepoID)
	}
	// The git source belongs to acme-base, not the preceding supermarket-only nginx.
	if name, _ := got.Details["cookbook_name"].(string); name != "acme-base" {
		t.Fatalf("cookbook_name = %q, want acme-base (cross-line bleed from nginx)", name)
	}
}

// TestDiscoverChefEvidenceIgnoresCommentedGitSource locks that a commented or
// disabled git source never fabricates a DEPENDS_ON edge: only a live `cookbook`
// git declaration resolves.
func TestDiscoverChefEvidenceIgnoresCommentedGitSource(t *testing.T) {
	t.Parallel()

	content := "cookbook 'nginx', '~> 12.0'\n" +
		"  # example: pin to an internal fork\n" +
		"  # git: 'https://github.com/acme/deployable-source'\n\n" +
		"cookbook 'acme-tools', '2.1.0' # supermarket cookbook, no git source\n"

	matcher := newCatalogMatcher([]CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	})
	evidence := discoverChefEvidence("chef-cookbooks", "Berksfile", content,
		matcher, map[evidenceKey]struct{}{})

	if len(evidence) != 0 {
		t.Fatalf("commented/disabled git source must not produce evidence, got %d: %+v", len(evidence), evidence)
	}
}
