// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Election coverage for the scope repository row (#6228 publishes live
// leaf): the loader hands envelopes in (observed_at, fact_id) order, so the
// first decodable repository fact is hash luck, not fixture intent. The
// election must skip tombstoned facts and remote-colliding facts in favor
// of a uniquely-identified active repository, falling back to first
// decodable only when no such candidate exists.
package canonical

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func electionRepoEnvelope(factID, repoID, remote, localPath string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:      factID,
		ScopeID:     "package-registry:npm:eshu-fixture-team-api",
		FactKind:    "repository",
		IsTombstone: tombstone,
		Payload: map[string]any{
			"repo_id":    repoID,
			"name":       repoID,
			"remote_url": remote,
			"local_path": localPath,
		},
	}
}

// TestElectRepositorySkipsTombstonedAndAmbiguousRemotes reproduces the
// publishes fixture in loader order (fact_id ascending elects shared-b
// today): the tombstoned stale fact and the two facts sharing one remote
// must not define the scope repository — the uniquely-identified active
// team-api fact must.
func TestElectRepositorySkipsTombstonedAndAmbiguousRemotes(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		electionRepoEnvelope("1e66", "repository:eshu-fixture-shared-b", "https://github.com/acme/shared-lib", "/repo-shared-b", false),
		electionRepoEnvelope("aeb7", "repository:eshu-fixture-stale-api", "https://github.com/acme/stale-api", "/repo-stale-api", true),
		electionRepoEnvelope("ed55", "repository:eshu-fixture-shared-a", "https://github.com/acme/shared-lib", "/repo-shared-a", false),
		electionRepoEnvelope("ee65", "repository:eshu-fixture-team-api", "https://github.com/acme/team-api", "/repo-team-api", false),
	}

	row, err := extractRepository(envelopes)
	if err != nil {
		t.Fatalf("extractRepository: %v", err)
	}
	if row == nil {
		t.Fatal("extractRepository returned nil")
	}
	if row.RepoID != "repository:eshu-fixture-team-api" {
		t.Errorf("RepoID = %q, want %q (unique active remote wins over tombstoned and ambiguous facts)", row.RepoID, "repository:eshu-fixture-team-api")
	}
}

// TestElectRepositoryFallsBackToFirstDecodable covers scopes where no
// uniquely-identified active repository exists (every remote collides):
// the election keeps the historical first-decodable behavior rather than
// leaving the scope repository-less.
func TestElectRepositoryFallsBackToFirstDecodable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		electionRepoEnvelope("1e66", "repo-first", "https://example.invalid/shared.git", "/repo-first", false),
		electionRepoEnvelope("ee65", "repo-second", "https://example.invalid/shared.git", "/repo-second", false),
	}

	row, err := extractRepository(envelopes)
	if err != nil {
		t.Fatalf("extractRepository: %v", err)
	}
	if row == nil {
		t.Fatal("extractRepository returned nil")
	}
	if row.RepoID != "repo-first" {
		t.Errorf("RepoID = %q, want %q (all remotes collide, first decodable wins)", row.RepoID, "repo-first")
	}
}

// TestElectRepositoryFallsBackToTombstoned covers a scope whose only
// repository fact is a deletion marker: electing it keeps the scope's
// repository node (and its retract scoping) rather than dropping the row.
func TestElectRepositoryFallsBackToTombstoned(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		electionRepoEnvelope("aeb7", "repo-retired", "https://example.invalid/retired.git", "/repo-retired", true),
	}

	row, err := extractRepository(envelopes)
	if err != nil {
		t.Fatalf("extractRepository: %v", err)
	}
	if row == nil {
		t.Fatal("extractRepository returned nil")
	}
	if row.RepoID != "repo-retired" {
		t.Errorf("RepoID = %q, want %q (sole tombstoned fact still elects)", row.RepoID, "repo-retired")
	}
}
