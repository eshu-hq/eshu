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
)

// failingSupplyChainImpactFindingStore records whether the reducer impact
// finding store was read and always errors, so scope tests can prove that
// empty or out-of-grant scoped requests never touch the store.
type failingSupplyChainImpactFindingStore struct {
	called bool
}

func (s *failingSupplyChainImpactFindingStore) ListSupplyChainImpactFindings(
	context.Context,
	SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	s.called = true
	return nil, errors.New("broad supply-chain impact finding read")
}

// failingSupplyChainImpactAggregateStore records whether either aggregate read
// ran for scope tests that must short-circuit before store access.
type failingSupplyChainImpactAggregateStore struct {
	countCalled     bool
	inventoryCalled bool
}

func (s *failingSupplyChainImpactAggregateStore) CountSupplyChainImpactFindings(
	context.Context,
	SupplyChainImpactAggregateFilter,
) (SupplyChainImpactAggregateCount, error) {
	s.countCalled = true
	return SupplyChainImpactAggregateCount{}, errors.New("broad supply-chain impact count read")
}

func (s *failingSupplyChainImpactAggregateStore) SupplyChainImpactInventory(
	context.Context,
	SupplyChainImpactAggregateFilter,
	SupplyChainImpactInventoryDimension,
	int,
	int,
) ([]SupplyChainImpactInventoryRow, error) {
	s.inventoryCalled = true
	return nil, errors.New("broad supply-chain impact inventory read")
}

// failingSupplyChainImpactReadinessStore proves the readiness lookup is also
// skipped for empty scoped grants.
type failingSupplyChainImpactReadinessStore struct {
	called bool
}

func (s *failingSupplyChainImpactReadinessStore) ReadSupplyChainImpactReadiness(
	context.Context,
	SupplyChainImpactReadinessQuery,
) (SupplyChainImpactReadinessSnapshot, error) {
	s.called = true
	return SupplyChainImpactReadinessSnapshot{}, errors.New("broad supply-chain impact readiness read")
}

func TestAuthMiddlewareWithScopedTokensAllowsSupplyChainImpactRoutes(t *testing.T) {
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

	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?repository_id=repo-team-a&limit=10",
		"/api/v0/supply-chain/impact/findings/count?repository_id=repo-team-a",
		"/api/v0/supply-chain/impact/inventory?repository_id=repo-team-a&limit=10",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusNoContent; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensRejectsAdjacentSupplyChainImpactRoutes(t *testing.T) {
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
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// Adjacent supply-chain reads stay fail-closed for scoped tokens until
	// each is separately proven tenant-filtered (issue #2124 scope).
	for _, target := range []string{
		"/api/v0/supply-chain/impact/explain?repository_id=repo-team-a&advisory_id=CVE-2026-0001",
		"/api/v0/supply-chain/advisories?limit=10",
		"/api/v0/supply-chain/vulnerabilities/CVE-2026-0001",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusForbidden; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestSupplyChainImpactScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	findings := &failingSupplyChainImpactFindingStore{}
	aggregates := &failingSupplyChainImpactAggregateStore{}
	readiness := &failingSupplyChainImpactReadinessStore{}
	handler := &SupplyChainHandler{
		Content:          repositorySelectorReadModelContentStore(),
		ImpactFindings:   findings,
		ImpactAggregates: aggregates,
		Readiness:        readiness,
		Profile:          ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name   string
		target string
	}{
		{
			name:   "list",
			target: "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10",
		},
		{
			name:   "count",
			target: "/api/v0/supply-chain/impact/findings/count?cve_id=CVE-2026-0001",
		},
		{
			name:   "inventory",
			target: "/api/v0/supply-chain/impact/inventory?group_by=ecosystem&limit=10",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
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
			switch tc.name {
			case "list":
				assertZeroImpactFindingsResponse(t, rec.Body.Bytes())
			case "count":
				assertZeroImpactCountResponse(t, rec.Body.Bytes())
			case "inventory":
				assertEmptyImpactInventoryResponse(t, rec.Body.Bytes())
			}
			if strings.Contains(rec.Body.String(), "CVE-2026-0001") {
				t.Fatalf("empty scoped response echoed requested anchor: %s", rec.Body.String())
			}
		})
	}
	if findings.called {
		t.Fatal("impact finding store was called for empty scoped grants")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for empty scoped grants (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
	if readiness.called {
		t.Fatal("readiness store was called for empty scoped grants")
	}
}

func TestSupplyChainImpactScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead(t *testing.T) {
	t.Parallel()

	findings := &failingSupplyChainImpactFindingStore{}
	aggregates := &failingSupplyChainImpactAggregateStore{}
	readiness := &failingSupplyChainImpactReadinessStore{}
	handler := &SupplyChainHandler{
		Content:          repositorySelectorReadModelContentStore(),
		ImpactFindings:   findings,
		ImpactAggregates: aggregates,
		Readiness:        readiness,
		Profile:          ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?repository_id=payments-api&limit=10",
		"/api/v0/supply-chain/impact/findings/count?repository_id=payments-api",
		"/api/v0/supply-chain/impact/inventory?repository_id=payments-api&limit=10",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo://example/other"},
			}))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusNotFound; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "repo://example/api") {
				t.Fatalf("out-of-grant response leaked repository id: %s", rec.Body.String())
			}
		})
	}
	if findings.called {
		t.Fatal("impact finding store was called for out-of-grant repository selector")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for out-of-grant repository selector (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestSupplyChainImpactHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	findings := &recordingSupplyChainImpactFindingStore{}
	aggregates := &stubSupplyChainImpactAggregateStore{
		count: SupplyChainImpactAggregateCount{
			ByPriorityBucket: map[string]int{},
			BySeverity:       map[string]int{},
		},
	}
	handler := &SupplyChainHandler{
		Content:          repositorySelectorReadModelContentStore(),
		ImpactFindings:   findings,
		ImpactAggregates: aggregates,
		Profile:          ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	auth := AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://example/api"},
		AllowedScopeIDs:      []string{"git-repository-scope:example/api"},
	}

	wantRepos := []string{"repo://example/api"}
	wantScopes := []string{"git-repository-scope:example/api"}

	listReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=payments-api&limit=10",
		nil,
	)
	listReq = listReq.WithContext(ContextWithAuthContext(listReq.Context(), auth))
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body = %s", got, want, listRec.Body.String())
	}
	if got, want := findings.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("list RepositoryID = %q, want %q", got, want)
	}
	if got := findings.lastFilter.AllowedRepositoryIDs; !equalPacketStringSlices(got, wantRepos) {
		t.Fatalf("list AllowedRepositoryIDs = %#v, want %#v", got, wantRepos)
	}
	if got := findings.lastFilter.AllowedScopeIDs; !equalPacketStringSlices(got, wantScopes) {
		t.Fatalf("list AllowedScopeIDs = %#v, want %#v", got, wantScopes)
	}

	countReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings/count?repository_id=payments-api",
		nil,
	)
	countReq = countReq.WithContext(ContextWithAuthContext(countReq.Context(), auth))
	countRec := httptest.NewRecorder()
	mux.ServeHTTP(countRec, countReq)
	if got, want := countRec.Code, http.StatusOK; got != want {
		t.Fatalf("count status = %d, want %d; body = %s", got, want, countRec.Body.String())
	}
	if got := aggregates.lastCountFilter.AllowedRepositoryIDs; !equalPacketStringSlices(got, wantRepos) {
		t.Fatalf("count AllowedRepositoryIDs = %#v, want %#v", got, wantRepos)
	}
	if got := aggregates.lastCountFilter.AllowedScopeIDs; !equalPacketStringSlices(got, wantScopes) {
		t.Fatalf("count AllowedScopeIDs = %#v, want %#v", got, wantScopes)
	}

	invReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/inventory?repository_id=payments-api&group_by=severity&limit=10",
		nil,
	)
	invReq = invReq.WithContext(ContextWithAuthContext(invReq.Context(), auth))
	invRec := httptest.NewRecorder()
	mux.ServeHTTP(invRec, invReq)
	if got, want := invRec.Code, http.StatusOK; got != want {
		t.Fatalf("inventory status = %d, want %d; body = %s", got, want, invRec.Body.String())
	}
	if got := aggregates.lastInvFilter.AllowedRepositoryIDs; !equalPacketStringSlices(got, wantRepos) {
		t.Fatalf("inventory AllowedRepositoryIDs = %#v, want %#v", got, wantRepos)
	}
	if got := aggregates.lastInvFilter.AllowedScopeIDs; !equalPacketStringSlices(got, wantScopes) {
		t.Fatalf("inventory AllowedScopeIDs = %#v, want %#v", got, wantScopes)
	}
}

func TestSupplyChainImpactSQLAppliesScopedAuthorizationBeforeOrderingAndGrouping(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		query      string
		beforeText string
		repoParam  string
		scopeParam string
	}{
		{
			name:       "list",
			query:      listSupplyChainImpactFindingsQuery,
			beforeText: "ORDER BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($22::text[])",
			scopeParam: "fact.scope_id = ANY($23::text[])",
		},
		{
			name:       "aggregate_cte",
			query:      supplyChainImpactAggregateCanonicalFactsCTE,
			beforeText: "ranked_facts",
			repoParam:  "fact.payload->>'repository_id' = ANY($18::text[])",
			scopeParam: "fact.scope_id = ANY($19::text[])",
		},
		{
			name:       "inventory",
			query:      supplyChainImpactInventoryQueryTemplate,
			beforeText: "GROUP BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($18::text[])",
			scopeParam: "fact.scope_id = ANY($19::text[])",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{tc.repoParam, tc.scopeParam} {
				if !strings.Contains(tc.query, want) {
					t.Fatalf("query missing %q:\n%s", want, tc.query)
				}
				if strings.Index(tc.query, want) > strings.Index(tc.query, tc.beforeText) {
					t.Fatalf("authorization predicate %q appears after %s:\n%s", want, tc.beforeText, tc.query)
				}
			}
		})
	}
}

func assertZeroImpactFindingsResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp struct {
		Findings  []json.RawMessage `json:"findings"`
		Count     int               `json:"count"`
		Truncated bool              `json:"truncated"`
		Readiness struct {
			State string `json:"readiness_state"`
		} `json:"readiness"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode findings response: %v; body = %s", err, string(body))
	}
	if resp.Count != 0 || len(resp.Findings) != 0 || resp.Truncated {
		t.Fatalf("empty scoped findings page = %#v, want zero findings", resp)
	}
	if resp.Readiness.State != string(ReadinessStateReadinessUnavailable) {
		t.Fatalf("empty scoped readiness state = %q, want %q", resp.Readiness.State, ReadinessStateReadinessUnavailable)
	}
}

func assertZeroImpactCountResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp struct {
		TotalFindings int `json:"total_findings"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode count response: %v; body = %s", err, string(body))
	}
	if resp.TotalFindings != 0 {
		t.Fatalf("total_findings = %d, want 0", resp.TotalFindings)
	}
}

func assertEmptyImpactInventoryResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp struct {
		Buckets   []json.RawMessage `json:"buckets"`
		Count     int               `json:"count"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode inventory response: %v; body = %s", err, string(body))
	}
	if resp.Count != 0 || len(resp.Buckets) != 0 || resp.Truncated {
		t.Fatalf("empty scoped inventory page = %#v, want zero buckets", resp)
	}
}
