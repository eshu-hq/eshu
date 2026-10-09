// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// deltaActiveResult builds a refinalize result that re-projected two
// delta-active scopes, one of which a reindex watermark can force.
func deltaActiveResult() recovery.RefinalizeResult {
	var delta recovery.DeltaActiveScopes
	delta.Add(recovery.DeltaActiveOutcomeReindexRequested, "git-repository-scope:repo-a")
	delta.Add(recovery.DeltaActiveOutcomeReindexUnsupported, "git-repository-scope:repo-a@feature")
	return recovery.RefinalizeResult{
		Enqueued:    2,
		ScopeIDs:    []string{"git-repository-scope:repo-a", "git-repository-scope:repo-a@feature"},
		DeltaActive: delta,
	}
}

// decodeDeltaActiveScopes pulls the delta_active_scopes object out of a
// response body.
func decodeDeltaActiveScopes(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	delta, ok := body["delta_active_scopes"].(map[string]any)
	if !ok {
		t.Fatalf("response has no delta_active_scopes object: %v", body)
	}
	return delta
}

// assertDeltaActiveReport checks the two-scope report deltaActiveResult builds.
func assertDeltaActiveReport(t *testing.T, delta map[string]any) {
	t.Helper()
	if got := int(delta["total"].(float64)); got != 2 {
		t.Fatalf("delta_active_scopes.total = %d, want 2", got)
	}
	byOutcome := delta["by_outcome"].(map[string]any)
	for _, outcome := range []string{recovery.DeltaActiveOutcomeReindexRequested, recovery.DeltaActiveOutcomeReindexUnsupported} {
		if got := int(byOutcome[outcome].(float64)); got != 1 {
			t.Fatalf("delta_active_scopes.by_outcome[%q] = %d, want 1", outcome, got)
		}
	}
	samples := delta["sample_scope_ids"].(map[string]any)
	requested := samples[recovery.DeltaActiveOutcomeReindexRequested].([]any)
	if len(requested) != 1 || requested[0] != "git-repository-scope:repo-a" {
		t.Fatalf("sample_scope_ids[reindex_requested] = %v, want the requested scope", requested)
	}
	if delta["detail"] != recovery.DeltaActiveDetail {
		t.Fatalf("delta_active_scopes.detail = %v, want the incomplete-graph message", delta["detail"])
	}
}

// assertReindexRequestsWritten checks reindex_requests_written in a response
// body: the exact watermark count and the named scope ids.
func assertReindexRequestsWritten(t *testing.T, body map[string]any, wantCount int, wantIDs ...string) {
	t.Helper()
	written, ok := body["reindex_requests_written"].(map[string]any)
	if !ok {
		t.Fatalf("response has no reindex_requests_written object: %v", body)
	}
	if got := int(written["count"].(float64)); got != wantCount {
		t.Fatalf("reindex_requests_written.count = %d, want %d", got, wantCount)
	}
	ids, ok := written["scope_ids"].([]any)
	if !ok {
		t.Fatalf("reindex_requests_written.scope_ids = %v, want a list (never null)", written["scope_ids"])
	}
	if len(ids) != len(wantIDs) {
		t.Fatalf("reindex_requests_written.scope_ids = %v, want %v", ids, wantIDs)
	}
	for i, id := range wantIDs {
		if ids[i] != id {
			t.Fatalf("reindex_requests_written.scope_ids = %v, want %v", ids, wantIDs)
		}
	}
}

// TestAdminHandler_RecoverGenerations_ReportsDeltaActiveScopes is the #7797
// visibility contract: a rebuild that re-projected delta generations must say
// that those scopes are incomplete until a full generation activates.
func TestAdminHandler_RecoverGenerations_ReportsDeltaActiveScopes(t *testing.T) {
	h := &Handler{
		Recovery: &stubRecoveryHandler{refinalizeResult: deltaActiveResult()},
		Store:    &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}},
	}
	w := postJSON(newAdminMux(h), "/api/v0/admin/recover-generations", map[string]any{
		"all_scopes":      true,
		"reason":          "graph rebuild after backend swap",
		"idempotency_key": "delta-report-1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	body := decodeBody(t, w)
	assertDeltaActiveReport(t, decodeDeltaActiveScopes(t, body))
	assertReindexRequestsWritten(t, body, 1, "git-repository-scope:repo-a")
}

// TestAdminHandler_RecoverGenerations_ReportsEmptyDeltaActiveScopes pins that
// the report is always present, with empty objects and no detail when no
// re-projected generation was a delta.
func TestAdminHandler_RecoverGenerations_ReportsEmptyDeltaActiveScopes(t *testing.T) {
	h := &Handler{
		Recovery: &stubRecoveryHandler{refinalizeResult: recovery.RefinalizeResult{Enqueued: 1, ScopeIDs: []string{"scope-1"}}},
		Store:    &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}},
	}
	w := postJSON(newAdminMux(h), "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-1"},
		"reason":          "wedged",
		"idempotency_key": "delta-report-empty",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	body := decodeBody(t, w)
	assertReindexRequestsWritten(t, body, 0)
	delta := decodeDeltaActiveScopes(t, body)
	if got := int(delta["total"].(float64)); got != 0 {
		t.Fatalf("delta_active_scopes.total = %d, want 0", got)
	}
	if byOutcome, ok := delta["by_outcome"].(map[string]any); !ok || len(byOutcome) != 0 {
		t.Fatalf("delta_active_scopes.by_outcome = %v, want an empty object", delta["by_outcome"])
	}
	if delta["detail"] != "" {
		t.Fatalf("delta_active_scopes.detail = %v, want empty when nothing was delta-active", delta["detail"])
	}
}

// TestAdminHandler_Refinalize_ReportsDeltaActiveScopes gives the legacy
// refinalize route the same report, since it runs the same store call.
func TestAdminHandler_Refinalize_ReportsDeltaActiveScopes(t *testing.T) {
	h := &Handler{Recovery: &stubRecoveryHandler{refinalizeResult: deltaActiveResult()}}
	w := postJSON(newAdminMux(h), "/api/v0/admin/refinalize", map[string]any{"scope_ids": []string{"git-repository-scope:repo-a"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	body := decodeBody(t, w)
	assertDeltaActiveReport(t, decodeDeltaActiveScopes(t, body))
	assertReindexRequestsWritten(t, body, 1, "git-repository-scope:repo-a")
}
