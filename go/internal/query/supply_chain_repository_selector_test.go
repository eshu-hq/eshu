// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type countingRepositoryContentStore struct {
	fakePortContentStore
	matchCalls int
}

func (s *countingRepositoryContentStore) MatchRepositories(
	ctx context.Context,
	selector string,
) ([]RepositoryCatalogEntry, error) {
	s.matchCalls++
	return s.fakePortContentStore.MatchRepositories(ctx, selector)
}

type canonicalRepositoryImpactStore struct {
	rows       []SupplyChainImpactFindingRow
	lastFilter SupplyChainImpactFindingFilter
	calls      int
}

func (s *canonicalRepositoryImpactStore) ListSupplyChainImpactFindings(
	_ context.Context,
	filter SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	s.calls++
	s.lastFilter = filter
	if filter.RepositoryID != "repo://example/api" {
		return nil, fmt.Errorf("repository_id = %q, want repo://example/api", filter.RepositoryID)
	}
	return append([]SupplyChainImpactFindingRow(nil), s.rows...), nil
}

type canonicalRepositorySecurityAlertStore struct {
	rows                 []SecurityAlertReconciliationRow
	providerScopes       []string
	providerScopeErr     error
	providerScopeLookups []string
	lastFilter           SecurityAlertReconciliationFilter
	calls                int
}

func (s *canonicalRepositorySecurityAlertStore) ListSecurityAlertReconciliations(
	_ context.Context,
	filter SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	s.calls++
	s.lastFilter = filter
	if filter.RepositoryID != "repo://example/api" {
		return nil, fmt.Errorf("repository_id = %q, want repo://example/api", filter.RepositoryID)
	}
	return append([]SecurityAlertReconciliationRow(nil), s.rows...), nil
}

func (s *canonicalRepositorySecurityAlertStore) SecurityAlertProviderRepositoryScopes(
	_ context.Context,
	repositoryName string,
) ([]string, error) {
	s.providerScopeLookups = append(s.providerScopeLookups, repositoryName)
	if s.providerScopeErr != nil {
		return nil, s.providerScopeErr
	}
	return append([]string(nil), s.providerScopes...), nil
}

func TestSupplyChainListImpactFindingsResolvesRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		selector  string
		wantCalls int
	}{
		{name: "internal_id", selector: "repo://example/api", wantCalls: 0},
		{name: "name", selector: "payments-api", wantCalls: 1},
		{name: "slug", selector: "example/payments-api", wantCalls: 1},
		{name: "path", selector: "/srv/payments-api", wantCalls: 1},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := &countingRepositoryContentStore{
				fakePortContentStore: fakePortContentStore{
					repositories: []RepositoryCatalogEntry{{
						ID:        "repo://example/api",
						Name:      "payments-api",
						LocalPath: "/srv/payments-api",
						RepoSlug:  "example/payments-api",
					}},
				},
			}
			store := &canonicalRepositoryImpactStore{
				rows: []SupplyChainImpactFindingRow{{
					FindingID:    "finding-1",
					RepositoryID: "repo://example/api",
					ImpactStatus: "affected_exact",
				}},
			}
			handler := &SupplyChainHandler{
				Content:        content,
				ImpactFindings: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/supply-chain/impact/findings?"+url.Values{
					"repository_id": []string{tc.selector},
					"limit":         []string{"10"},
				}.Encode(),
				nil,
			)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.lastFilter.RepositoryID; got != "repo://example/api" {
				t.Fatalf("RepositoryID = %q, want repo://example/api", got)
			}
			if got := content.matchCalls; got != tc.wantCalls {
				t.Fatalf("MatchRepositories calls = %d, want %d", got, tc.wantCalls)
			}

			var resp struct {
				Findings []SupplyChainImpactFindingResult `json:"findings"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got := len(resp.Findings); got != 1 {
				t.Fatalf("len(findings) = %d, want 1", got)
			}
		})
	}
}

func TestSupplyChainListImpactFindingsRejectsInvalidRepositorySelector(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo://example/api",
				Name: "payments-api",
			}},
		},
	}
	store := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{
		Content:        content,
		ImpactFindings: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=unknown-repo&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.RepositoryID != "" {
		t.Fatalf("store received filter %#v, want no store call", store.lastFilter)
	}
	if body := w.Body.String(); body == "" ||
		!containsAll(body, "repository selector", "unknown-repo", "did not match") {
		t.Fatalf("body = %q, want clear selector error", body)
	}
}

func TestSupplyChainSecurityAlertReconciliationsResolveRepositorySelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		selector  string
		wantCalls int
	}{
		{name: "internal_id", selector: "repo://example/api", wantCalls: 1},
		{name: "name", selector: "payments-api", wantCalls: 1},
		{name: "slug", selector: "example/payments-api", wantCalls: 1},
		{name: "path", selector: "/srv/payments-api", wantCalls: 1},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := &countingRepositoryContentStore{
				fakePortContentStore: fakePortContentStore{
					repositories: []RepositoryCatalogEntry{{
						ID:        "repo://example/api",
						Name:      "payments-api",
						LocalPath: "/srv/payments-api",
						RepoSlug:  "example/payments-api",
					}},
				},
			}
			store := &canonicalRepositorySecurityAlertStore{
				rows: []SecurityAlertReconciliationRow{{
					ReconciliationID: "reconciliation-1",
					ProviderAlert: ProviderSecurityAlertRow{
						RepositoryID: "repo://example/api",
					},
					ReconciliationStatus: "matched",
				}},
			}
			handler := &SupplyChainHandler{
				Content:        content,
				SecurityAlerts: store,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/supply-chain/security-alerts/reconciliations?"+url.Values{
					"repository_id": []string{tc.selector},
					"limit":         []string{"10"},
				}.Encode(),
				nil,
			)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got := store.lastFilter.RepositoryID; got != "repo://example/api" {
				t.Fatalf("RepositoryID = %q, want repo://example/api", got)
			}
			if got, want := strings.Join(store.lastFilter.RepositoryScopeIDs, ","), "repo://example/api,security-alert:github:example/payments-api"; got != want {
				t.Fatalf("RepositoryScopeIDs = %q, want %q", got, want)
			}
			if got := content.matchCalls; got != tc.wantCalls {
				t.Fatalf("MatchRepositories calls = %d, want %d", got, tc.wantCalls)
			}

			var resp struct {
				Reconciliations []SecurityAlertReconciliationResult `json:"reconciliations"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got := len(resp.Reconciliations); got != 1 {
				t.Fatalf("len(reconciliations) = %d, want 1", got)
			}
		})
	}
}

func TestSupplyChainSecurityAlertReconciliationsDeriveProviderScopeFromRemoteURL(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:        "repo://example/api",
				Name:      "payments-api",
				RemoteURL: "git@github.com:Example/Payments-API.git",
			}},
		},
	}
	store := &canonicalRepositorySecurityAlertStore{
		rows: []SecurityAlertReconciliationRow{{
			ReconciliationID:     "reconciliation-provider-only",
			ReconciliationStatus: "provider_only",
		}},
	}
	handler := &SupplyChainHandler{
		Content:        content,
		SecurityAlerts: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?"+url.Values{
			"repository_id": []string{"git@github.com:Example/Payments-API.git"},
			"limit":         []string{"10"},
		}.Encode(),
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := strings.Join(store.lastFilter.RepositoryScopeIDs, ","), "repo://example/api,security-alert:github:example/payments-api"; got != want {
		t.Fatalf("RepositoryScopeIDs = %q, want %q", got, want)
	}
}

func TestSupplyChainSecurityAlertReconciliationsResolveNameOnlyCatalogProviderScope(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo://example/api",
				Name: "payments-api",
			}},
		},
	}
	store := &canonicalRepositorySecurityAlertStore{
		providerScopes: []string{"security-alert:github:example/payments-api"},
		rows: []SecurityAlertReconciliationRow{{
			ReconciliationID:     "reconciliation-provider-only",
			ReconciliationStatus: "provider_only",
		}},
	}
	handler := &SupplyChainHandler{
		Content:        content,
		SecurityAlerts: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := strings.Join(store.lastFilter.RepositoryScopeIDs, ","), "repo://example/api,security-alert:github:example/payments-api"; got != want {
		t.Fatalf("RepositoryScopeIDs = %q, want %q", got, want)
	}
	if got, want := strings.Join(store.providerScopeLookups, ","), "payments-api"; got != want {
		t.Fatalf("provider scope lookups = %q, want %q", got, want)
	}
}

func TestSupplyChainSecurityAlertReconciliationsRejectAmbiguousNameOnlyProviderScope(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo://example/api",
				Name: "payments-api",
			}},
		},
	}
	store := &canonicalRepositorySecurityAlertStore{
		providerScopes: []string{
			"security-alert:github:example/payments-api",
			"security-alert:github:other/payments-api",
		},
	}
	handler := &SupplyChainHandler{
		Content:        content,
		SecurityAlerts: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.RepositoryID != "" {
		t.Fatalf("store received filter %#v, want no store call", store.lastFilter)
	}
	if body := w.Body.String(); body == "" ||
		!containsAll(body, "repository selector", "payments-api", "provider security alert") {
		t.Fatalf("body = %q, want provider scope ambiguity error", body)
	}
}

func TestSupplyChainSecurityAlertReconciliationsRejectInvalidRepositorySelector(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo://example/api",
				Name: "payments-api",
			}},
		},
	}
	store := &recordingSecurityAlertReconciliationStore{}
	handler := &SupplyChainHandler{
		Content:        content,
		SecurityAlerts: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=unknown-repo&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.RepositoryID != "" {
		t.Fatalf("store received filter %#v, want no store call", store.lastFilter)
	}
	if body := w.Body.String(); body == "" ||
		!containsAll(body, "repository selector", "unknown-repo", "did not match") {
		t.Fatalf("body = %q, want clear selector error", body)
	}
}

func containsAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
