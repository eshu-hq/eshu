// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

var errReopenFailed = errors.New("reopen store failure")

// postReopen serves a reopen request, optionally under a scoped auth context,
// and returns the recorder.
func postReopen(t *testing.T, h *Handler, body map[string]any, authCtx *auth.AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal reopen body: %v", err)
	}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/reopen", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if authCtx != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func reopenBody() map[string]any {
	return map[string]any{
		"domain":          "workload_materialization",
		"scope_id":        "repo:acme/api",
		"reason":          "re-drive the #7285 repair targets",
		"idempotency_key": "reopen-1",
	}
}

func TestReopenRefusesMissingReason(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	h := &Handler{Store: &stubAdminStore{}, Audit: audit}
	body := reopenBody()
	delete(body, "reason")
	rec := postReopen(t, h, body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "reopen_refused_missing_reason" {
		t.Fatalf("want reopen_refused_missing_reason denied audit, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)
}

func TestReopenRefusesMissingIdempotencyKey(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	h := &Handler{Store: &stubAdminStore{}, Audit: audit}
	body := reopenBody()
	delete(body, "idempotency_key")
	rec := postReopen(t, h, body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "reopen_refused_missing_idempotency_key" {
		t.Fatalf("want reopen_refused_missing_idempotency_key denied audit, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)
}

func TestReopenRefusesMissingScope(t *testing.T) {
	h := &Handler{Store: &stubAdminStore{}}
	body := reopenBody()
	delete(body, "scope_id")
	rec := postReopen(t, h, body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestReopenRefusesUnknownDomain(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	h := &Handler{Store: &stubAdminStore{}, Audit: audit}
	body := reopenBody()
	body["domain"] = "package_consumption"
	rec := postReopen(t, h, body, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "reopen_refused_unknown_domain" {
		t.Fatalf("want reopen_refused_unknown_domain denied audit, got %+v", audit.Events)
	}
	assertReopenRefusedBody(t, rec, "unknown reopen domain: want one of repo_dependency, workload_materialization, submodule_pin", "other domains fail closed; reopening them needs a repair-sized review first")
	assertAuditValid(t, audit.Events)
}

func TestReopenRefusesUnauthorizedScopedToken(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	h := &Handler{Store: &stubAdminStore{}, Audit: audit}
	authCtx := &auth.AuthContext{Mode: auth.AuthModeScoped, SubjectIDHash: "sha256:deadbeef", AllScopes: false}
	rec := postReopen(t, h, reopenBody(), authCtx)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "reopen_refused_unauthorized" {
		t.Fatalf("want reopen_refused_scope_denied denied audit, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)
}

func TestReopenHappyPathReopensAndAudits(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	stub := &stubAdminStore{
		claim: ReplayIdempotencyClaim{Claimed: true},
		reopened: ReopenResult{
			GenerationID:       "gen-7",
			ReducerWorkItemIDs: []string{"w1", "w2"},
		},
	}
	h := &Handler{Store: stub, Audit: audit}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	if got["status"] != "reopened" {
		t.Fatalf("status = %v, want reopened", got["status"])
	}
	if got["reopened_reducer_count"] != float64(2) {
		t.Fatalf("reopened_reducer_count = %v, want 2", got["reopened_reducer_count"])
	}
	if got["generation_id"] != "gen-7" {
		t.Fatalf("generation_id = %v, want gen-7", got["generation_id"])
	}
	if got["duplicate"] != false {
		t.Fatalf("duplicate = %v, want false", got["duplicate"])
	}
	if !stub.completed {
		t.Fatal("idempotency claim was not completed")
	}
	if len(audit.Events) != 1 || audit.Events[0].Decision != governanceaudit.DecisionAllowed {
		t.Fatalf("want one allowed audit event, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)
}

func TestReopenIntentDomainReportsUnitsAndAcceptedRuns(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	stub := &stubAdminStore{
		claim: ReplayIdempotencyClaim{Claimed: true},
		reopened: ReopenResult{
			GenerationID: "gen-9",
			IntentIDs:    []string{"i1", "i2"},
			Units: []ReopenedUnit{
				{AcceptanceUnitID: "repo:acme/api", SourceRunID: "run-1", IntentID: "i1"},
				{AcceptanceUnitID: "repo:acme/web", SourceRunID: "run-2", IntentID: "i2"},
			},
		},
	}
	h := &Handler{Store: stub, Audit: audit}
	body := reopenBody()
	body["domain"] = "repo_dependency"
	rec := postReopen(t, h, body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	if got["reopened_intent_count"] != float64(2) {
		t.Fatalf("reopened_intent_count = %v, want 2", got["reopened_intent_count"])
	}
	units, ok := got["reopened_units"].([]any)
	if !ok || len(units) != 2 {
		t.Fatalf("reopened_units = %v, want 2 units", got["reopened_units"])
	}
	first := units[0].(map[string]any)
	if first["acceptance_unit_id"] != "repo:acme/api" || first["source_run_id"] != "run-1" || first["intent_id"] != "i1" {
		t.Fatalf("first unit = %v, want the accepted run and reopened intent", first)
	}
	if stub.reopenFilter.Domain != "repo_dependency" || stub.reopenFilter.ScopeID != "repo:acme/api" {
		t.Fatalf("reopen filter = %+v, want the requested domain and scope", stub.reopenFilter)
	}
}

func TestReopenDuplicateReturnsPriorOutcomeWithoutReopening(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	// Must match the handler's fingerprint, which uses the default limit (1000).
	fingerprint := reopenRequestFingerprint("workload_materialization", "repo:acme/api", 1000)
	stub := &stubAdminStore{
		claim: ReplayIdempotencyClaim{
			Claimed:       false,
			Status:        ReplayRequestStatusCompleted,
			Fingerprint:   fingerprint,
			ReplayedCount: 2,
			WorkItemIDs:   []string{"w1", "w2"},
		},
	}
	h := &Handler{Store: stub, Audit: audit}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeBody(t, rec)
	if got["duplicate"] != true || got["reopened_total_count"] != float64(2) {
		t.Fatalf("want duplicate prior outcome, got %+v", got)
	}
	if stub.reopenCalls != 0 {
		t.Fatalf("reopen calls = %d, want 0 (duplicate must not reopen)", stub.reopenCalls)
	}
	if stub.completed {
		t.Fatal("duplicate must not complete the claim again")
	}
}

func TestReopenInProgressDuplicateConflicts(t *testing.T) {
	stub := &stubAdminStore{
		claim: ReplayIdempotencyClaim{Claimed: false, Status: ReplayRequestStatusInProgress},
	}
	h := &Handler{Store: stub, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReopenRowVanishedFailsClosed(t *testing.T) {
	// Claim not won and no recorded status: the ledger row vanished. The
	// handler must fail closed (409), not report a false duplicate success.
	stub := &stubAdminStore{
		claim: ReplayIdempotencyClaim{Claimed: false, Status: ""},
	}
	h := &Handler{Store: stub, Audit: &testutil.FakeGovernanceAuditAppender{}}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReopenReusedKeyDifferentParamsConflicts(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	stub := &stubAdminStore{
		claim: ReplayIdempotencyClaim{
			Claimed:     false,
			Status:      ReplayRequestStatusCompleted,
			Fingerprint: "fingerprint-from-a-different-request",
		},
	}
	h := &Handler{Store: stub, Audit: audit}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "reopen_idempotency_key_reused" {
		t.Fatalf("want reopen_idempotency_key_reused denied audit, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)
}

func TestReopenUnknownScopeIsNotFound(t *testing.T) {
	stub := &stubAdminStore{resolveErr: ErrReopenScopeNotFound}
	h := &Handler{Store: stub}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if stub.claimCalls != 0 || stub.reopenCalls != 0 {
		t.Fatalf("pre-claim refusal must not claim or reopen (claim=%d reopen=%d)", stub.claimCalls, stub.reopenCalls)
	}
}

func TestReopenWithoutActiveGenerationIsUnprocessable(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	stub := &stubAdminStore{resolveErr: ErrReopenNoActiveGeneration}
	h := &Handler{Store: stub, Audit: audit}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	assertReopenRefusedBody(t, rec, "scope has no active generation to reopen work for", "nothing was reopened and the idempotency key was not consumed")
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "reopen_refused_no_active_generation" {
		t.Fatalf("want reopen_refused_no_active_generation denied audit, got %+v", audit.Events)
	}
	assertAuditValid(t, audit.Events)
	if stub.claimCalls != 0 || stub.reopenCalls != 0 {
		t.Fatalf("pre-claim refusal must not claim or reopen (claim=%d reopen=%d)", stub.claimCalls, stub.reopenCalls)
	}
}

func TestReopenResolveRaceAfterClaimFailsClosed(t *testing.T) {
	// The pre-claim probe passed but the scope vanished before the store
	// ran: the post-claim mapping still reports 404 and leaves the claim
	// in progress instead of losing the outcome.
	stub := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}, reopenErr: ErrReopenScopeNotFound}
	h := &Handler{Store: stub}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if stub.completed {
		t.Fatal("failed reopen must leave the claim in progress")
	}
}

func TestReopenNoActiveGenerationRaceAfterClaimIsRefused(t *testing.T) {
	// The pre-claim probe passed but the scope lost its active generation
	// before the store ran: the post-claim arm returns the same refused
	// 422 body as the probe (per the OpenAPI schema) and leaves the claim
	// in progress instead of losing the outcome.
	stub := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}, reopenErr: ErrReopenNoActiveGeneration}
	h := &Handler{Store: stub}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	assertReopenRefusedBody(t, rec, "scope has no active generation to reopen work for", "the scope lost its active generation after the claim; the idempotency key stays in progress")
	if stub.completed {
		t.Fatal("failed reopen must leave the claim in progress")
	}
}

// assertReopenRefusedBody pins the 422 refused shape the OpenAPI schema
// requires (status/reason/detail) so a WriteError-shaped regression fails
// loudly instead of silently breaking the documented contract.
func assertReopenRefusedBody(t *testing.T, rec *httptest.ResponseRecorder, wantReason, wantDetail string) {
	t.Helper()
	body := decodeBody(t, rec)
	if body["status"] != "refused" {
		t.Fatalf(`body["status"] = %v, want "refused"`, body["status"])
	}
	if body["reason"] != wantReason {
		t.Fatalf(`body["reason"] = %v, want %q`, body["reason"], wantReason)
	}
	if body["detail"] != wantDetail {
		t.Fatalf(`body["detail"] = %v, want %q`, body["detail"], wantDetail)
	}
}

func TestReopenErrorFailsClosed(t *testing.T) {
	stub := &stubAdminStore{claim: ReplayIdempotencyClaim{Claimed: true}, reopenErr: errReopenFailed}
	h := &Handler{Store: stub}
	rec := postReopen(t, h, reopenBody(), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if stub.completed {
		t.Fatal("failed reopen must leave the claim in progress")
	}
}

func TestReopenDomainsPinCanonicalNames(t *testing.T) {
	// The reopen allowlist mirrors the canonical reducercontract domain
	// names without importing the reducer tree; pin them so a rename on
	// either side fails loudly instead of silently narrowing the endpoint.
	if ReopenDomainRepoDependency != reducercontract.DomainRepoDependency {
		t.Fatalf("ReopenDomainRepoDependency = %q, want %q", ReopenDomainRepoDependency, reducercontract.DomainRepoDependency)
	}
	if ReopenDomainWorkloadMaterialization != string(reducercontract.DomainWorkloadMaterialization) {
		t.Fatalf("ReopenDomainWorkloadMaterialization = %q, want %q", ReopenDomainWorkloadMaterialization, reducercontract.DomainWorkloadMaterialization)
	}
	if ReopenDomainSubmodulePin != string(reducercontract.DomainSubmodulePin) {
		t.Fatalf("ReopenDomainSubmodulePin = %q, want %q", ReopenDomainSubmodulePin, reducercontract.DomainSubmodulePin)
	}
}

func TestReopenFingerprintSeparatesOperations(t *testing.T) {
	a := reopenRequestFingerprint("workload_materialization", "repo:acme/api", 100)
	b := reopenRequestFingerprint("submodule_pin", "repo:acme/api", 100)
	c := reopenRequestFingerprint("workload_materialization", "repo:acme/api", 100)
	if a == b {
		t.Fatal("different domains share a fingerprint")
	}
	if a != c {
		t.Fatal("same inputs produce different fingerprints")
	}
	if a == replayRequestFingerprint(nil, "", "", "", 0, false) {
		t.Fatal("reopen fingerprint collides with a replay fingerprint")
	}
}
