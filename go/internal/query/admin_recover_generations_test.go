// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
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

// TestOpenAPIRecoverGenerationsResponsesMatchTheHandler holds the published
// contract to what the endpoint actually sends. The two 200 bodies are not the
// same shape: a recovery this call performed reports the three dedup counters,
// and an idempotent replay cannot, because the admin_replay_requests ledger does
// not persist them.
//
// A single documented shape covering both is how an operator ends up reading
// "reducer_work_deleted" in the reference and finding it absent from the reply
// to their retry -- exactly when the original response was lost and the counters
// are what they need. This drives the handler for both cases and compares the
// keys it wrote against the schema's required lists.
func TestOpenAPIRecoverGenerationsResponsesMatchTheHandler(t *testing.T) {
	freshStore := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}}
	freshHandler := &AdminHandler{
		Recovery: &stubRecoveryHandler{refinalizeResult: recovery.RefinalizeResult{
			Enqueued:               1,
			ScopeIDs:               []string{"scope-1"},
			ReducerWorkDeleted:     4,
			SharedIntentsReopened:  5,
			ReadinessPhasesCleared: 6,
		}},
		Store: freshStore,
	}
	fresh := postJSON(newAdminMux(freshHandler), "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-1"},
		"reason":          "wedged",
		"idempotency_key": "fresh-key",
	})
	if fresh.Code != http.StatusOK {
		t.Fatalf("fresh recovery status = %d, want %d; body: %s", fresh.Code, http.StatusOK, fresh.Body.String())
	}

	duplicateHandler := &AdminHandler{
		Recovery: &stubRecoveryHandler{},
		Store: &stubAdminStore{claim: ReplayIdempotencyClaim{
			Claimed:       false,
			Status:        replayRequestStatusCompleted,
			ReplayedCount: 1,
			WorkItemIDs:   []string{"scope-1"},
		}},
	}
	duplicate := postJSON(newAdminMux(duplicateHandler), "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-1"},
		"reason":          "retry",
		"idempotency_key": "dup-key",
	})
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate replay status = %d, want %d; body: %s", duplicate.Code, http.StatusOK, duplicate.Body.String())
	}

	variants := recoverGenerationsResponseVariants(t)
	for name, tc := range map[string]struct {
		body      map[string]any
		duplicate bool
	}{
		"recovery performed by this call": {body: decodeBody(t, fresh), duplicate: false},
		"idempotent replay":               {body: decodeBody(t, duplicate), duplicate: true},
	} {
		t.Run(name, func(t *testing.T) {
			required, ok := variants[tc.duplicate]
			if !ok {
				t.Fatalf("the OpenAPI 200 response has no variant for duplicate=%v", tc.duplicate)
			}
			if got := sortedResponseKeys(tc.body); strings.Join(got, ",") != strings.Join(required, ",") {
				t.Fatalf("the %s response sends %v but the OpenAPI 200 schema requires %v; "+
					"a client generated from this spec would read a field the endpoint never sends, "+
					"or miss one it does", name, got, required)
			}
		})
	}
}

// recoverGenerationsResponseVariants returns the required-property list of each
// 200 response variant, keyed by the value of its `duplicate` discriminator.
func recoverGenerationsResponseVariants(t *testing.T) map[bool][]string {
	t.Helper()

	var spec struct {
		Paths map[string]struct {
			Post struct {
				Responses map[string]struct {
					Content map[string]struct {
						Schema struct {
							OneOf []struct {
								Required   []string `json:"required"`
								Properties map[string]struct {
									Enum []any `json:"enum"`
								} `json:"properties"`
							} `json:"oneOf"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPI spec is not valid JSON: %v", err)
	}

	schema := spec.Paths["/api/v0/admin/recover-generations"].Post.
		Responses["200"].Content["application/json"].Schema
	if len(schema.OneOf) != 2 {
		t.Fatalf("recover-generations 200 has %d response variants, want 2 (performed, replayed)", len(schema.OneOf))
	}

	variants := make(map[bool][]string, 2)
	for _, variant := range schema.OneOf {
		enum := variant.Properties["duplicate"].Enum
		if len(enum) != 1 {
			t.Fatalf("a 200 variant does not pin `duplicate` to one value, so a client cannot tell "+
				"the two shapes apart: %v", enum)
		}
		flag, ok := enum[0].(bool)
		if !ok {
			t.Fatalf("`duplicate` enum value is %T, want bool", enum[0])
		}
		required := append([]string(nil), variant.Required...)
		sort.Strings(required)
		variants[flag] = required
	}
	return variants
}

// sortedResponseKeys returns a response body's top-level keys in sorted order.
func sortedResponseKeys(body map[string]any) []string {
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestOpenAPIRecoverGenerationsRequestRejectsBothModes documents the request
// rule the handler enforces: exactly one of a non-empty scope_ids or
// all_scopes: true. A generated client that sent both, or neither, would get a
// 400 with nothing in the schema to explain why.
func TestOpenAPIRecoverGenerationsRequestRejectsBothModes(t *testing.T) {
	var spec struct {
		Paths map[string]struct {
			Post struct {
				RequestBody struct {
					Content map[string]struct {
						Schema struct {
							OneOf []map[string]any `json:"oneOf"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPI spec is not valid JSON: %v", err)
	}

	oneOf := spec.Paths["/api/v0/admin/recover-generations"].Post.
		RequestBody.Content["application/json"].Schema.OneOf
	if len(oneOf) != 2 {
		t.Fatalf("recover-generations request schema has %d oneOf branches, want 2 "+
			"(named scopes, or all_scopes); the handler rejects any other combination", len(oneOf))
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
