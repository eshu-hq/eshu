// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSupplyChainImpactAggregateRoutesResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		target     string
		wantCounts int
		wantInvs   int
		wantLookup int
	}{
		{
			name:       "count internal id",
			target:     "/api/v0/supply-chain/impact/findings/count?repository_id=repo://example/api",
			wantCounts: 1,
			wantLookup: 0,
		},
		{
			name:       "count repository name",
			target:     "/api/v0/supply-chain/impact/findings/count?repository_id=payments-api",
			wantCounts: 1,
			wantLookup: 1,
		},
		{
			name:       "inventory repository slug",
			target:     "/api/v0/supply-chain/impact/inventory?repository_id=example/payments-api&limit=10",
			wantInvs:   1,
			wantLookup: 1,
		},
		{
			name:       "inventory repository path",
			target:     "/api/v0/supply-chain/impact/inventory?repository_id=/srv/payments-api&limit=10",
			wantInvs:   1,
			wantLookup: 1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := selectorAggregateContentStore()
			store := &stubSupplyChainImpactAggregateStore{
				count: SupplyChainImpactAggregateCount{
					TotalFindings:    1,
					ByPriorityBucket: map[string]int{"high": 1},
					BySeverity:       map[string]int{"high": 1},
				},
				inventory: []SupplyChainImpactInventoryRow{{
					Dimension: SupplyChainImpactInventoryByRepository,
					Value:     "repo://example/api",
					Count:     1,
				}},
			}
			handler := &SupplyChainHandler{
				Content:          content,
				ImpactAggregates: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.lastFilter.RepositoryID; got != "repo://example/api" {
				t.Fatalf("RepositoryID = %q, want repo://example/api", got)
			}
			if got := store.callCountCount; got != tc.wantCounts {
				t.Fatalf("Count calls = %d, want %d", got, tc.wantCounts)
			}
			if got := store.callInvCount; got != tc.wantInvs {
				t.Fatalf("Inventory calls = %d, want %d", got, tc.wantInvs)
			}
			if got := content.matchCalls; got != tc.wantLookup {
				t.Fatalf("MatchRepositories calls = %d, want %d", got, tc.wantLookup)
			}
		})
	}
}

func TestSupplyChainSecurityAlertAggregateRoutesResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		target     string
		wantCounts int
		wantInvs   int
		wantLookup int
	}{
		{
			name:       "count internal id",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=repo://example/api",
			wantCounts: 1,
			wantLookup: 1,
		},
		{
			name:       "count repository name",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=payments-api",
			wantCounts: 1,
			wantLookup: 1,
		},
		{
			name:       "inventory repository slug",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/inventory?repository_id=example/payments-api&limit=10",
			wantInvs:   1,
			wantLookup: 1,
		},
		{
			name:       "inventory repository path",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/inventory?repository_id=/srv/payments-api&limit=10",
			wantInvs:   1,
			wantLookup: 1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := selectorAggregateContentStore()
			store := &stubSecurityAlertReconciliationAggregateStore{
				count: SecurityAlertReconciliationAggregateCount{
					TotalReconciliations:   1,
					ByReconciliationStatus: map[string]int{"both_active": 1},
					ByProvider:             map[string]int{"github_security_advisories": 1},
					ByProviderState:        map[string]int{"open": 1},
				},
				inventory: []SecurityAlertReconciliationInventoryRow{{
					Dimension: SecurityAlertReconciliationInventoryByRepository,
					Value:     "repo://example/api",
					Count:     1,
				}},
			}
			handler := &SupplyChainHandler{
				Content:                 content,
				SecurityAlertAggregates: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.lastFilter.RepositoryID; got != "repo://example/api" {
				t.Fatalf("RepositoryID = %q, want repo://example/api", got)
			}
			if got, want := strings.Join(store.lastFilter.RepositoryScopeIDs, ","), "repo://example/api,security-alert:github:example/payments-api"; got != want {
				t.Fatalf("RepositoryScopeIDs = %q, want %q", got, want)
			}
			if got := store.countCalls; got != tc.wantCounts {
				t.Fatalf("Count calls = %d, want %d", got, tc.wantCounts)
			}
			if got := store.invCalls; got != tc.wantInvs {
				t.Fatalf("Inventory calls = %d, want %d", got, tc.wantInvs)
			}
			if got := content.matchCalls; got != tc.wantLookup {
				t.Fatalf("MatchRepositories calls = %d, want %d", got, tc.wantLookup)
			}
		})
	}
}

func TestSupplyChainSecurityAlertAggregateRoutesResolveNameOnlyCatalogProviderScope(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo://example/api",
				Name: "payments-api",
			}},
		},
	}
	store := &stubSecurityAlertReconciliationAggregateStore{
		providerScopes: []string{"security-alert:github:example/payments-api"},
		count: SecurityAlertReconciliationAggregateCount{
			TotalReconciliations:   1,
			ByReconciliationStatus: map[string]int{"provider_only": 1},
			ByProvider:             map[string]int{"github": 1},
			ByProviderState:        map[string]int{"open": 1},
		},
	}
	handler := &SupplyChainHandler{
		Content:                 content,
		SecurityAlertAggregates: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(
		w,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=payments-api",
			nil,
		),
	)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := strings.Join(store.lastFilter.RepositoryScopeIDs, ","), "repo://example/api,security-alert:github:example/payments-api"; got != want {
		t.Fatalf("RepositoryScopeIDs = %q, want %q", got, want)
	}
	if got, want := strings.Join(store.providerScopeLookups, ","), "payments-api"; got != want {
		t.Fatalf("provider scope lookups = %q, want %q", got, want)
	}
}

func TestSupplyChainAggregateRoutesRejectInvalidRepositorySelector(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		target  string
		handler *SupplyChainHandler
	}{
		{
			name:   "impact count",
			target: "/api/v0/supply-chain/impact/findings/count?repository_id=unknown-repo",
			handler: &SupplyChainHandler{
				Content:          selectorAggregateContentStore(),
				ImpactAggregates: &stubSupplyChainImpactAggregateStore{},
			},
		},
		{
			name:   "impact inventory",
			target: "/api/v0/supply-chain/impact/inventory?repository_id=unknown-repo&limit=10",
			handler: &SupplyChainHandler{
				Content:          selectorAggregateContentStore(),
				ImpactAggregates: &stubSupplyChainImpactAggregateStore{},
			},
		},
		{
			name:   "security alert count",
			target: "/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=unknown-repo",
			handler: &SupplyChainHandler{
				Content:                 selectorAggregateContentStore(),
				SecurityAlertAggregates: &stubSecurityAlertReconciliationAggregateStore{},
			},
		},
		{
			name:   "security alert inventory",
			target: "/api/v0/supply-chain/security-alerts/reconciliations/inventory?repository_id=unknown-repo&limit=10",
			handler: &SupplyChainHandler{
				Content:                 selectorAggregateContentStore(),
				SecurityAlertAggregates: &stubSecurityAlertReconciliationAggregateStore{},
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			tc.handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if got, want := w.Code, http.StatusNotFound; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if body := w.Body.String(); !containsAll(body, "repository selector", "unknown-repo", "did not match") {
				t.Fatalf("body = %q, want clear selector error", body)
			}
		})
	}
}

func selectorAggregateContentStore() *countingRepositoryContentStore {
	return &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:        "repo://example/api",
				Name:      "payments-api",
				LocalPath: "/srv/payments-api",
				RepoSlug:  "example/payments-api",
			}},
		},
	}
}
