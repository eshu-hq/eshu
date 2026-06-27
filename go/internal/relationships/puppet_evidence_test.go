// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "testing"

// TestDiscoverPuppetEvidencePerDeclaration locks the per-`mod` block parser: a
// forge-only module that precedes a git-backed module must not steal the later
// module's git source (the cross-line-bleed regression), the git module's name
// must be attributed correctly, and a forge-only module must never produce an
// edge.
func TestDiscoverPuppetEvidencePerDeclaration(t *testing.T) {
	t.Parallel()

	content := "forge 'https://forge.puppet.com'\n\n" +
		"mod 'puppetlabs-stdlib', '9.4.0'\n\n" +
		"mod 'acme-base',\n  :git => 'https://github.com/acme/deployable-source',\n  :ref => 'v1.0.0'\n"

	matcher := newCatalogMatcher([]CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	})
	evidence := discoverPuppetEvidence("puppet-platform-modules", "Puppetfile", content,
		matcher, map[evidenceKey]struct{}{})

	if len(evidence) != 1 {
		t.Fatalf("expected exactly 1 evidence fact (forge-only module must not resolve), got %d: %+v", len(evidence), evidence)
	}
	got := evidence[0]
	if got.EvidenceKind != EvidenceKindPuppetModuleReference {
		t.Fatalf("evidence kind = %q, want %q", got.EvidenceKind, EvidenceKindPuppetModuleReference)
	}
	if got.TargetRepoID != "deployable-source" {
		t.Fatalf("target = %q, want deployable-source", got.TargetRepoID)
	}
	// The git source belongs to acme-base, not the preceding forge-only stdlib.
	if name, _ := got.Details["module_name"].(string); name != "acme-base" {
		t.Fatalf("module_name = %q, want acme-base (cross-line bleed from puppetlabs-stdlib)", name)
	}
}

// TestDiscoverPuppetEvidenceIgnoresCommentedGitSource locks that a commented or
// disabled git source never fabricates a DEPENDS_ON edge: only a live `mod` git
// declaration resolves.
func TestDiscoverPuppetEvidenceIgnoresCommentedGitSource(t *testing.T) {
	t.Parallel()

	content := "mod 'puppetlabs-stdlib', '9.4.0'\n" +
		"  # example: pin to an internal fork\n" +
		"  # :git => 'https://github.com/acme/deployable-source'\n\n" +
		"mod 'acme-tools', '2.1.0' # forge module, no git source\n"

	matcher := newCatalogMatcher([]CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	})
	evidence := discoverPuppetEvidence("puppet-platform-modules", "Puppetfile", content,
		matcher, map[evidenceKey]struct{}{})

	if len(evidence) != 0 {
		t.Fatalf("commented/disabled git source must not produce evidence, got %d: %+v", len(evidence), evidence)
	}
}
