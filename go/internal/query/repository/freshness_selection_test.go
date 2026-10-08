// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func serveRepositoryFreshness(t *testing.T, snapshot status.RepositoryFreshnessSnapshot) map[string]any {
	t.Helper()
	handler := repositoryFreshnessTestHandler(&testutil.FakeRepositoryFreshnessReader{Snapshot: snapshot})
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/freshness", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	return testutil.DecodeResponseBody(t, w)
}

func assertSelection(t *testing.T, resp map[string]any, want map[string]any) {
	t.Helper()
	sel := testutil.MustMapField(t, resp, "selection")
	if len(sel) != len(want) {
		t.Fatalf("selection = %#v, want exactly the keys %v", sel, want)
	}
	for key, value := range want {
		if sel[key] != value {
			t.Fatalf("selection.%s = %#v, want %#v (selection %#v)", key, sel[key], value, sel)
		}
	}
}

// TestGetRepositoryFreshnessRendersNotSelected verifies the #7625 verdict and
// the selection block for a scope every live selector confirmed as excluded.
func TestGetRepositoryFreshnessRendersNotSelected(t *testing.T) {
	t.Parallel()

	evaluatedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	snapshot := testutil.FullyBuiltRepositoryFreshnessSnapshot()
	snapshot.Selection = status.RepositoryFreshnessSelection{
		State:             selection.AggregateNotSelected,
		Reason:            selection.StateNotListed,
		StateSince:        evaluatedAt.Add(-10 * time.Minute),
		LastListedAt:      evaluatedAt.Add(-15 * time.Minute),
		EvaluatedAt:       evaluatedAt,
		LiveSelectorCount: 2,
	}

	resp := serveRepositoryFreshness(t, snapshot)
	if got, want := resp["verdict"], "not_selected"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
	assertSelection(t, resp, map[string]any{
		"state":               "not_selected",
		"reason":              "not_listed",
		"state_since":         "2026-10-08T11:50:00Z",
		"last_listed_at":      "2026-10-08T11:45:00Z",
		"evaluated_at":        "2026-10-08T12:00:00Z",
		"live_selector_count": float64(2),
	})
}

// TestGetRepositoryFreshnessSelectionUnknownWithoutLiveRows verifies the
// selection block is always rendered: with no live selector observation, or
// when the read stopped before the selection lookup, it is state unknown,
// zero live selectors, and null everywhere else, and the verdict is
// unchanged.
func TestGetRepositoryFreshnessSelectionUnknownWithoutLiveRows(t *testing.T) {
	t.Parallel()

	unknown := map[string]any{
		"state": "unknown", "reason": nil, "state_since": nil, "last_listed_at": nil,
		"evaluated_at": nil, "live_selector_count": float64(0),
	}
	for name, sel := range map[string]status.RepositoryFreshnessSelection{
		"no live rows":           {State: selection.AggregateUnknown},
		"selection never looked": {},
	} {
		snapshot := testutil.FullyBuiltRepositoryFreshnessSnapshot()
		snapshot.Selection = sel
		resp := serveRepositoryFreshness(t, snapshot)
		assertSelection(t, resp, unknown)
		if got, want := resp["verdict"], "current"; got != want {
			t.Fatalf("%s: verdict = %#v, want %#v", name, got, want)
		}
	}
}

// TestGetRepositoryFreshnessSelectedRendersNullReason verifies a selected
// scope renders a null reason and keeps verdict current.
func TestGetRepositoryFreshnessSelectedRendersNullReason(t *testing.T) {
	t.Parallel()

	evaluatedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	snapshot := testutil.FullyBuiltRepositoryFreshnessSnapshot()
	snapshot.Selection = status.RepositoryFreshnessSelection{
		State:             selection.AggregateSelected,
		StateSince:        evaluatedAt.Add(-time.Hour),
		LastListedAt:      evaluatedAt,
		EvaluatedAt:       evaluatedAt,
		LiveSelectorCount: 1,
	}

	resp := serveRepositoryFreshness(t, snapshot)
	if got, want := resp["verdict"], "current"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
	assertSelection(t, resp, map[string]any{
		"state": "selected", "reason": nil, "state_since": "2026-10-08T11:00:00Z",
		"last_listed_at": "2026-10-08T12:00:00Z", "evaluated_at": "2026-10-08T12:00:00Z",
		"live_selector_count": float64(1),
	})
}

// TestGetRepositoryFreshnessExcludedStillIngestedKeepsTheVerdict verifies
// the excluded_still_ingested state renders its reason while the verdict
// falls through to the build-based answer.
func TestGetRepositoryFreshnessExcludedStillIngestedKeepsTheVerdict(t *testing.T) {
	t.Parallel()

	evaluatedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	snapshot := testutil.FullyBuiltRepositoryFreshnessSnapshot()
	snapshot.Selection = status.RepositoryFreshnessSelection{
		State:             selection.AggregateExcludedStillIngested,
		Reason:            selection.StateRuleExcluded,
		StateSince:        evaluatedAt.Add(-time.Hour),
		LastListedAt:      evaluatedAt,
		EvaluatedAt:       evaluatedAt,
		LiveSelectorCount: 1,
	}
	resp := serveRepositoryFreshness(t, snapshot)
	if got, want := resp["verdict"], "current"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
	if sel := testutil.MustMapField(t, resp, "selection"); sel["state"] != "excluded_still_ingested" || sel["reason"] != "rule_excluded" {
		t.Fatalf("selection = %#v, want excluded_still_ingested/rule_excluded", sel)
	}
}
