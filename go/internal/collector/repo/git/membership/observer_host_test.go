// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestKnownScopeHost(t *testing.T) {
	t.Parallel()

	for kind, want := range map[string]string{
		KindGitHubOrg: "github.com",
		KindExplicit:  "",
		"unknown":     "",
	} {
		if got := KnownScopeHost(kind); got != want {
			t.Errorf("KnownScopeHost(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestObserverGitHubOrgReadsOnlyGitHubHostScopes proves a github.com org
// listing never judges another host's scopes that share the org's slug
// owner: 20 gitlab.com acme/* scopes can never appear in the listing, so
// without the host filter they would read not_listed and trip the guard.
func TestObserverGitHubOrgReadsOnlyGitHubHostScopes(t *testing.T) {
	t.Parallel()

	selector := NewGitHubOrgSelector("githubOrg", "acme", nil, false, GitHubAppPrincipal("1", "2"))
	listing := Listing{Complete: true}
	var github, gitlab []KnownScope
	for i := range 9 {
		scope := KnownScope{ScopeID: fmt.Sprintf("git-repository-scope:gh-%d", i), Slug: fmt.Sprintf("acme/repo-%d", i)}
		github = append(github, scope)
		listing.Repositories = append(listing.Repositories, ListedRepository{
			ScopeID: scope.ScopeID, Slug: scope.Slug, GitHubID: int64(i + 1), State: StateSelected,
		})
	}
	for i := range 20 {
		gitlab = append(gitlab, KnownScope{ScopeID: fmt.Sprintf("git-repository-scope:gl-%d", i), Slug: fmt.Sprintf("acme/gl-%d", i)})
	}
	store := &fakeStore{selector: selector, knownByHost: map[string][]KnownScope{
		"github.com": github,
		"":           append(slices.Clone(github), gitlab...),
	}}
	h := newObserverHarness(t, store)

	result := h.observer.Observe(context.Background(), Request{
		Selector: selector, SourceMode: "githubOrg", RepoShardCount: 1, RepoLimit: 4000,
		Now: cycleOne, LivenessWindow: testWindow, Listing: listing,
	})

	if result.Outcome != OutcomeEvaluated {
		t.Fatalf("outcome = %q, want %q (gitlab scopes must not trip the guard)", result.Outcome, OutcomeEvaluated)
	}
	if want := []string{"github.com"}; !reflect.DeepEqual(store.knownHosts, want) {
		t.Fatalf("KnownScopes hosts = %q, want %q", store.knownHosts, want)
	}
	if result.Counts.Known != 9 || result.Counts.NewlyUnlisted != 0 {
		t.Fatalf("counts = %+v, want 9 known and 0 newly unlisted", result.Counts)
	}
	if len(store.upserts) != 1 || len(store.upserts[0].Rows) != 9 {
		t.Fatalf("upserts = %+v, want one batch of the 9 github.com scopes", store.upserts)
	}
	for _, row := range store.upserts[0].Rows {
		if strings.Contains(row.ScopeID, ":gl-") {
			t.Fatalf("row for gitlab scope %s written by a github.com org selector", row.ScopeID)
		}
	}
}

func TestObserverExplicitReadsKnownScopesWithoutAHost(t *testing.T) {
	t.Parallel()

	selector := NewExplicitSelector("explicit", "acme", []Rule{{Kind: "exact", Value: "acme/repo-0"}}, "anonymous")
	store := &fakeStore{selector: selector, known: []KnownScope{{ScopeID: "git-repository-scope:gh-0", Slug: "acme/repo-0"}}}
	h := newObserverHarness(t, store)

	result := h.observer.Observe(context.Background(), Request{
		Selector: selector, SourceMode: "explicit", RepoShardCount: 1, RepoLimit: 4000,
		Now: cycleOne, LivenessWindow: testWindow,
		Listing: Listing{Complete: true, Repositories: []ListedRepository{
			{ScopeID: "git-repository-scope:gh-0", Slug: "acme/repo-0", State: StateSelected},
		}},
	})

	if result.Outcome != OutcomeEvaluated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
	}
	if want := []string{""}; !reflect.DeepEqual(store.knownHosts, want) {
		t.Fatalf("KnownScopes hosts = %q, want %q (explicit selectors match by scope id, no host filter)", store.knownHosts, want)
	}
}
