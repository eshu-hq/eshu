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

type failingContainerImageIdentityStore struct {
	called bool
}

func (s *failingContainerImageIdentityStore) ListContainerImageIdentities(
	context.Context,
	ContainerImageIdentityFilter,
) ([]ContainerImageIdentityRow, error) {
	s.called = true
	return nil, errors.New("broad container image identity read")
}

type failingContainerImageIdentityAggregateStore struct {
	countCalled     bool
	inventoryCalled bool
}

func (s *failingContainerImageIdentityAggregateStore) CountContainerImageIdentities(
	context.Context,
	ContainerImageIdentityAggregateFilter,
) (ContainerImageIdentityAggregateCount, error) {
	s.countCalled = true
	return ContainerImageIdentityAggregateCount{}, errors.New("broad container image identity count read")
}

func (s *failingContainerImageIdentityAggregateStore) ContainerImageIdentityInventory(
	context.Context,
	ContainerImageIdentityAggregateFilter,
	ContainerImageIdentityInventoryDimension,
	int,
	int,
) ([]ContainerImageIdentityInventoryRow, error) {
	s.inventoryCalled = true
	return nil, errors.New("broad container image identity inventory read")
}

func TestAuthMiddlewareWithScopedTokensAllowsContainerImageIdentityRoutes(t *testing.T) {
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
		"/api/v0/supply-chain/container-images/identities?source_repository_id=repo-team-a&limit=10",
		"/api/v0/supply-chain/container-images/identities/count?source_repository_id=repo-team-a",
		"/api/v0/supply-chain/container-images/identities/inventory?source_repository_id=repo-team-a&limit=10",
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

func TestAuthMiddlewareWithScopedTokensRejectsAdjacentContainerImageRoutes(t *testing.T) {
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

	for _, target := range []string{
		"/api/v0/supply-chain/advisories?limit=10",
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

func TestContainerImageIdentityScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	identities := &failingContainerImageIdentityStore{}
	aggregates := &failingContainerImageIdentityAggregateStore{}
	handler := &SupplyChainHandler{
		Content:                  repositorySelectorReadModelContentStore(),
		ContainerImageIdentities: identities,
		ContainerImageAggregates: aggregates,
		Profile:                  ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name   string
		target string
	}{
		{name: "list", target: "/api/v0/supply-chain/container-images/identities?digest=sha256:" + strings.Repeat("a", 64) + "&limit=10"},
		{name: "count", target: "/api/v0/supply-chain/container-images/identities/count?digest=sha256:" + strings.Repeat("a", 64)},
		{name: "inventory", target: "/api/v0/supply-chain/container-images/identities/inventory?group_by=outcome&limit=10"},
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
				assertZeroContainerImageIdentitiesResponse(t, rec.Body.Bytes())
			case "count":
				assertZeroContainerImageIdentityCountResponse(t, rec.Body.Bytes())
			case "inventory":
				assertEmptyContainerImageIdentityInventoryResponse(t, rec.Body.Bytes())
			}
		})
	}
	if identities.called {
		t.Fatal("identity store was called for empty scoped grants")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for empty scoped grants (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestContainerImageIdentityScopedSourceSelectorDeniesOutOfGrantWithoutStoreRead(t *testing.T) {
	t.Parallel()

	identities := &failingContainerImageIdentityStore{}
	aggregates := &failingContainerImageIdentityAggregateStore{}
	handler := &SupplyChainHandler{
		Content:                  repositorySelectorReadModelContentStore(),
		ContainerImageIdentities: identities,
		ContainerImageAggregates: aggregates,
		Profile:                  ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/container-images/identities?source_repository_id=payments-api&limit=10",
		"/api/v0/supply-chain/container-images/identities/count?source_repository_id=payments-api",
		"/api/v0/supply-chain/container-images/identities/inventory?source_repository_id=payments-api&limit=10",
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
	if identities.called {
		t.Fatal("identity store was called for out-of-grant source selector")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for out-of-grant source selector (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestContainerImageIdentityHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	identities := &recordingContainerImageIdentityStore{}
	aggregates := &stubContainerImageIdentityAggregateStore{
		count: ContainerImageIdentityAggregateCount{
			ByOutcome:          map[string]int{},
			ByIdentityStrength: map[string]int{},
		},
	}
	handler := &SupplyChainHandler{
		Content:                  repositorySelectorReadModelContentStore(),
		ContainerImageIdentities: identities,
		ContainerImageAggregates: aggregates,
		Profile:                  ProfileProduction,
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
	// The grant predicate overlaps source_repository_ids with the union of
	// granted repository and scope ids.
	wantGrants := []string{"git-repository-scope:example/api", "repo://example/api"}

	listReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities?source_repository_id=payments-api&limit=10",
		nil,
	)
	listReq = listReq.WithContext(ContextWithAuthContext(listReq.Context(), auth))
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body = %s", got, want, listRec.Body.String())
	}
	if got, want := identities.lastFilter.SourceRepositoryID, "repo://example/api"; got != want {
		t.Fatalf("list SourceRepositoryID = %q, want %q", got, want)
	}
	if got := identities.lastFilter.AllowedSourceRepositoryIDs; !equalPacketStringSlices(got, wantGrants) {
		t.Fatalf("list AllowedSourceRepositoryIDs = %#v, want %#v", got, wantGrants)
	}

	countReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities/count?source_repository_id=payments-api",
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

	invReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities/inventory?source_repository_id=payments-api&group_by=outcome&limit=10",
		nil,
	)
	invReq = invReq.WithContext(ContextWithAuthContext(invReq.Context(), auth))
	invRec := httptest.NewRecorder()
	mux.ServeHTTP(invRec, invReq)
	if got, want := invRec.Code, http.StatusOK; got != want {
		t.Fatalf("inventory status = %d, want %d; body = %s", got, want, invRec.Body.String())
	}
	if got := aggregates.lastFilter.AllowedSourceRepositoryIDs; !equalPacketStringSlices(got, wantGrants) {
		t.Fatalf("inventory AllowedSourceRepositoryIDs = %#v, want %#v", got, wantGrants)
	}
}

func TestContainerImageIdentitySQLAppliesSourceRepositoryGrantOverlap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		query      string
		beforeText string
		predicate  string
	}{
		{
			name:       "list",
			query:      listContainerImageIdentitiesQuery,
			beforeText: "ORDER BY",
			predicate:  "fact.payload->'source_repository_ids' ?| $9::text[]",
		},
		{
			name:       "total",
			query:      containerImageIdentityAggregateTotalQuery,
			beforeText: ";",
			predicate:  "fact.payload->'source_repository_ids' ?| $6::text[]",
		},
		{
			name:       "group",
			query:      containerImageIdentityAggregateGroupQueryTemplate,
			beforeText: "GROUP BY",
			predicate:  "fact.payload->'source_repository_ids' ?| $6::text[]",
		},
		{
			name:       "inventory",
			query:      containerImageIdentityInventoryQueryTemplate,
			beforeText: "GROUP BY",
			predicate:  "fact.payload->'source_repository_ids' ?| $6::text[]",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tc.query, tc.predicate) {
				t.Fatalf("query missing source-repo grant overlap %q:\n%s", tc.predicate, tc.query)
			}
			if strings.Index(tc.query, tc.predicate) > strings.Index(tc.query, tc.beforeText) {
				t.Fatalf("grant overlap %q appears after %s:\n%s", tc.predicate, tc.beforeText, tc.query)
			}
		})
	}
	if !strings.Contains(containerImageIdentityInventoryQueryTemplate, "LIMIT $7 OFFSET $8") {
		t.Fatalf("inventory limit/offset must shift to $7/$8 after the grant array:\n%s", containerImageIdentityInventoryQueryTemplate)
	}
}

func assertZeroContainerImageIdentitiesResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp struct {
		Identities []json.RawMessage `json:"identities"`
		Count      int               `json:"count"`
		Truncated  bool              `json:"truncated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode identities response: %v; body = %s", err, string(body))
	}
	if resp.Count != 0 || len(resp.Identities) != 0 || resp.Truncated {
		t.Fatalf("empty scoped identities page = %#v, want zero identities", resp)
	}
}

func assertZeroContainerImageIdentityCountResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp struct {
		TotalIdentities int `json:"total_identities"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode count response: %v; body = %s", err, string(body))
	}
	if resp.TotalIdentities != 0 {
		t.Fatalf("total_identities = %d, want 0", resp.TotalIdentities)
	}
}

func assertEmptyContainerImageIdentityInventoryResponse(t *testing.T, body []byte) {
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
