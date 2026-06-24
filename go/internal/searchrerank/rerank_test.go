// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchrerank

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// result builds one baseline retrieval result with the given id, rank, score,
// and graph handles for reranking tests.
func result(id string, rank int, score float64, handles ...searchdocs.GraphHandle) searchretrieval.Result {
	return searchretrieval.Result{
		Document: searchdocs.Document{
			ID:           id,
			TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived},
			Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
			GraphHandles: handles,
		},
		Rank:      rank,
		Score:     score,
		Handles:   handles,
		Freshness: searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
}

func handle(kind, id string) searchdocs.GraphHandle {
	return searchdocs.GraphHandle{Kind: kind, ID: id}
}

func ids(results []RankedResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Result.Document.ID)
	}
	return out
}

func equalOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRerankDisabledReturnsBaseline(t *testing.T) {
	baseline := []searchretrieval.Result{
		result("a", 1, 3.0),
		result("b", 2, 2.0, handle("service", "checkout")),
	}
	out := Rerank(baseline, Options{Enabled: false, Scope: searchretrieval.Scope{ServiceID: "checkout"}})
	if out.State != StateDisabled {
		t.Fatalf("state = %q, want %q", out.State, StateDisabled)
	}
	if !equalOrder(ids(out.Results), []string{"a", "b"}) {
		t.Fatalf("order = %v, want baseline [a b]", ids(out.Results))
	}
	for _, r := range out.Results {
		if r.Basis.GraphBoost != 0 || len(r.Basis.Contributions) != 0 {
			t.Fatalf("disabled rerank must not record graph boost: %+v", r.Basis)
		}
		if r.Basis.FinalRank != r.Basis.BaselineRank {
			t.Fatalf("disabled rerank must preserve rank: %+v", r.Basis)
		}
	}
}

func TestRerankAppliedPromotesAnchoredResult(t *testing.T) {
	// "b" is the service-anchored result but ranks below "a" at baseline.
	baseline := []searchretrieval.Result{
		result("a", 1, 3.0),
		result("b", 2, 2.0, handle("service", "checkout")),
	}
	out := Rerank(baseline, Options{
		Enabled: true,
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindService, ID: "checkout"},
		Scope:   searchretrieval.Scope{ServiceID: "checkout"},
	})
	if out.State != StateApplied {
		t.Fatalf("state = %q, want %q", out.State, StateApplied)
	}
	if !equalOrder(ids(out.Results), []string{"b", "a"}) {
		t.Fatalf("order = %v, want [b a] after service-anchor rerank", ids(out.Results))
	}
	top := out.Results[0]
	if top.Basis.GraphBoost <= 0 {
		t.Fatalf("promoted result must record positive graph boost: %+v", top.Basis)
	}
	if top.Basis.BaselineRank != 2 || top.Basis.FinalRank != 1 {
		t.Fatalf("basis ranks wrong: %+v", top.Basis)
	}
	if top.Basis.BaselineScore != 2.0 {
		t.Fatalf("baseline score must be preserved, got %v", top.Basis.BaselineScore)
	}
	if len(top.Basis.Contributions) == 0 || top.Basis.Contributions[0].Kind != SignalService {
		t.Fatalf("expected service contribution, got %+v", top.Basis.Contributions)
	}
	// Contribution handle must be kind:id only, never document content.
	if top.Basis.Contributions[0].Handle != "service:checkout" {
		t.Fatalf("handle leak/format wrong: %q", top.Basis.Contributions[0].Handle)
	}
}

func TestRerankInactiveWhenNoSignalsFire(t *testing.T) {
	baseline := []searchretrieval.Result{
		result("a", 1, 3.0),
		result("b", 2, 2.0),
	}
	out := Rerank(baseline, Options{
		Enabled: true,
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindService, ID: "checkout"},
		Scope:   searchretrieval.Scope{ServiceID: "checkout"},
	})
	if out.State != StateInactive {
		t.Fatalf("state = %q, want %q", out.State, StateInactive)
	}
	if !equalOrder(ids(out.Results), []string{"a", "b"}) {
		t.Fatalf("inactive rerank must preserve baseline, got %v", ids(out.Results))
	}
}

func TestRerankStaleFailsClosed(t *testing.T) {
	baseline := []searchretrieval.Result{
		result("a", 1, 3.0),
		result("b", 2, 2.0, handle("service", "checkout")),
	}
	out := Rerank(baseline, Options{
		Enabled:    true,
		GraphStale: true,
		Anchor:     searchretrieval.Anchor{Kind: searchretrieval.ScopeKindService, ID: "checkout"},
		Scope:      searchretrieval.Scope{ServiceID: "checkout"},
	})
	if out.State != StateStale {
		t.Fatalf("state = %q, want %q", out.State, StateStale)
	}
	if !equalOrder(ids(out.Results), []string{"a", "b"}) {
		t.Fatalf("stale rerank must fail closed to baseline, got %v", ids(out.Results))
	}
}

func TestRerankPreservesResultSetAndIsDeterministic(t *testing.T) {
	// Two results with identical graph signals must tie-break by document id,
	// and reranking must never add or drop a result (authz/scope invariant).
	baseline := []searchretrieval.Result{
		result("z", 1, 1.0, handle("incident", "i-1")),
		result("a", 2, 1.0, handle("incident", "i-2")),
	}
	out := Rerank(baseline, Options{
		Enabled: true,
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "r"},
		Scope:   searchretrieval.Scope{RepoID: "r"},
	})
	if len(out.Results) != 2 {
		t.Fatalf("rerank changed result-set size: %d", len(out.Results))
	}
	// equal graph boost => deterministic by baseline rank then id; both rank
	// equally so the lower baseline rank ("z" at 1) leads.
	if out.Results[0].Result.Document.ID != "z" {
		t.Fatalf("nondeterministic tie-break: %v", ids(out.Results))
	}
}

func TestRerankPackageAndOwnerSignals(t *testing.T) {
	baseline := []searchretrieval.Result{
		result("a", 1, 3.0),
		result("b", 2, 2.0, handle("container_image", "img-1"), handle("owner", "team-pay")),
	}
	out := Rerank(baseline, Options{
		Enabled: true,
		Anchor:  searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "r"},
		Scope:   searchretrieval.Scope{RepoID: "r"},
	})
	if out.State != StateApplied {
		t.Fatalf("state = %q, want applied", out.State)
	}
	kinds := map[SignalKind]bool{}
	for _, c := range out.Results[0].Basis.Contributions {
		kinds[c.Kind] = true
	}
	if !kinds[SignalPackage] || !kinds[SignalOwner] {
		t.Fatalf("expected package and owner signals, got %+v", out.Results[0].Basis.Contributions)
	}
}

func TestRerankEmptyInput(t *testing.T) {
	out := Rerank(nil, Options{Enabled: true, Scope: searchretrieval.Scope{RepoID: "r"}})
	if out.State != StateInactive {
		t.Fatalf("empty input state = %q, want %q", out.State, StateInactive)
	}
	if len(out.Results) != 0 {
		t.Fatalf("empty input must yield no results, got %d", len(out.Results))
	}
}
