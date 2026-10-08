// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"errors"
	"testing"
	"time"
)

// priorAt returns a selected prior observation for every known scope, last
// evaluated at evaluatedAt.
func priorAt(known []KnownScope, evaluatedAt time.Time) []Observation {
	prior := make([]Observation, 0, len(known))
	for _, scope := range known {
		prior = append(prior, Observation{
			ScopeID: scope.ScopeID, State: StateSelected, LastListedAt: evaluatedAt,
			StateSince: evaluatedAt, StateCycleCount: 1, EvaluatedAt: evaluatedAt, LivenessWindow: testWindow,
		})
	}
	return prior
}

func TestObserverLivenessLapseWarnsOnlyPastTheWindow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		gap     time.Duration
		lapsed  bool
		wantGap float64
	}{
		{name: "20h gap stays live", gap: 20 * time.Hour, wantGap: 72000},
		{name: "48h gap is still inside the window", gap: 48 * time.Hour, wantGap: 172800},
		{name: "50h gap lapses", gap: 50 * time.Hour, lapsed: true, wantGap: 180000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			known, listing := qaFixture()
			store := &fakeStore{known: known, prior: priorAt(known, cycleOne.Add(-tc.gap))}
			h := newObserverHarness(t, store)
			if result := h.observer.Observe(context.Background(), qaRequest(listing)); result.Outcome != OutcomeEvaluated {
				t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
			}
			info := h.logLine(t, "git_repository_selection_evaluated")
			if info["evaluation_gap_seconds"] != tc.wantGap {
				t.Fatalf("evaluation_gap_seconds = %v, want %v", info["evaluation_gap_seconds"], tc.wantGap)
			}
			if got := h.hasLog(t, "git_repository_selection_liveness_lapsed"); got != tc.lapsed {
				t.Fatalf("liveness_lapsed logged = %v, want %v", got, tc.lapsed)
			}
			if !tc.lapsed {
				return
			}
			warn := h.logLine(t, "git_repository_selection_liveness_lapsed")
			if warn["level"] != "WARN" || warn["selector_id"] != testSelector.ID || warn["selector_kind"] != "github_org" ||
				warn["evaluation_gap_seconds"] != tc.wantGap || warn["liveness_window_seconds"] != float64(172800) {
				t.Fatalf("liveness_lapsed log = %v", warn)
			}
		})
	}
}

func TestObserverExplicitWritesSelectedRowsWithoutTheGauge(t *testing.T) {
	t.Parallel()

	selector := NewExplicitSelector("explicit", "boatsgroup", []Rule{{Kind: "exact", Value: "boatsgroup/repo-001"}}, appPrincipal)
	known, _ := qaFixture()
	store := &fakeStore{selector: selector, known: known, prior: priorAt(known[:1], cycleOne.Add(-50*time.Hour))}
	h := newObserverHarness(t, store)
	listing := Listing{Complete: true, Repositories: []ListedRepository{
		{ScopeID: scopeIDFor("boatsgroup/repo-001"), Slug: "boatsgroup/repo-001", State: StateSelected},
	}}
	result := h.observer.Observe(context.Background(), Request{
		Selector: selector, SourceMode: "explicit", RepoShardCount: 4, Now: cycleOne, LivenessWindow: testWindow, Listing: listing,
	})
	if result.Outcome != OutcomeEvaluated || len(store.upserts) != 1 {
		t.Fatalf("explicit outcome = %q with %d upserts, want evaluated with one", result.Outcome, len(store.upserts))
	}
	batch := store.upserts[0]
	if batch.Selector.Kind != KindExplicit || len(batch.Rows) != 1 || batch.Rows[0].State != StateSelected || batch.LivenessWindow != testWindow {
		t.Fatalf("explicit batch = %+v", batch)
	}
	info := h.logLine(t, "git_repository_selection_evaluated")
	if info["selector_kind"] != "explicit" || info["evaluation_gap_seconds"] != float64(180000) {
		t.Fatalf("explicit evaluated log = %v", info)
	}
	if !h.hasLog(t, "git_repository_selection_liveness_lapsed") {
		t.Fatal("an explicit selector past its window must log liveness_lapsed")
	}
	h.assertKindOutcomeCount(t, KindExplicit, OutcomeEvaluated, 1)
	h.assertNoGauge(t)
}

func TestObserverExplicitStoreErrorIsCountedUnderExplicit(t *testing.T) {
	t.Parallel()

	selector := NewExplicitSelector("explicit", "boatsgroup", nil, appPrincipal)
	store := &fakeStore{
		selector: selector, upsertErr: errors.New("deadline"),
		known: []KnownScope{{ScopeID: scopeIDFor("boatsgroup/a"), Slug: "boatsgroup/a"}},
	}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), Request{
		Selector: selector, SourceMode: "explicit", Now: cycleOne, LivenessWindow: testWindow,
		Listing: Listing{Complete: true, Repositories: []ListedRepository{{ScopeID: scopeIDFor("boatsgroup/a"), Slug: "boatsgroup/a", State: StateSelected}}},
	})
	if result.Outcome != OutcomeStoreError {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeStoreError)
	}
	h.assertKindOutcomeCount(t, KindExplicit, OutcomeStoreError, 1)
	h.assertNoGauge(t)
}
