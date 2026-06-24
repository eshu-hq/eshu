// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdmissionDecisionHandlerReturnsBoundedStatesAndNextCalls(t *testing.T) {
	t.Parallel()

	store := &recordingAdmissionDecisionReadStore{
		rows: []AdmissionDecisionReadRow{
			admissionDecisionReadRow("decision-1", "admitted"),
			admissionDecisionReadRow("decision-2", "ambiguous"),
			admissionDecisionReadRow("decision-3", "rejected"),
			admissionDecisionReadRow("decision-4", "stale"),
			admissionDecisionReadRow("decision-5", "permission_hidden"),
			admissionDecisionReadRow("decision-6", "missing_evidence"),
		},
		evidence: map[string][]AdmissionDecisionEvidenceRow{
			"decision-1": {{
				EvidenceID:   "evidence-1",
				DecisionID:   "decision-1",
				SourceHandle: "fact:relationship:1",
				EvidenceKind: "relationship_fact",
				Detail:       map[string]any{"fact_id": "fact:relationship:1"},
				CreatedAt:    time.Unix(1700000000, 0).UTC(),
			}},
		},
	}
	handler := &EvidenceHandler{
		AdmissionDecisions: store,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=git-repository-scope:team/api&generation_id=generation-1&anchor_kind=repository&anchor_id=repo://team/api&limit=5&include_evidence=true",
		nil,
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.lastFilter.Limit, 6; got != want {
		t.Fatalf("filter.Limit = %d, want %d for limit+1 truncation proof", got, want)
	}
	if got, want := store.lastFilter.Domain, "deployable_unit"; got != want {
		t.Fatalf("filter.Domain = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ScopeID, "git-repository-scope:team/api"; got != want {
		t.Fatalf("filter.ScopeID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.AnchorKind, "repository"; got != want {
		t.Fatalf("filter.AnchorKind = %q, want %q", got, want)
	}

	var body struct {
		Decisions            []AdmissionDecisionResult `json:"decisions"`
		Count                int                       `json:"count"`
		Limit                int                       `json:"limit"`
		Truncated            bool                      `json:"truncated"`
		RecommendedNextCalls []RecommendedNextCall     `json:"recommended_next_calls"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if got, want := body.Count, 5; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if !body.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if len(body.Decisions) != 5 {
		t.Fatalf("len(decisions) = %d, want 5", len(body.Decisions))
	}
	states := map[string]bool{}
	for _, decision := range body.Decisions {
		states[decision.State] = true
		if len(decision.SourceHandles) == 0 {
			t.Fatalf("decision %s missing source handles", decision.DecisionID)
		}
	}
	for _, state := range []string{"admitted", "ambiguous", "rejected", "stale", "permission_hidden"} {
		if !states[state] {
			t.Fatalf("response missing state %q in %#v", state, states)
		}
	}
	if got := body.Decisions[0].Evidence[0].SourceHandle; got != "fact:relationship:1" {
		t.Fatalf("evidence source handle = %q, want fact:relationship:1", got)
	}
	if got, want := body.Decisions[0].DomainState, "exact"; got != want {
		t.Fatalf("domain_state = %q, want %q", got, want)
	}
	if len(body.RecommendedNextCalls) == 0 {
		t.Fatal("recommended_next_calls empty, want bounded next-call guidance")
	}
}

func TestAdmissionDecisionHandlerFiltersStateAndReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := &recordingAdmissionDecisionReadStore{}
	handler := &EvidenceHandler{
		AdmissionDecisions: store,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/evidence/admission-decisions?domain=package_source&scope_id=git-repository-scope:team/api&generation_id=generation-1&state=missing_evidence&limit=10",
		nil,
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.lastFilter.State == nil || *store.lastFilter.State != "missing_evidence" {
		t.Fatalf("filter.State = %#v, want missing_evidence", store.lastFilter.State)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if got, want := int(body["count"].(float64)), 0; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := body["truncated"].(bool), false; got != want {
		t.Fatalf("truncated = %v, want %v", got, want)
	}
}

func TestAdmissionDecisionScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingAdmissionDecisionReadStore{}
	handler := &EvidenceHandler{
		AdmissionDecisions: store,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/evidence/admission-decisions?domain=cloud_inventory&scope_id=git-repository-scope:team/api&generation_id=generation-1&limit=10",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("store was called for empty scoped grants")
	}
	for _, leaked := range []string{"git-repository-scope:team/api", "generation-1", "cloud_inventory"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("empty scoped response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestAdmissionDecisionScopedOutOfGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingAdmissionDecisionReadStore{}
	handler := &EvidenceHandler{
		AdmissionDecisions: store,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=git-repository-scope:team/api&generation_id=generation-1&anchor_kind=repository&anchor_id=repo://team/api&limit=10",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://team/other"},
		AllowedScopeIDs:      []string{"git-repository-scope:team/other"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("store was called for out-of-grant scoped request")
	}
	for _, leaked := range []string{"repo://team/api", "git-repository-scope:team/api", "generation-1"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("out-of-grant response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestAdmissionDecisionHandlerRejectsUnboundedOrInvalidFilters(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		AdmissionDecisions: &recordingAdmissionDecisionReadStore{},
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=scope-1",
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=scope-1&generation_id=generation-1&state=exact",
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=scope-1&generation_id=generation-1&anchor_kind=repository",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if got, want := rec.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsAdmissionDecisionRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=repo-team-a&generation_id=generation-1&limit=10",
		nil,
	)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func admissionDecisionReadRow(decisionID string, state string) AdmissionDecisionReadRow {
	return AdmissionDecisionReadRow{
		DecisionID:       decisionID,
		Domain:           "deployable_unit",
		State:            state,
		DomainState:      admissionDecisionDomainStateForTest(state),
		ScopeID:          "git-repository-scope:team/api",
		GenerationID:     "generation-1",
		AnchorKind:       "repository",
		AnchorID:         "repo://team/api",
		CandidateKind:    "service",
		CandidateID:      "service:api",
		ConfidenceScore:  0.9,
		ConfidenceBucket: "high",
		ConfidenceBasis:  "explicit relationship evidence",
		FreshnessState:   "current",
		SourceHandles: []AdmissionDecisionSourceHandle{{
			Kind:    "fact",
			ID:      "fact:relationship:1",
			ScopeID: "git-repository-scope:team/api",
		}},
		CanonicalWrite: AdmissionDecisionCanonicalWrite{
			Eligible:   state == "admitted",
			Written:    state == "admitted",
			TargetKind: "relationship",
			TargetID:   "DEPLOYS_FROM:repo://team/api:service:api",
		},
		RecommendedAction: AdmissionDecisionNextAction{
			Action: "inspect_source_handles",
			Reason: "review reducer admission evidence",
		},
		PayloadVersion: "v1",
		DecidedAt:      time.Unix(1700000000, 0).UTC(),
		UpdatedAt:      time.Unix(1700000001, 0).UTC(),
	}
}

func admissionDecisionDomainStateForTest(state string) string {
	if state == "admitted" {
		return "exact"
	}
	return state
}

type recordingAdmissionDecisionReadStore struct {
	lastFilter        AdmissionDecisionReadFilter
	lastEvidenceLimit int
	rows              []AdmissionDecisionReadRow
	evidence          map[string][]AdmissionDecisionEvidenceRow
}

func (s *recordingAdmissionDecisionReadStore) ListAdmissionDecisions(
	_ context.Context,
	filter AdmissionDecisionReadFilter,
) ([]AdmissionDecisionReadRow, error) {
	s.lastFilter = filter
	return append([]AdmissionDecisionReadRow(nil), s.rows...), nil
}

func (s *recordingAdmissionDecisionReadStore) ListAdmissionDecisionEvidence(
	_ context.Context,
	decisionID string,
	limit int,
) ([]AdmissionDecisionEvidenceRow, error) {
	s.lastEvidenceLimit = limit
	rows := append([]AdmissionDecisionEvidenceRow(nil), s.evidence[decisionID]...)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

type failingAdmissionDecisionReadStore struct {
	called bool
}

func (s *failingAdmissionDecisionReadStore) ListAdmissionDecisions(
	context.Context,
	AdmissionDecisionReadFilter,
) ([]AdmissionDecisionReadRow, error) {
	s.called = true
	return nil, errors.New("broad admission decision read")
}

func (s *failingAdmissionDecisionReadStore) ListAdmissionDecisionEvidence(
	context.Context,
	string,
	int,
) ([]AdmissionDecisionEvidenceRow, error) {
	s.called = true
	return nil, errors.New("broad admission decision evidence read")
}
