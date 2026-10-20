// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// stubRetirementFencer scripts FenceRepository and records every request on
// the shared call log so the order test observes fence before purge.
type stubRetirementFencer struct {
	calls  *[]string
	result pgstatus.FenceResult
	err    error
}

func (s *stubRetirementFencer) FenceRepository(_ context.Context, req pgstatus.FenceRequest) (pgstatus.FenceResult, error) {
	*s.calls = append(*s.calls, "fence")
	return s.result, s.err
}

// stubRetirementPurger scripts PurgeRetired on the same shared call log.
type stubRetirementPurger struct {
	calls   *[]string
	lastReq pgstatus.PurgeRequest
	result  pgstatus.PurgeResult
	err     error
}

func (s *stubRetirementPurger) PurgeRetired(_ context.Context, req pgstatus.PurgeRequest) (pgstatus.PurgeResult, error) {
	*s.calls = append(*s.calls, "purge")
	s.lastReq = req
	return s.result, s.err
}

// stubRetirementCatalog resolves selectors to catalog entries or fails.
type stubRetirementCatalog struct {
	entries map[string][]querycontract.RepositoryCatalogEntry
	err     error
}

func (s *stubRetirementCatalog) MatchRepositories(_ context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.entries[selector], nil
}

func gitCatalogEntry(repoID, scopeID string) querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{ID: repoID, ScopeID: scopeID}
}

func retireBody() map[string]any {
	return map[string]any{
		"repositories":    []string{"acme/api"},
		"reason":          "decommissioned upstream; purge after the fence",
		"idempotency_key": "retire-1",
	}
}

func postRetire(t *testing.T, h *Handler, body map[string]any, authCtx *auth.AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal retire body: %v", err)
	}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/repositories/retire", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if authCtx != nil {
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func retireHandler(catalog *stubRetirementCatalog, fencer *stubRetirementFencer, purger *stubRetirementPurger, audit *testutil.FakeGovernanceAuditAppender) *Handler {
	return &Handler{
		Repositories:     catalog,
		RetirementFencer: fencer,
		RetirementPurger: purger,
		Audit:            audit,
	}
}

func singleRepoCatalog() *stubRetirementCatalog {
	return &stubRetirementCatalog{entries: map[string][]querycontract.RepositoryCatalogEntry{
		"acme/api": {gitCatalogEntry("PARTITION:acme/api", "git-repository-scope:PARTITION:acme/api")},
	}}
}

func TestRetireRepositoriesRunsFenceBeforePurge(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fencer := &stubRetirementFencer{calls: &calls, result: pgstatus.FenceResult{
		RetirementIDs: []string{"rr_accepted01"},
		RepoIDs:       []string{"PARTITION:acme/api"},
		ScopeIDs:      []string{"git-repository-scope:PARTITION:acme/api"},
	}}
	purger := &stubRetirementPurger{calls: &calls, result: pgstatus.PurgeResult{
		Completed: []string{"PARTITION:acme/api"},
	}}
	rec := postRetire(t, retireHandler(singleRepoCatalog(), fencer, purger, audit), retireBody(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 2 || calls[0] != "fence" || calls[1] != "purge" {
		t.Fatalf("call order = %v, want [fence purge]", calls)
	}
	if len(purger.lastReq.RepoIDs) != 1 || purger.lastReq.RepoIDs[0] != "PARTITION:acme/api" {
		t.Fatalf("purge repos = %v, want the fenced repo", purger.lastReq.RepoIDs)
	}
	got := decodeBody(t, rec)
	if got["status"] != "retired" {
		t.Fatalf("status = %v, want retired", got["status"])
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_accepted" {
		t.Fatalf("want retire_accepted audit, got %+v", audit.Events)
	}
	if audit.Events[0].Type != governanceaudit.EventTypeAdminRetirementAction {
		t.Fatalf("audit type = %v, want admin_retirement_action", audit.Events[0].Type)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireSurfacesUnfencedPurgeRefusal(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fencer := &stubRetirementFencer{calls: &calls, result: pgstatus.FenceResult{
		RetirementIDs: []string{"rr_unfenced01"},
		RepoIDs:       []string{"PARTITION:acme/api"},
	}}
	purger := &stubRetirementPurger{
		calls: &calls,
		err:   errors.Join(errors.New("retirement purge PARTITION:acme/api"), pgstatus.ErrRetirementNoOpenTombstone),
	}
	rec := postRetire(t, retireHandler(singleRepoCatalog(), fencer, purger, audit), retireBody(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 2 || calls[1] != "purge" {
		t.Fatalf("call order = %v, want the purge attempted after the fence", calls)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_unfenced" {
		t.Fatalf("want retire_refused_unfenced denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireSurfacesWrongPhasePurgeRefusal(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fencer := &stubRetirementFencer{calls: &calls, result: pgstatus.FenceResult{
		RetirementIDs: []string{"rr_wrongphase01"},
		RepoIDs:       []string{"PARTITION:acme/api"},
	}}
	purger := &stubRetirementPurger{
		calls: &calls,
		err:   errors.Join(errors.New("retirement purge PARTITION:acme/api"), pgstatus.ErrRetirementTombstoneNotFenced),
	}
	rec := postRetire(t, retireHandler(singleRepoCatalog(), fencer, purger, audit), retireBody(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_wrong_phase" {
		t.Fatalf("want retire_refused_wrong_phase denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireReportsLiveGenerationsBlocked(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fencer := &stubRetirementFencer{calls: &calls, result: pgstatus.FenceResult{
		RetirementIDs: []string{"rr_blocked01"},
		RepoIDs:       []string{"PARTITION:acme/api"},
	}}
	purger := &stubRetirementPurger{calls: &calls, result: pgstatus.PurgeResult{
		Blocked: []pgstatus.PurgeBlocked{{
			RepoID:       "PARTITION:acme/api",
			RetirementID: "rr_blocked01",
			Reason:       pgstatus.PurgeReasonTerminalNotReached,
			Detail:       "scope generations still live; a new fence must move them before the purge",
			Statuses:     "active:1",
		}},
	}}
	rec := postRetire(t, retireHandler(singleRepoCatalog(), fencer, purger, audit), retireBody(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeBody(t, rec)
	if got["status"] != "blocked" {
		t.Fatalf("status = %v, want blocked", got["status"])
	}
	blocked, ok := got["blocked"].([]any)
	if !ok || len(blocked) != 1 {
		t.Fatalf("blocked = %v, want one entry", got["blocked"])
	}
	entry, ok := blocked[0].(map[string]any)
	if !ok || entry["reason"] != pgstatus.PurgeReasonTerminalNotReached || entry["statuses"] != "active:1" {
		t.Fatalf("blocked entry = %v, want the terminal gate readback", blocked[0])
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_accepted_blocked" {
		t.Fatalf("want retire_accepted_blocked audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireRefusesFenceBlockedWithoutPurging(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fencer := &stubRetirementFencer{calls: &calls, err: &pgstatus.FenceBlockedError{
		Reason:  "claim_fence_busy",
		RepoIDs: []string{"PARTITION:acme/api"},
		Detail:  "phase-1 deadline exhausted",
	}}
	purger := &stubRetirementPurger{calls: &calls}
	rec := postRetire(t, retireHandler(singleRepoCatalog(), fencer, purger, audit), retireBody(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	for _, call := range calls {
		if call == "purge" {
			t.Fatalf("purge ran after a blocked fence; calls = %v", calls)
		}
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_fence_blocked" {
		t.Fatalf("want retire_refused_fence_blocked denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireRefusesAlreadyOpenRetirement(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fencer := &stubRetirementFencer{calls: &calls, err: pgstatus.ErrRetirementAlreadyOpen}
	purger := &stubRetirementPurger{calls: &calls}
	rec := postRetire(t, retireHandler(singleRepoCatalog(), fencer, purger, audit), retireBody(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_already_open" {
		t.Fatalf("want retire_refused_already_open denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireFenceRequestCarriesOperatorEvidence(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fenced := pgstatus.FenceResult{
		RetirementIDs: []string{"rr_evidence01"},
		RepoIDs:       []string{"PARTITION:acme/api"},
	}
	purged := pgstatus.PurgeResult{Completed: []string{"PARTITION:acme/api"}}
	var gotReq pgstatus.FenceRequest
	h := retireHandler(singleRepoCatalog(), nil, nil, audit)
	h.RetirementFencer = &captureFencer{calls: &calls, result: fenced, req: &gotReq}
	h.RetirementPurger = &capturePurger{calls: &calls, result: purged}
	rec := postRetire(t, h, retireBody(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gotReq.ReasonCode != pgstatus.RetirementReasonOperatorRetired {
		t.Fatalf("reason code = %q, want operator_retired", gotReq.ReasonCode)
	}
	if len(gotReq.Retirements) != 1 {
		t.Fatalf("retirements = %v, want one entry", gotReq.Retirements)
	}
	entry := gotReq.Retirements[0]
	if entry.RepoID != "PARTITION:acme/api" || entry.ScopeID != "git-repository-scope:PARTITION:acme/api" {
		t.Fatalf("retirement entry = %+v, want the resolved repo and scope", entry)
	}
	if !strings.HasPrefix(entry.RetirementID, "rr_") {
		t.Fatalf("retirement id = %q, want the rr_ prefix", entry.RetirementID)
	}
	if gotReq.ReasonHash == "" || gotReq.IdempotencyKeyHash == "" {
		t.Fatalf("reason and idempotency hashes must be set, got %+v", gotReq)
	}
	if gotReq.ReasonHash == "decommissioned upstream; purge after the fence" {
		t.Fatalf("reason hash carries the raw reason text")
	}
}

// assertRetireAuditValid confirms every captured event is a well-formed
// admin_retirement_action governance audit event. The shared
// assertAuditValid pins admin_recovery_action, so retirement needs its own
// check against the operator retirement event type.
func assertRetireAuditValid(t *testing.T, events []governanceaudit.Event) {
	t.Helper()
	for i, ev := range events {
		if _, err := governanceaudit.NormalizeEvent(ev); err != nil {
			t.Fatalf("audit event %d invalid: %v (%+v)", i, err, ev)
		}
		if ev.Type != governanceaudit.EventTypeAdminRetirementAction {
			t.Fatalf("audit event %d type = %q, want admin_retirement_action", i, ev.Type)
		}
	}
}

// captureFencer retains the fence request for the evidence test.
type captureFencer struct {
	calls  *[]string
	result pgstatus.FenceResult
	req    *pgstatus.FenceRequest
}

func (s *captureFencer) FenceRepository(_ context.Context, req pgstatus.FenceRequest) (pgstatus.FenceResult, error) {
	*s.calls = append(*s.calls, "fence")
	*s.req = req
	return s.result, nil
}

// capturePurger completes the capture pair.
type capturePurger struct {
	calls  *[]string
	result pgstatus.PurgeResult
}

func (s *capturePurger) PurgeRetired(_ context.Context, _ pgstatus.PurgeRequest) (pgstatus.PurgeResult, error) {
	*s.calls = append(*s.calls, "purge")
	return s.result, nil
}
