// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "testing"

// TestDiscoverSaltEvidenceMultipleRemotes locks the gitfs_remotes parser across
// both entry shapes: a plain-URL scalar entry and a single-key map entry (URL as
// the first key) with per-remote options. Both in-catalog formula repositories
// must resolve, and an unmatched remote must not fabricate an edge.
func TestDiscoverSaltEvidenceMultipleRemotes(t *testing.T) {
	t.Parallel()

	content := "fileserver_backend:\n  - gitfs\n\n" +
		"gitfs_provider: pygit2\n\n" +
		"gitfs_remotes:\n" +
		"  - https://github.com/acme/deployable-source\n" +
		"  - https://github.com/acme/network-formulas:\n" +
		"      - root: salt\n" +
		"      - base: main\n" +
		"  - https://github.com/acme/unlisted-formula\n"

	matcher := newCatalogMatcher([]CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
		{RepoID: "network-formulas", Aliases: []string{"network-formulas"}},
	})
	evidence := discoverSaltEvidence("salt-formulas", "master.yaml", content,
		matcher, map[evidenceKey]struct{}{})

	if len(evidence) != 2 {
		t.Fatalf("expected exactly 2 evidence facts (unlisted remote must not resolve), got %d: %+v", len(evidence), evidence)
	}
	targets := map[string]bool{}
	for _, e := range evidence {
		if e.EvidenceKind != EvidenceKindSaltFormulaReference {
			t.Fatalf("evidence kind = %q, want %q", e.EvidenceKind, EvidenceKindSaltFormulaReference)
		}
		if e.RelationshipType != RelDependsOn {
			t.Fatalf("evidence relationship = %q, want %q", e.RelationshipType, RelDependsOn)
		}
		targets[e.TargetRepoID] = true
	}
	if !targets["deployable-source"] {
		t.Fatalf("plain-URL gitfs remote did not resolve to deployable-source: %+v", evidence)
	}
	if !targets["network-formulas"] {
		t.Fatalf("single-key map gitfs remote did not resolve to network-formulas: %+v", evidence)
	}
}

// TestDiscoverSaltEvidenceIgnoresNonGitfsConfig locks that a Salt config without
// a gitfs_remotes list — even one that mentions an in-catalog repository in an
// unrelated field — never fabricates a DEPENDS_ON edge.
func TestDiscoverSaltEvidenceIgnoresNonGitfsConfig(t *testing.T) {
	t.Parallel()

	content := "file_roots:\n  base:\n    - /srv/salt\n\n" +
		"# gitfs is not enabled; deployable-source appears only in a comment\n" +
		"pillar_roots:\n  base:\n    - /srv/pillar\n"

	matcher := newCatalogMatcher([]CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	})
	evidence := discoverSaltEvidence("salt-formulas", "minion.yaml", content,
		matcher, map[evidenceKey]struct{}{})

	if len(evidence) != 0 {
		t.Fatalf("a config with no gitfs_remotes must not produce evidence, got %d: %+v", len(evidence), evidence)
	}
}

// TestIsSaltGitfsArtifact locks the content-based detection probe: only a
// TOP-LEVEL gitfs_remotes mapping key triggers Salt routing, so a Compose /
// GitHub Actions / other YAML file that merely mentions the string in a comment,
// a value, or a nested field is not misrouted to the Salt emitter (review #4030).
func TestIsSaltGitfsArtifact(t *testing.T) {
	t.Parallel()

	positives := []string{
		"gitfs_remotes:\n  - https://github.com/acme/x\n",
		"file_roots:\n  base:\n    - /srv/salt\ngitfs_remotes:\n  - https://github.com/acme/x\n",
	}
	for _, content := range positives {
		if !isSaltGitfsArtifact(content) {
			t.Fatalf("expected top-level gitfs_remotes to be detected as a Salt artifact: %q", content)
		}
	}

	negatives := map[string]string{
		"no gitfs_remotes":   "file_roots:\n  base:\n    - /srv/salt\n",
		"comment mention":    "services:\n  web:\n    image: nginx\n# gitfs_remotes is unrelated here\n",
		"nested key":         "env:\n  gitfs_remotes: not-a-salt-top-level-key\n",
		"value mention":      "steps:\n  - run: echo gitfs_remotes\n",
		"indented gitfs key": "  gitfs_remotes:\n    - https://github.com/acme/x\n",
	}
	for name, content := range negatives {
		if isSaltGitfsArtifact(content) {
			t.Fatalf("%s: non-top-level gitfs_remotes mention must NOT be detected as a Salt artifact: %q", name, content)
		}
	}
}
