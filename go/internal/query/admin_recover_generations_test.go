// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestAdminHandler_RecoverGenerations_AllScopesRebuildsEveryScope is the
// disaster-recovery entry point: after restoring Postgres, an operator rebuilds
// the graph from preserved facts without knowing a single scope id. The request
// must reach the store with AllScopes set, because a filter that arrives empty
// enqueues nothing and reports a clean success over an empty graph.
func TestAdminHandler_RecoverGenerations_AllScopesRebuildsEveryScope(t *testing.T) {
	recoveryStub := &stubRecoveryHandler{
		refinalizeResult: recovery.RefinalizeResult{
			Enqueued: 20,
			ScopeIDs: []string{"scope-1", "scope-2"},
		},
	}
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}
	h := &AdminHandler{Recovery: recoveryStub, Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"all_scopes":      true,
		"reason":          "graph rebuild from preserved facts after Postgres restore",
		"idempotency_key": "dr-rebuild-1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !recoveryStub.refinalizeFilter.AllScopes {
		t.Fatal("store received AllScopes = false: the all-scopes rebuild would enqueue nothing")
	}
	if len(recoveryStub.refinalizeFilter.ScopeIDs) != 0 {
		t.Fatalf("store received ScopeIDs = %v, want empty", recoveryStub.refinalizeFilter.ScopeIDs)
	}
	got := decodeBody(t, w)
	if int(got["enqueued"].(float64)) != 20 {
		t.Errorf("enqueued = %v, want 20", got["enqueued"])
	}
	if !store.completed {
		t.Error("an all-scopes rebuild must complete the idempotency ledger row")
	}
}

// TestAdminHandler_RecoverGenerations_RejectsAllScopesWithScopeIDs keeps the two
// modes apart. A body carrying both is ambiguous, and guessing either way
// rebuilds a different set than the operator asked for.
func TestAdminHandler_RecoverGenerations_RejectsAllScopesWithScopeIDs(t *testing.T) {
	recoveryStub := &stubRecoveryHandler{}
	h := &AdminHandler{Recovery: recoveryStub, Store: &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"all_scopes":      true,
		"scope_ids":       []string{"scope-1"},
		"reason":          "why",
		"idempotency_key": "k",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if recoveryStub.refinalizeFilter.AllScopes || len(recoveryStub.refinalizeFilter.ScopeIDs) > 0 {
		t.Fatal("ambiguous request reached the store; it must be refused before any enqueue")
	}
}

// TestAdminHandler_RecoverGenerations_AllScopesFingerprintIsolatesTheFlag holds
// the scope list constant so only all_scopes varies. Comparing a scoped request
// against an all-scopes one with a different scope list would pass on the scope
// difference alone and prove nothing about the flag.
func TestAdminHandler_RecoverGenerations_AllScopesFingerprintIsolatesTheFlag(t *testing.T) {
	scoped := recoverGenerationsFingerprint(nil, false)
	all := recoverGenerationsFingerprint(nil, true)

	if scoped == all {
		t.Fatal("all_scopes does not reach the fingerprint: one idempotency key would cover both modes")
	}
}

// TestAdminHandler_RecoverGenerations_AllScopesConflictsWithScopedKey is the
// consequence that matters to an operator. A key already used for a two-scope
// recovery must not answer a whole-deployment rebuild with the two-scope
// outcome: that reports "recovered, 2 scopes" and leaves the rest of the graph
// unbuilt, during the one operation where nobody is in a position to notice.
func TestAdminHandler_RecoverGenerations_AllScopesConflictsWithScopedKey(t *testing.T) {
	recoveryStub := &stubRecoveryHandler{}
	store := &stubAdminStore{claim: ReplayIdempotencyClaim{
		Claimed:       false,
		Status:        replayRequestStatusCompleted,
		Fingerprint:   recoverGenerationsFingerprint([]string{"scope-1", "scope-2"}, false),
		ReplayedCount: 2,
		WorkItemIDs:   []string{"scope-1", "scope-2"},
	}}
	h := &AdminHandler{Recovery: recoveryStub, Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/recover-generations", map[string]any{
		"all_scopes":      true,
		"reason":          "graph rebuild from preserved facts after Postgres restore",
		"idempotency_key": "reused-key",
	})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
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
