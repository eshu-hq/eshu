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

type failingSecurityAlertReconciliationStore struct {
	called bool
}

func (s *failingSecurityAlertReconciliationStore) ListSecurityAlertReconciliations(
	context.Context,
	SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	s.called = true
	return nil, errors.New("broad security alert reconciliation read")
}

type failingSecurityAlertReconciliationAggregateStore struct {
	countCalled     bool
	inventoryCalled bool
}

func (s *failingSecurityAlertReconciliationAggregateStore) CountSecurityAlertReconciliations(
	context.Context,
	SecurityAlertReconciliationAggregateFilter,
) (SecurityAlertReconciliationAggregateCount, error) {
	s.countCalled = true
	return SecurityAlertReconciliationAggregateCount{}, errors.New("broad security alert reconciliation count read")
}

func (s *failingSecurityAlertReconciliationAggregateStore) SecurityAlertReconciliationInventory(
	context.Context,
	SecurityAlertReconciliationAggregateFilter,
	SecurityAlertReconciliationInventoryDimension,
	int,
	int,
) ([]SecurityAlertReconciliationInventoryRow, error) {
	s.inventoryCalled = true
	return nil, errors.New("broad security alert reconciliation inventory read")
}

func TestAuthMiddlewareWithScopedTokensAllowsSecurityAlertReconciliationRoutes(t *testing.T) {
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
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo-team-a&limit=10",
		"/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=repo-team-a",
		"/api/v0/supply-chain/security-alerts/reconciliations/inventory?repository_id=repo-team-a&limit=10",
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

func TestSecurityAlertReconciliationScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	alerts := &failingSecurityAlertReconciliationStore{}
	aggregates := &failingSecurityAlertReconciliationAggregateStore{}
	handler := &SupplyChainHandler{
		Content:                 repositorySelectorReadModelContentStore(),
		SecurityAlerts:          alerts,
		SecurityAlertAggregates: aggregates,
		Profile:                 ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name   string
		target string
	}{
		{name: "list", target: "/api/v0/supply-chain/security-alerts/reconciliations?cve_id=CVE-2026-0001&limit=10"},
		{name: "count", target: "/api/v0/supply-chain/security-alerts/reconciliations/count?cve_id=CVE-2026-0001"},
		{name: "inventory", target: "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=provider&limit=10"},
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
				assertZeroSecurityAlertReconciliationsResponse(t, rec.Body.Bytes())
			case "count":
				assertZeroSecurityAlertReconciliationCountResponse(t, rec.Body.Bytes())
			case "inventory":
				assertEmptySecurityAlertReconciliationInventoryResponse(t, rec.Body.Bytes())
			}
			if strings.Contains(rec.Body.String(), "CVE-2026-0001") {
				t.Fatalf("empty scoped response echoed requested anchor: %s", rec.Body.String())
			}
		})
	}
	if alerts.called {
		t.Fatal("reconciliation store was called for empty scoped grants")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for empty scoped grants (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestSecurityAlertReconciliationScopedSelectorDeniesOutOfGrantWithoutStoreRead(t *testing.T) {
	t.Parallel()

	alerts := &failingSecurityAlertReconciliationStore{}
	aggregates := &failingSecurityAlertReconciliationAggregateStore{}
	handler := &SupplyChainHandler{
		Content:                 repositorySelectorReadModelContentStore(),
		SecurityAlerts:          alerts,
		SecurityAlertAggregates: aggregates,
		Profile:                 ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=payments-api&limit=10",
		"/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=payments-api",
		"/api/v0/supply-chain/security-alerts/reconciliations/inventory?repository_id=payments-api&limit=10",
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
	if alerts.called {
		t.Fatal("reconciliation store was called for out-of-grant selector")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for out-of-grant selector (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestSecurityAlertReconciliationHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	alerts := &recordingSecurityAlertReconciliationStore{}
	aggregates := &stubSecurityAlertReconciliationAggregateStore{
		count: SecurityAlertReconciliationAggregateCount{
			ByReconciliationStatus: map[string]int{},
			ByProvider:             map[string]int{},
			ByProviderState:        map[string]int{},
			BySourceFreshness:      map[string]int{},
		},
	}
	handler := &SupplyChainHandler{
		Content:                 repositorySelectorReadModelContentStore(),
		SecurityAlerts:          alerts,
		SecurityAlertAggregates: aggregates,
		Profile:                 ProfileProduction,
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
	wantGrants := []string{"git-repository-scope:example/api", "repo://example/api"}

	listReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=payments-api&limit=10",
		nil,
	)
	listReq = listReq.WithContext(ContextWithAuthContext(listReq.Context(), auth))
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body = %s", got, want, listRec.Body.String())
	}
	if got, want := alerts.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("list RepositoryID = %q, want %q", got, want)
	}
	if got := alerts.lastFilter.AllowedSourceRepositoryIDs; !equalPacketStringSlices(got, wantGrants) {
		t.Fatalf("list AllowedSourceRepositoryIDs = %#v, want %#v", got, wantGrants)
	}

	countReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=payments-api",
		nil,
	)
	countReq = countReq.WithContext(ContextWithAuthContext(countReq.Context(), auth))
	countRec := httptest.NewRecorder()
	mux.ServeHTTP(countRec, countReq)
	if got, want := countRec.Code, http.StatusOK; got != want {
		t.Fatalf("count status = %d, want %d; body = %s", got, want, countRec.Body.String())
	}
	if got := aggregates.lastFilter.AllowedSourceRepositoryIDs; !equalPacketStringSlices(got, wantGrants) {
		t.Fatalf("count AllowedSourceRepositoryIDs = %#v, want %#v", got, wantGrants)
	}
}

func TestSecurityAlertReconciliationSQLAppliesScopedGrant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		query      string
		beforeText string
		predicate  string
	}{
		{
			// The list query has a window-function ORDER BY inside the CTE, so
			// anchor on the final LIMIT to assert the grant predicate precedes
			// pagination.
			name:       "list",
			query:      listSecurityAlertReconciliationsQuery,
			beforeText: "LIMIT $10",
			predicate:  "cardinality($11::text[]) = 0",
		},
		{
			name:       "total",
			query:      securityAlertReconciliationAggregateTotalQuery,
			beforeText: ";",
			predicate:  "COALESCE(cardinality($8::text[]), 0) = 0",
		},
		{
			name:       "group",
			query:      securityAlertReconciliationAggregateGroupQueryTemplate,
			beforeText: "GROUP BY",
			predicate:  "COALESCE(cardinality($8::text[]), 0) = 0",
		},
		{
			name:       "inventory",
			query:      securityAlertReconciliationInventoryQueryTemplate,
			beforeText: "GROUP BY",
			predicate:  "COALESCE(cardinality($8::text[]), 0) = 0",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tc.query, tc.predicate) {
				t.Fatalf("query missing scoped grant predicate %q:\n%s", tc.predicate, tc.query)
			}
			if strings.Index(tc.query, tc.predicate) > strings.Index(tc.query, tc.beforeText) {
				t.Fatalf("grant predicate %q appears after %s:\n%s", tc.predicate, tc.beforeText, tc.query)
			}
		})
	}
	if !strings.Contains(securityAlertReconciliationInventoryQueryTemplate, "LIMIT $9 OFFSET $10") {
		t.Fatalf("inventory limit/offset must shift to $9/$10 after the grant array:\n%s", securityAlertReconciliationInventoryQueryTemplate)
	}
}

func assertZeroSecurityAlertReconciliationsResponse(t *testing.T, body []byte) {
	t.Helper()
	var resp struct {
		Reconciliations []json.RawMessage `json:"reconciliations"`
		Count           int               `json:"count"`
		Truncated       bool              `json:"truncated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode reconciliations response: %v; body = %s", err, string(body))
	}
	if resp.Count != 0 || len(resp.Reconciliations) != 0 || resp.Truncated {
		t.Fatalf("empty scoped reconciliations page = %#v, want zero", resp)
	}
}

func assertZeroSecurityAlertReconciliationCountResponse(t *testing.T, body []byte) {
	t.Helper()
	var resp struct {
		TotalReconciliations int `json:"total_reconciliations"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode count response: %v; body = %s", err, string(body))
	}
	if resp.TotalReconciliations != 0 {
		t.Fatalf("total_reconciliations = %d, want 0", resp.TotalReconciliations)
	}
}

func assertEmptySecurityAlertReconciliationInventoryResponse(t *testing.T, body []byte) {
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
		t.Fatalf("empty scoped inventory page = %#v, want zero", resp)
	}
}
