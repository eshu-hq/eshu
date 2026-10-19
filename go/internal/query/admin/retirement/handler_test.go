// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// errCatalogDownForTest is a catalog outage: no reader verdict wraps it,
// so the route answers the fixed 500.
var errCatalogDownForTest = errors.New("catalog: connection refused")

// scriptedCatalog resolves selectors to canned entries.
type scriptedCatalog struct {
	entries map[string][]querycontract.RepositoryCatalogEntry
	err     error
}

func (c *scriptedCatalog) MatchRepositories(_ context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.entries[selector], nil
}

// scriptedTriggerer records trigger calls and replays a canned result.
type scriptedTriggerer struct {
	calls  []TriggerRequest
	result TriggerResult
	err    error
}

func (s *scriptedTriggerer) TriggerRetirement(_ context.Context, req TriggerRequest) (TriggerResult, error) {
	s.calls = append(s.calls, req)
	return s.result, s.err
}

func gitEntry(id, scope string) querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{ID: id, ScopeID: scope}
}

func handlerFixture() (*Handler, *scriptedCatalog, *scriptedTriggerer) {
	catalog := &scriptedCatalog{entries: map[string][]querycontract.RepositoryCatalogEntry{
		"trigger-repo": {gitEntry("repository:r_trigger", "git-repository-scope:repository:r_trigger")},
	}}
	triggerer := &scriptedTriggerer{result: TriggerResult{
		Repos: []RepoOutcome{{
			RetirementID: "rr_trigger01", RepoID: "repository:r_trigger",
			ScopeID: "git-repository-scope:repository:r_trigger",
			Outcome: TriggerOutcomeCompleted,
		}},
	}}
	return &Handler{Catalog: catalog, Triggerer: triggerer}, catalog, triggerer
}

func retireHTTPBody(t *testing.T, handler *Handler, body string, authCtx auth.AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/repository-retirements", strings.NewReader(body))
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
	rec := httptest.NewRecorder()
	handler.retireRepository(rec, req)
	return rec
}

func adminAuth() auth.AuthContext {
	return auth.AuthContext{Mode: "shared", AllScopes: true}
}

// TestRetireRepositoryAcceptsTrigger proves the #8602 operator path: a
// well-formed admin request resolves the selector, triggers mark, fence,
// and purge, and answers 202 with the per-repository outcome.
func TestRetireRepositoryAcceptsTrigger(t *testing.T) {
	handler, _, triggerer := handlerFixture()
	rec := retireHTTPBody(t, handler, `{"repositories":["trigger-repo"],"reason":"deselected","idempotency_key":"key-01"}`, adminAuth())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if len(triggerer.calls) != 1 {
		t.Fatalf("trigger calls = %d, want 1", len(triggerer.calls))
	}
	if got := triggerer.calls[0].Reason; got != "deselected" {
		t.Fatalf("reason = %q, want the operator reason", got)
	}
	var decoded struct {
		Status      string `json:"status"`
		Retirements []any  `json:"retirements"`
		Idempotency string `json:"idempotency_key"`
		Duplicate   bool   `json:"duplicate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode = %v", err)
	}
	if decoded.Status != "accepted" || len(decoded.Retirements) != 1 {
		t.Fatalf("body = %s, want one accepted retirement", rec.Body.String())
	}
	if decoded.Idempotency != "key-01" {
		t.Fatalf("idempotency_key = %q, want the request key", decoded.Idempotency)
	}
}

// TestRetireRepositoryRejectsBadInput proves malformed operator input never
// reaches the trigger: every invalid body answers 400 with zero trigger
// calls.
func TestRetireRepositoryRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"no repositories":   `{"repositories":[],"reason":"r","idempotency_key":"k"}`,
		"blank reason":      `{"repositories":["trigger-repo"],"reason":"  ","idempotency_key":"k"}`,
		"blank key":         `{"repositories":["trigger-repo"],"reason":"r","idempotency_key":""}`,
		"malformed json":    `{"repositories":`,
		"unknown selector":  `{"repositories":["ghost"],"reason":"r","idempotency_key":"k"}`,
		"non-git scope":     `{"repositories":["workspace-scope"],"reason":"r","idempotency_key":"k"}`,
		"ambiguous matches": `{"repositories":["two-repos"],"reason":"r","idempotency_key":"k"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			handler, catalog, triggerer := handlerFixture()
			catalog.entries["workspace-scope"] = []querycontract.RepositoryCatalogEntry{gitEntry("workspace:root", "workspace:root")}
			catalog.entries["two-repos"] = []querycontract.RepositoryCatalogEntry{
				gitEntry("repository:r_a", "git-repository-scope:repository:r_a"), gitEntry("repository:r_b", "git-repository-scope:repository:r_b"),
			}
			rec := retireHTTPBody(t, handler, body, adminAuth())
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if len(triggerer.calls) != 0 {
				t.Fatal("trigger ran for invalid input")
			}
		})
	}
}

// TestRetireRepositoryForbidsScopedTokens proves the admin-only gate: a
// scoped token answers 403 and never reaches the trigger.
func TestRetireRepositoryForbidsScopedTokens(t *testing.T) {
	handler, _, triggerer := handlerFixture()
	rec := retireHTTPBody(t, handler,
		`{"repositories":["trigger-repo"],"reason":"r","idempotency_key":"k"}`,
		auth.AuthContext{Mode: auth.AuthModeScoped})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if len(triggerer.calls) != 0 {
		t.Fatal("trigger ran for a scoped token")
	}
}

// TestRetireRepositoryMapsFenceRefusalToConflict proves the fence refusal
// surface: a blocked fence answers 409 naming the bounded reason, never a
// 500 and never backend error text.
func TestRetireRepositoryMapsFenceRefusalToConflict(t *testing.T) {
	handler, _, triggerer := handlerFixture()
	triggerer.result = TriggerResult{Repos: []RepoOutcome{{
		RetirementID: "rr_trigger01", RepoID: "repository:r_trigger",
		ScopeID: "git-repository-scope:repository:r_trigger", Outcome: TriggerOutcomeRefused,
	}}}
	triggerer.err = &pgstorage.FenceBlockedError{
		Reason:  "projector_lease_live",
		RepoIDs: []string{"repository:r_trigger"},
		Detail:  "drain bound exhausted with a live lease",
	}
	rec := retireHTTPBody(t, handler,
		`{"repositories":["trigger-repo"],"reason":"r","idempotency_key":"k"}`, adminAuth())
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "projector_lease_live") {
		t.Fatalf("body = %s, want the bounded fence reason", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "conn refused") {
		t.Fatalf("body = %s, leaks backend error text", rec.Body.String())
	}
}

// TestRetireRepositoryAnswersLookupFailureOnce proves a catalog failure
// answers exactly once: the lookup failure writes the response and the
// handler never appends a second 400 after it.
func TestRetireRepositoryAnswersLookupFailureOnce(t *testing.T) {
	handler, catalog, triggerer := handlerFixture()
	catalog.err = errCatalogDownForTest
	rec := retireHTTPBody(t, handler,
		`{"repositories":["trigger-repo"],"reason":"r","idempotency_key":"k"}`, adminAuth())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "repository retirement selector lookup failed") {
		t.Fatalf("body = %s, want the fixed lookup message", rec.Body.String())
	}
	if len(triggerer.calls) != 0 {
		t.Fatal("trigger ran after a lookup failure")
	}
}

// TestRetireRepositoryNamesOverBudgetCounts proves an over-budget fence
// refusal tells the operator the counts: request_too_large answers 409
// with the summed generations and reducer rows.
func TestRetireRepositoryNamesOverBudgetCounts(t *testing.T) {
	handler, _, triggerer := handlerFixture()
	triggerer.result = TriggerResult{Repos: []RepoOutcome{{
		RetirementID: "rr_trigger01", RepoID: "repository:r_trigger",
		ScopeID: "git-repository-scope:repository:r_trigger", Outcome: TriggerOutcomeRefused,
	}}}
	triggerer.err = &pgstorage.FenceBlockedError{
		Reason: pgstorage.FenceReasonRequestTooLarge, Generations: 6000, ReducerRows: 10,
	}
	rec := retireHTTPBody(t, handler,
		`{"repositories":["trigger-repo"],"reason":"r","idempotency_key":"k"}`, adminAuth())
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"request_too_large", "6000", "10"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("body = %s, want %q", rec.Body.String(), want)
		}
	}
}

// TestRetireRepositoryUnconfigured proves the 503 contract: a handler
// without its catalog or trigger answers unavailable, never nil-panics.
func TestRetireRepositoryUnconfigured(t *testing.T) {
	for name, handler := range map[string]*Handler{
		"no catalog":   {Triggerer: &scriptedTriggerer{}},
		"no triggerer": {Catalog: &scriptedCatalog{}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := retireHTTPBody(t, handler,
				`{"repositories":["trigger-repo"],"reason":"r","idempotency_key":"k"}`, adminAuth())
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("code = %d, want 503", rec.Code)
			}
		})
	}
}

// TestRetireRepositoryReportsBlockedInline proves the live-generation
// refusal reaches the operator: a terminal_not_reached purge exit still
// answers 202, with the blocked reason inline for the retry.
func TestRetireRepositoryReportsBlockedInline(t *testing.T) {
	handler, _, triggerer := handlerFixture()
	triggerer.result = TriggerResult{Repos: []RepoOutcome{{
		RetirementID: "rr_trigger01", RepoID: "repository:r_trigger",
		ScopeID: "git-repository-scope:repository:r_trigger", Outcome: TriggerOutcomeBlocked,
		BlockedReason: "terminal_not_reached", BlockedStatuses: "active:1",
	}}}
	rec := retireHTTPBody(t, handler,
		`{"repositories":["trigger-repo"],"reason":"r","idempotency_key":"k"}`, adminAuth())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "terminal_not_reached") {
		t.Fatalf("body = %s, want the blocked reason inline", rec.Body.String())
	}
}
