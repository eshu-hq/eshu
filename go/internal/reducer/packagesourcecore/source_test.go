// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagesourcecore

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractRepositoriesDerivesIDFromPayloadFieldsInOrder pins the ID
// fallback chain ExtractRepositories reads: graph_id, then repo_id, then
// repository_id, then the scope ID's embedded identity. Reordering this
// chain silently changes which repositories a package-source hint can match.
func TestExtractRepositoriesDerivesIDFromPayloadFieldsInOrder(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id":      "graph-1",
				"repo_id":       "repo-1",
				"repository_id": "repository-1",
				"name":          "team-api",
				"remote_url":    "https://github.com/acme/team-api",
			},
		},
		{
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repo-2",
				"repository_id": "repository-2",
			},
		},
		{
			FactKind: "repository",
			Payload:  map[string]any{"repository_id": "repository-3"},
		},
		{
			FactKind: "repository",
			ScopeID:  "git-repository-scope:repo-4",
			Payload:  map[string]any{},
		},
	}

	got := ExtractRepositories(envelopes)
	if len(got) != 4 {
		t.Fatalf("got %d repositories, want 4: %+v", len(got), got)
	}
	wantIDs := []string{"graph-1", "repo-2", "repo-4", "repository-3"}
	for i, want := range wantIDs {
		if got[i].RepositoryID != want {
			t.Errorf("got[%d].RepositoryID = %q, want %q (result must stay sorted by RepositoryID)", i, got[i].RepositoryID, want)
		}
	}
	if got[0].RepositoryName != "team-api" || got[0].RemoteURL != "https://github.com/acme/team-api" {
		t.Errorf("got[0] = %+v, want name/remote_url read from payload", got[0])
	}
}

// TestExtractRepositoriesDropsFactsWithNoDerivableID pins that a repository
// fact with no payload ID field and a scope carrying no repository identity
// is dropped rather than surfacing an empty-ID repository, and that non
// "repository"-kind facts are ignored entirely.
func TestExtractRepositoriesDropsFactsWithNoDerivableID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", ScopeID: "", Payload: map[string]any{}},
		{FactKind: "file", ScopeID: "git-repository-scope:repo-1", Payload: map[string]any{}},
	}

	got := ExtractRepositories(envelopes)
	if len(got) != 0 {
		t.Fatalf("got %d repositories, want 0 (no derivable ID, wrong fact kind): %+v", len(got), got)
	}
}

// TestExtractRepositoriesPropagatesTombstone pins that IsTombstone on the
// envelope carries through to Repository.Tombstone, since MatchRepositories
// depends on this to separate active from stale matches.
func TestExtractRepositoriesPropagatesTombstone(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-1"}, IsTombstone: true},
	}

	got := ExtractRepositories(envelopes)
	if len(got) != 1 || !got[0].Tombstone {
		t.Fatalf("got %+v, want one tombstoned repository", got)
	}
}

// TestRepositoryIDFromScopeReturnsWholeTrimmedScopeWhenUnprefixed pins the
// behavior that makes this function deliberately looser than
// payloadcore.RepositoryIDFromReducerScope: ExtractRepositories uses it only
// as the LAST fallback after every payload ID field comes up empty, so
// narrowing an unprefixed scope to "" (matching the payloadcore helper) would
// silently drop repositories that today extract an ID this way.
func TestRepositoryIDFromScopeReturnsWholeTrimmedScopeWhenUnprefixed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		scopeID string
		want    string
	}{
		{name: "git-repository-scope prefix stripped", scopeID: "git-repository-scope:repo-1", want: "repo-1"},
		{name: "prefix stripped and trimmed", scopeID: "git-repository-scope:  repo-1  ", want: "repo-1"},
		{name: "no prefix returns whole scope, trimmed", scopeID: "  some-other-scope:repo-2  ", want: "some-other-scope:repo-2"},
		{name: "empty scope returns empty", scopeID: "", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RepositoryIDFromScope(tc.scopeID); got != tc.want {
				t.Errorf("RepositoryIDFromScope(%q) = %q, want %q", tc.scopeID, got, tc.want)
			}
		})
	}
}

// TestMatchRepositoriesPartitionsActiveAndStale pins that MatchRepositories
// returns tombstoned matches SEPARATELY from active ones rather than
// filtering them out — the reducer root's classification depends on seeing
// stale-only matches to report the distinct "stale" correlation outcome
// instead of collapsing it into "unresolved".
func TestMatchRepositoriesPartitionsActiveAndStale(t *testing.T) {
	t.Parallel()

	hint := Hint{SourceURL: "https://github.com/acme/team-api"}
	repositories := []Repository{
		{RepositoryID: "active-1", RemoteURL: "https://github.com/acme/team-api", Tombstone: false},
		{RepositoryID: "stale-1", RemoteURL: "https://github.com/acme/team-api", Tombstone: true},
		{RepositoryID: "unrelated", RemoteURL: "https://github.com/acme/other", Tombstone: false},
	}

	active, stale := MatchRepositories(hint, repositories)
	if len(active) != 1 || active[0].RepositoryID != "active-1" {
		t.Errorf("active = %+v, want only active-1", active)
	}
	if len(stale) != 1 || stale[0].RepositoryID != "stale-1" {
		t.Errorf("stale = %+v, want only stale-1", stale)
	}
}

// TestMatchRepositoriesReturnsNilForUncanonicalizableHint pins the guard: a
// hint whose source URL has no canonical key (blank) matches nothing, active
// or stale, rather than falling through to compare against every repository.
func TestMatchRepositoriesReturnsNilForUncanonicalizableHint(t *testing.T) {
	t.Parallel()

	active, stale := MatchRepositories(Hint{SourceURL: ""}, []Repository{
		{RepositoryID: "repo-1", RemoteURL: "https://github.com/acme/team-api"},
	})
	if active != nil || stale != nil {
		t.Errorf("active=%v stale=%v, want nil, nil for a hint with no canonical key", active, stale)
	}
}

// TestCanonicalURLKeyNormalizesEquivalentRemotes pins that CanonicalURLKey
// reduces SSH and HTTPS forms of the same remote to the same key — the
// property MatchRepositories relies on to match a hint against a repository
// regardless of which URL form each side observed.
func TestCanonicalURLKeyNormalizesEquivalentRemotes(t *testing.T) {
	t.Parallel()

	ssh := CanonicalURLKey("git@github.com:acme/team-api.git")
	https := CanonicalURLKey("https://github.com/acme/team-api")
	if ssh == "" || https == "" {
		t.Fatalf("CanonicalURLKey returned empty for a well-formed remote: ssh=%q https=%q", ssh, https)
	}
	if ssh != https {
		t.Errorf("CanonicalURLKey(ssh) = %q, CanonicalURLKey(https) = %q, want equal", ssh, https)
	}
	if got := CanonicalURLKey(""); got != "" {
		t.Errorf("CanonicalURLKey(\"\") = %q, want empty", got)
	}
}
