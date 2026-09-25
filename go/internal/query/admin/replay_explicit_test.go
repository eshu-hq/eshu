// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestReplayExplicitIDsInManualReviewClassRefusedWithoutForce is the #7120
// regression: explicit work_item_ids that resolve to manual-review rows used to
// be silently excluded by the store and answered 200 with replayed_count 0.
func TestReplayExplicitIDsInManualReviewClassRefusedWithoutForce(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	store := &stubAdminStore{
		claim: ReplayIdempotencyClaim{Claimed: true},
		unsafeTargets: []UnsafeReplayTarget{
			{WorkItemID: "wi-b", FailureClass: "projection_bug"},
			{WorkItemID: "wi-a", FailureClass: "resource_exhausted"},
		},
	}
	h := &Handler{Store: store, Audit: audit}
	rec := postReplay(t, h, map[string]any{
		"work_item_ids":   []string{"wi-a", "wi-b"},
		"reason":          "cause fixed",
		"idempotency_key": "k-explicit",
	}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	if got["status"] != "refused" || got["reason"] == "" || got["detail"] == "" {
		t.Fatalf("want actionable refusal, got %+v", got)
	}
	refused, ok := got["refused_work_items"].([]any)
	if !ok || len(refused) != 2 {
		t.Fatalf("refused_work_items = %+v, want 2 entries", got["refused_work_items"])
	}
	first := refused[0].(map[string]any)
	if first["work_item_id"] != "wi-a" || first["failure_class"] != "resource_exhausted" || first["reason"] == "" {
		t.Fatalf("refused_work_items[0] = %+v, want sorted wi-a/resource_exhausted with guidance", first)
	}
	second := refused[1].(map[string]any)
	if second["work_item_id"] != "wi-b" || second["failure_class"] != "projection_bug" {
		t.Fatalf("refused_work_items[1] = %+v, want wi-b/projection_bug", second)
	}
	if store.claimCalls != 0 || store.completed {
		t.Fatalf("refusal must not claim or complete the idempotency key (claims=%d completed=%v)", store.claimCalls, store.completed)
	}
	if store.replayFilter.WorkItemIDs != nil {
		t.Fatalf("refusal must not replay anything, got filter %+v", store.replayFilter)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "replay_refused_unsafe_work_items" {
		t.Fatalf("want unsafe-work-items denied audit, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)
}

// TestReplayExplicitIDsMixedSafeAndUnsafeRefusesWholeRequest pins the atomic
// mixed-id decision: one unsafe id refuses the whole request, listing only the
// unsafe id, and no safe id is replayed.
func TestReplayExplicitIDsMixedSafeAndUnsafeRefusesWholeRequest(t *testing.T) {
	store := &stubAdminStore{
		claim:         ReplayIdempotencyClaim{Claimed: true},
		replayed:      []WorkItem{{WorkItemID: "wi-safe"}},
		unsafeTargets: []UnsafeReplayTarget{{WorkItemID: "wi-poison", FailureClass: "projection_bug"}},
	}
	h := &Handler{Store: store, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"work_item_ids":   []string{"wi-safe", "wi-poison"},
		"reason":          "retry",
		"idempotency_key": "k-mixed",
	}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	refused := decodeBody(t, rec)["refused_work_items"].([]any)
	if len(refused) != 1 || refused[0].(map[string]any)["work_item_id"] != "wi-poison" {
		t.Fatalf("refused_work_items = %+v, want only wi-poison", refused)
	}
	if store.claimCalls != 0 || store.replayFilter.WorkItemIDs != nil {
		t.Fatalf("mixed refusal must have no side effects")
	}
}

// TestReplayExplicitIDsForceReplaysManualReviewClass proves force skips the
// unsafe-target read and keeps the existing replay behavior.
func TestReplayExplicitIDsForceReplaysManualReviewClass(t *testing.T) {
	store := &stubAdminStore{
		claim:         ReplayIdempotencyClaim{Claimed: true},
		replayed:      []WorkItem{{WorkItemID: "wi-poison"}},
		unsafeTargets: []UnsafeReplayTarget{{WorkItemID: "wi-poison", FailureClass: "projection_bug"}},
	}
	h := &Handler{Store: store, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"work_item_ids":   []string{"wi-poison"},
		"reason":          "cause fixed by #7100",
		"idempotency_key": "k-force",
		"force":           true,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := int(decodeBody(t, rec)["replayed_count"].(float64)); got != 1 {
		t.Fatalf("replayed_count = %d, want 1", got)
	}
	if store.unsafeCalls != 0 {
		t.Fatalf("force must not consult the unsafe-target read")
	}
}

// TestReplayExplicitSafeIDsStillReplayAndReadIsScoped proves ids with no
// unsafe rows (safe class, unknown, or not terminal) keep today's behavior, and
// that the pre-replay read carries the same selectors as the replay.
func TestReplayExplicitSafeIDsStillReplayAndReadIsScoped(t *testing.T) {
	store := &stubAdminStore{
		claim:    ReplayIdempotencyClaim{Claimed: true},
		replayed: []WorkItem{{WorkItemID: "wi-1"}},
	}
	h := &Handler{Store: store, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"work_item_ids":   []string{"wi-1", "wi-unknown"},
		"scope_id":        "scope-1",
		"stage":           "projector",
		"reason":          "backend recovered",
		"idempotency_key": "k-safe",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if store.unsafeCalls != 1 {
		t.Fatalf("unsafeCalls = %d, want 1", store.unsafeCalls)
	}
	f := store.unsafeFilter
	if len(f.WorkItemIDs) != 2 || f.ScopeID != "scope-1" || f.Stage != "projector" {
		t.Fatalf("unsafe read filter = %+v, want ids/scope/stage carried", f)
	}
	classes := make(map[string]bool)
	for _, class := range f.UnsafeFailureClasses {
		classes[class] = true
	}
	for _, required := range []string{"input_invalid", "unsafe_payload", "projection_bug", "resource_exhausted"} {
		if !classes[required] {
			t.Fatalf("unsafe read must cover %q, got %v", required, f.UnsafeFailureClasses)
		}
	}
	if !store.completed {
		t.Fatalf("safe replay must record the idempotency outcome")
	}
}

// TestReplayScopeOnlySelectorSkipsUnsafeTargetRead keeps the broad-selector path
// unchanged: no pre-read, the store-side exclusion still applies.
func TestReplayScopeOnlySelectorSkipsUnsafeTargetRead(t *testing.T) {
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}
	h := &Handler{Store: store, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "backend recovered",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusOK || store.unsafeCalls != 0 {
		t.Fatalf("status=%d unsafeCalls=%d, want 200 and no pre-read", rec.Code, store.unsafeCalls)
	}
}

// TestReplayUnsafeTargetReadErrorFailsClosedBeforeClaim proves a failed
// pre-read returns 500 without consuming the idempotency key.
func TestReplayUnsafeTargetReadErrorFailsClosedBeforeClaim(t *testing.T) {
	store := &stubAdminStore{
		claim:            ReplayIdempotencyClaim{Claimed: true},
		unsafeTargetsErr: errReplayFailed,
	}
	h := &Handler{Store: store, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"work_item_ids":   []string{"wi-1"},
		"reason":          "retry",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if store.claimCalls != 0 {
		t.Fatalf("read failure must not claim the idempotency key")
	}
}

// TestReplayExplicitIDsWithFailureClassSelectorSkipsRead proves a request that
// also names a failure_class never causes a false 422: an unsafe class is
// refused (or forced) before this check, so any class reaching it is safe and
// "unsafe AND that class" is empty by construction. The read is skipped rather
// than spent on a provably empty result.
func TestReplayExplicitIDsWithFailureClassSelectorSkipsRead(t *testing.T) {
	store := &stubAdminStore{
		claim:    ReplayIdempotencyClaim{Claimed: true},
		replayed: []WorkItem{{WorkItemID: "wi-transient"}},
		// The real store applies the failure_class predicate, so the
		// projection_bug id is not returned; the stub returns nothing.
	}
	h := &Handler{Store: store, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"work_item_ids":   []string{"wi-poison", "wi-transient"},
		"failure_class":   "transient_error",
		"reason":          "backend recovered",
		"idempotency_key": "k-class",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if store.unsafeCalls != 0 {
		t.Fatalf("unsafe read ran %d times with a safe failure_class selector, want 0", store.unsafeCalls)
	}
}
