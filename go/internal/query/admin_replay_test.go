package query

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

var errReplayFailed = errors.New("replay store failure")

// postReplay serves a replay request, optionally under a scoped auth context,
// and returns the recorder.
func postReplay(t *testing.T, h *AdminHandler, body map[string]any, auth *AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal replay body: %v", err)
	}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/replay", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// assertAuditValid confirms every captured event is a well-formed governance
// audit event (so no malformed or secret-bearing event could be persisted).
func assertAuditValid(t *testing.T, events []governanceaudit.Event) {
	t.Helper()
	for i, ev := range events {
		if _, err := governanceaudit.NormalizeEvent(ev); err != nil {
			t.Fatalf("audit event %d invalid: %v (%+v)", i, err, ev)
		}
		if ev.Type != governanceaudit.EventTypeAdminRecoveryAction {
			t.Fatalf("audit event %d type = %q, want admin_recovery_action", i, ev.Type)
		}
	}
}

func TestReplayRefusesMissingReason(t *testing.T) {
	audit := &fakeGovernanceAuditAppender{}
	h := &AdminHandler{Store: &stubAdminStore{}, Audit: audit}
	rec := postReplay(t, h, map[string]any{
		"failure_class":   "transient_error",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied {
		t.Fatalf("want one denied audit event, got %+v", audit.events)
	}
	assertAuditValid(t, audit.events)
}

func TestReplayRefusesMissingIdempotencyKey(t *testing.T) {
	audit := &fakeGovernanceAuditAppender{}
	h := &AdminHandler{Store: &stubAdminStore{}, Audit: audit}
	rec := postReplay(t, h, map[string]any{
		"failure_class": "transient_error",
		"reason":        "cleared the backend flake",
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != "replay_refused_missing_idempotency_key" {
		t.Fatalf("want missing-idempotency-key denied audit, got %+v", audit.events)
	}
}

func TestReplayRefusesUnauthorizedScopedToken(t *testing.T) {
	audit := &fakeGovernanceAuditAppender{}
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}
	h := &AdminHandler{Store: store, Audit: audit}
	scoped := AuthContext{Mode: AuthModeScoped, SubjectIDHash: "sha256:deadbeef", AllScopes: false}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "operator requested",
		"idempotency_key": "k1",
	}, &scoped)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if store.claimCalls != 0 {
		t.Fatalf("unauthorized replay must not claim an idempotency key")
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != "replay_refused_unauthorized" {
		t.Fatalf("want unauthorized denied audit, got %+v", audit.events)
	}
	assertAuditValid(t, audit.events)
}

func TestReplayRefusesUnsafeClassWithoutForce(t *testing.T) {
	audit := &fakeGovernanceAuditAppender{}
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}
	h := &AdminHandler{Store: store, Audit: audit}
	rec := postReplay(t, h, map[string]any{
		"failure_class":   "input_invalid",
		"reason":          "thought it was transient",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	if got["status"] != "refused" || got["reason"] == "" {
		t.Fatalf("want actionable refusal, got %+v", got)
	}
	if store.claimCalls != 0 {
		t.Fatalf("unsafe-class refusal must happen before claiming")
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != "replay_refused_unsafe_class" {
		t.Fatalf("want unsafe-class denied audit, got %+v", audit.events)
	}
}

func TestReplayHappyPathExcludesUnsafeClassesAndAudits(t *testing.T) {
	audit := &fakeGovernanceAuditAppender{}
	store := &stubAdminStore{
		replayed: []AdminWorkItem{{WorkItemID: "wi-1"}, {WorkItemID: "wi-2"}},
		claim:    ReplayIdempotencyClaim{Claimed: true},
	}
	h := &AdminHandler{Store: store, Audit: audit}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "backend recovered",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Broad selectors must skip unsafe classes by default.
	if len(store.replayFilter.ExcludeFailureClasses) != 2 {
		t.Fatalf("expected unsafe classes excluded, got %v", store.replayFilter.ExcludeFailureClasses)
	}
	if !store.completed {
		t.Fatalf("expected idempotency completion recorded")
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionAllowed {
		t.Fatalf("want one allowed audit, got %+v", audit.events)
	}
	assertAuditValid(t, audit.events)
}

func TestReplayForceKeepsUnsafeClasses(t *testing.T) {
	store := &stubAdminStore{
		replayed: []AdminWorkItem{{WorkItemID: "wi-1"}},
		claim:    ReplayIdempotencyClaim{Claimed: true},
	}
	h := &AdminHandler{Store: store, Audit: &fakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"failure_class":   "input_invalid",
		"reason":          "input fixed at source, forcing",
		"idempotency_key": "k1",
		"force":           true,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(store.replayFilter.ExcludeFailureClasses) != 0 {
		t.Fatalf("force must not exclude any class, got %v", store.replayFilter.ExcludeFailureClasses)
	}
}

func TestReplayDuplicateReturnsPriorOutcomeWithoutReplaying(t *testing.T) {
	audit := &fakeGovernanceAuditAppender{}
	// Must match the handler's fingerprint, which uses the default limit (100).
	fingerprint := replayRequestFingerprint(nil, "scope-1", "", "", 100, false)
	store := &stubAdminStore{
		claim: ReplayIdempotencyClaim{
			Claimed:       false,
			Status:        replayRequestStatusCompleted,
			Fingerprint:   fingerprint,
			ReplayedCount: 3,
			WorkItemIDs:   []string{"a", "b", "c"},
		},
	}
	h := &AdminHandler{Store: store, Audit: audit}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "retry after crash",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	if got["duplicate"] != true || int(got["replayed_count"].(float64)) != 3 {
		t.Fatalf("want duplicate prior outcome, got %+v", got)
	}
	if store.completed {
		t.Fatalf("duplicate replay must not re-complete or re-run the replay")
	}
}

func TestReplayErrorLeavesClaimInProgress(t *testing.T) {
	// On a replay error the ledger row must stay in_progress (not be erased), so
	// a retry with the same key cannot lose the prior outcome or double-replay.
	store := &stubAdminStore{
		claim:     ReplayIdempotencyClaim{Claimed: true},
		replayErr: errReplayFailed,
	}
	h := &AdminHandler{Store: store, Audit: &fakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "retry",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if store.completed {
		t.Fatalf("a failed replay must not record a completed outcome")
	}
}

func TestReplayRowVanishedFailsClosed(t *testing.T) {
	// Claim not won and no recorded status: the ledger row vanished. The handler
	// must fail closed (409), not report a false duplicate success.
	store := &stubAdminStore{
		claim: ReplayIdempotencyClaim{Claimed: false, Status: ""},
	}
	h := &AdminHandler{Store: store, Audit: &fakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "retry",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReplayInProgressDuplicateConflicts(t *testing.T) {
	store := &stubAdminStore{
		claim: ReplayIdempotencyClaim{Claimed: false, Status: replayRequestStatusInProgress},
	}
	h := &AdminHandler{Store: store, Audit: &fakeGovernanceAuditAppender{}}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "retry",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReplayReusedKeyDifferentParamsConflicts(t *testing.T) {
	audit := &fakeGovernanceAuditAppender{}
	store := &stubAdminStore{
		claim: ReplayIdempotencyClaim{
			Claimed:     false,
			Status:      replayRequestStatusCompleted,
			Fingerprint: "fingerprint-from-a-different-request",
		},
	}
	h := &AdminHandler{Store: store, Audit: audit}
	rec := postReplay(t, h, map[string]any{
		"scope_id":        "scope-1",
		"reason":          "retry",
		"idempotency_key": "k1",
	}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != "replay_idempotency_key_reused" {
		t.Fatalf("want key-reuse denied audit, got %+v", audit.events)
	}
}
