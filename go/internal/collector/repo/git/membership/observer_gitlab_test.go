// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func gitlabGroupRequest(selector Selector, listing Listing) Request {
	return Request{Selector: selector, SourceMode: "gitlabGroup", RepoShardCount: 1, RepoLimit: 4000, Now: cycleOne, LivenessWindow: testWindow, Listing: listing, SweepExpired: true}
}

// TestObserverGitLabGroupBlankHostFailsClosed proves a gitlab_group selector
// with no host never runs a host-less known-scope read: the outcome is
// store_error with failure_class known_scopes_read and nothing is written.
func TestObserverGitLabGroupBlankHostFailsClosed(t *testing.T) {
	t.Parallel()

	selector := NewGitLabGroupSelector("gitlabGroup", "", "acme/platform", nil, false, "")
	if selector.Host != "" {
		t.Fatalf("selector host = %q, want blank for this test", selector.Host)
	}
	store := &fakeStore{selector: selector}
	h := newObserverHarness(t, store)

	result := h.observer.Observe(context.Background(), gitlabGroupRequest(selector, Listing{Complete: true}))

	if result.Outcome != OutcomeStoreError {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeStoreError)
	}
	if store.knownCalls != 0 || store.priorCalls != 0 || len(store.upserts) != 0 {
		t.Fatalf("store calls = %d known, %d prior, %d upserts; want none", store.knownCalls, store.priorCalls, len(store.upserts))
	}
	failed := h.logLine(t, "git_repository_selection_store_failed")
	if failed["failure_class"] != FailureClassKnownScopesRead {
		t.Fatalf("store_failed log = %v, want failure_class %q", failed, FailureClassKnownScopesRead)
	}
	h.assertKindOutcomeCount(t, KindGitLabGroup, OutcomeStoreError, 1)
}

// TestObserverGitLabGroupEvaluatesPrefixPartition proves the observer reads
// the group prefix partition with the selector host and writes rows only for
// the scopes under it.
func TestObserverGitLabGroupEvaluatesPrefixPartition(t *testing.T) {
	t.Parallel()

	selector := NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "Acme/Platform", nil, false, "")
	store := &fakeStore{
		selector: selector,
		known: []KnownScope{
			{ScopeID: scopeIDFor("acme/platform/a"), Slug: "acme/platform/a"},
			{ScopeID: scopeIDFor("acme/platform/sub/b"), Slug: "acme/platform/sub/b"},
			{ScopeID: scopeIDFor("acme/other/c"), Slug: "acme/other/c"},
			{ScopeID: scopeIDFor("acme/platformer/d"), Slug: "acme/platformer/d"},
		},
	}
	h := newObserverHarness(t, store)

	listing := Listing{Complete: true, Repositories: []ListedRepository{
		{ScopeID: scopeIDFor("acme/platform/a"), Slug: "gitlab/acme/platform/a", GitHubID: 101, State: StateSelected},
	}}
	result := h.observer.Observe(context.Background(), gitlabGroupRequest(selector, listing))

	if result.Outcome != OutcomeEvaluated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
	}
	if len(store.knownHosts) != 1 || store.knownHosts[0] != "gitlab.com" {
		t.Fatalf("KnownScopes hosts = %q, want [gitlab.com]", store.knownHosts)
	}
	if len(store.upserts) != 1 || len(store.upserts[0].Rows) != 2 {
		t.Fatalf("upserts = %+v, want one batch of the 2 in-prefix scopes", store.upserts)
	}
	for _, row := range store.upserts[0].Rows {
		switch row.ScopeID {
		case scopeIDFor("acme/platform/a"):
			if row.State != StateSelected {
				t.Fatalf("a row = %+v, want selected", row)
			}
		case scopeIDFor("acme/platform/sub/b"):
			if row.State != StateNotListed {
				t.Fatalf("sub/b row = %+v, want not_listed", row)
			}
		default:
			t.Fatalf("row for out-of-prefix scope %s written by a gitlab_group selector", row.ScopeID)
		}
	}
	h.assertKindOutcomeCount(t, KindGitLabGroup, OutcomeEvaluated, 1)
}

// TestObserverGitLabGroupSamplesGauge proves an evaluated gitlab_group cycle
// samples the scope gauge, which carries no selector_kind label.
func TestObserverGitLabGroupSamplesGauge(t *testing.T) {
	t.Parallel()

	selector := NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "acme/platform", nil, false, "")
	store := &fakeStore{
		selector: selector,
		known: []KnownScope{
			{ScopeID: scopeIDFor("acme/platform/a"), Slug: "acme/platform/a"},
			{ScopeID: scopeIDFor("acme/platform/sub/b"), Slug: "acme/platform/sub/b"},
		},
	}
	h := newObserverHarness(t, store)

	listing := Listing{Complete: true, Repositories: []ListedRepository{
		{ScopeID: scopeIDFor("acme/platform/a"), Slug: "gitlab/acme/platform/a", GitHubID: 101, State: StateSelected},
	}}
	h.observer.Observe(context.Background(), gitlabGroupRequest(selector, listing))

	gauge := h.gauge(t)
	if gauge[telemetry.RepositorySelectionStateSelected] != 1 || gauge[telemetry.RepositorySelectionStateNotListedPending] != 1 {
		t.Fatalf("gauge = %v, want 1 selected and 1 not_listed_pending", gauge)
	}
}
