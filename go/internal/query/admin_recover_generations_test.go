package query

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// TestAdminHandler_RecoverGenerations_DurablyEnqueuesAndRecordsLedger pins the
// operator escape hatch contract: a recover-generations request must durably
// re-enqueue projector work for the named scopes (via Refinalize) AND record the
// action in the admin_replay_requests ledger through the idempotency
// claim/complete pair, so the ledger no longer stays empty for recovery work.
func TestAdminHandler_RecoverGenerations_DurablyEnqueuesAndRecordsLedger(t *testing.T) {
	recoveryStub := &stubRecoveryHandler{
		refinalizeResult: recovery.RefinalizeResult{
			Enqueued: 2,
			ScopeIDs: []string{"scope-1", "scope-2"},
		},
	}
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}
	h := &AdminHandler{Recovery: recoveryStub, Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-1", "scope-2"},
		"reason":          "generations wedged past canonical_nodes_committed",
		"idempotency_key": "recover-key-1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := decodeBody(t, w)
	if int(got["enqueued"].(float64)) != 2 {
		t.Errorf("enqueued = %v, want 2", got["enqueued"])
	}
	if got["status"] != "recovered" {
		t.Errorf("status = %v, want recovered", got["status"])
	}
	// The ledger must be claimed and completed so admin_replay_requests is written.
	if store.claimCalls != 1 {
		t.Errorf("claimCalls = %d, want 1 (recovery must durably claim the ledger)", store.claimCalls)
	}
	if !store.completed {
		t.Error("recovery must complete the idempotency ledger row")
	}
	if store.claimKey != "recover-key-1" {
		t.Errorf("claimKey = %q, want recover-key-1", store.claimKey)
	}
}

func TestAdminHandler_RecoverGenerations_RequiresScopeIDs(t *testing.T) {
	h := &AdminHandler{Recovery: &stubRecoveryHandler{}, Store: &stubAdminStore{}}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"reason":          "x",
		"idempotency_key": "k",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminHandler_RecoverGenerations_RequiresReasonAndKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{
			name: "missing reason",
			body: map[string]any{"scope_ids": []string{"s1"}, "idempotency_key": "k"},
		},
		{
			name: "missing idempotency_key",
			body: map[string]any{"scope_ids": []string{"s1"}, "reason": "why"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &AdminHandler{Recovery: &stubRecoveryHandler{}, Store: &stubAdminStore{}}
			mux := newAdminMux(h)

			w := postJSON(mux, "/api/v0/admin/recover-generations", tc.body)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestAdminHandler_RecoverGenerations_DuplicateReturnsPriorOutcome proves the
// idempotency guard: a second delivery with the same key that lost the claim
// returns the prior completed outcome without re-enqueuing.
func TestAdminHandler_RecoverGenerations_DuplicateReturnsPriorOutcome(t *testing.T) {
	recoveryStub := &stubRecoveryHandler{}
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{
		Claimed:       false,
		Status:        replayRequestStatusCompleted,
		ReplayedCount: 3,
		WorkItemIDs:   []string{"scope-a", "scope-b", "scope-c"},
	}}
	h := &AdminHandler{Recovery: recoveryStub, Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-a"},
		"reason":          "retry",
		"idempotency_key": "dup-key",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["duplicate"] != true {
		t.Errorf("duplicate = %v, want true", got["duplicate"])
	}
	if int(got["enqueued"].(float64)) != 3 {
		t.Errorf("enqueued = %v, want 3 (prior outcome)", got["enqueued"])
	}
}
