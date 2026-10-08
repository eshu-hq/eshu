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

// TestGetRepositoryFreshnessRendersNotSelected verifies the #7625 verdict and
// the additive selection object for a scope a live selector no longer lists.
func TestGetRepositoryFreshnessRendersNotSelected(t *testing.T) {
	t.Parallel()

	evaluatedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	snapshot := testutil.FullyBuiltRepositoryFreshnessSnapshot()
	snapshot.Selection = &status.RepositoryFreshnessSelection{
		State:         selection.AggregateNotSelected,
		Reason:        selection.StateNotListed,
		LastListedAt:  evaluatedAt.Add(-15 * time.Minute),
		UnlistedSince: evaluatedAt.Add(-10 * time.Minute),
		EvaluatedAt:   evaluatedAt,
	}

	resp := serveRepositoryFreshness(t, snapshot)
	if got, want := resp["verdict"], "not_selected"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
	sel := testutil.MustMapField(t, resp, "selection")
	want := map[string]any{
		"state":          "not_selected",
		"reason":         "not_listed",
		"last_listed_at": "2026-10-08T11:45:00Z",
		"unlisted_since": "2026-10-08T11:50:00Z",
		"evaluated_at":   "2026-10-08T12:00:00Z",
	}
	if len(sel) != len(want) {
		t.Fatalf("selection = %#v, want exactly the keys %v", sel, want)
	}
	for key, value := range want {
		if sel[key] != value {
			t.Fatalf("selection.%s = %#v, want %#v", key, sel[key], value)
		}
	}
}

// TestGetRepositoryFreshnessSelectionNullWithoutLiveRows verifies the
// backwards-compatible shape: with no live selector observation the
// selection key is present and null, and the verdict is unchanged.
func TestGetRepositoryFreshnessSelectionNullWithoutLiveRows(t *testing.T) {
	t.Parallel()

	resp := serveRepositoryFreshness(t, testutil.FullyBuiltRepositoryFreshnessSnapshot())
	value, present := resp["selection"]
	if !present || value != nil {
		t.Fatalf("selection = %#v (present=%v), want present and null", value, present)
	}
	if got, want := resp["verdict"], "current"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
}

// TestGetRepositoryFreshnessSelectedRendersNullReason verifies a selected
// scope renders null reason and unlisted_since and keeps verdict current.
func TestGetRepositoryFreshnessSelectedRendersNullReason(t *testing.T) {
	t.Parallel()

	evaluatedAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	snapshot := testutil.FullyBuiltRepositoryFreshnessSnapshot()
	snapshot.Selection = &status.RepositoryFreshnessSelection{
		State:        selection.AggregateSelected,
		LastListedAt: evaluatedAt,
		EvaluatedAt:  evaluatedAt,
	}

	resp := serveRepositoryFreshness(t, snapshot)
	if got, want := resp["verdict"], "current"; got != want {
		t.Fatalf("verdict = %#v, want %#v", got, want)
	}
	sel := testutil.MustMapField(t, resp, "selection")
	if sel["state"] != "selected" || sel["reason"] != nil || sel["unlisted_since"] != nil {
		t.Fatalf("selection = %#v, want state selected with null reason and unlisted_since", sel)
	}
	if sel["last_listed_at"] != "2026-10-08T12:00:00Z" {
		t.Fatalf("selection.last_listed_at = %#v, want 2026-10-08T12:00:00Z", sel["last_listed_at"])
	}
}
