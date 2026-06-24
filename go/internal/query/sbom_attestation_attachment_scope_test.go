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

type failingSBOMAttachmentStore struct {
	called bool
}

func (s *failingSBOMAttachmentStore) ListSBOMAttestationAttachments(
	context.Context,
	SBOMAttestationAttachmentFilter,
) (SBOMAttestationAttachmentPage, error) {
	s.called = true
	return SBOMAttestationAttachmentPage{}, errors.New("broad sbom attachment read")
}

type failingSBOMAttachmentAggregateStore struct {
	countCalled     bool
	inventoryCalled bool
}

func (s *failingSBOMAttachmentAggregateStore) CountSBOMAttestationAttachments(
	context.Context,
	SBOMAttestationAttachmentAggregateFilter,
) (SBOMAttestationAttachmentAggregateCount, error) {
	s.countCalled = true
	return SBOMAttestationAttachmentAggregateCount{}, errors.New("broad sbom attachment count read")
}

func (s *failingSBOMAttachmentAggregateStore) SBOMAttestationAttachmentInventory(
	context.Context,
	SBOMAttestationAttachmentAggregateFilter,
	SBOMAttestationAttachmentInventoryDimension,
	int,
	int,
) ([]SBOMAttestationAttachmentInventoryRow, error) {
	s.inventoryCalled = true
	return nil, errors.New("broad sbom attachment inventory read")
}

func TestAuthMiddlewareWithScopedTokensAllowsSBOMAttachmentRoutes(t *testing.T) {
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
		"/api/v0/supply-chain/sbom-attestations/attachments?repository_id=repo-team-a&limit=10",
		"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo-team-a",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory?repository_id=repo-team-a&limit=10",
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

func TestSBOMAttachmentScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	attachments := &failingSBOMAttachmentStore{}
	aggregates := &failingSBOMAttachmentAggregateStore{}
	handler := &SupplyChainHandler{
		Content:                  repositorySelectorReadModelContentStore(),
		SBOMAttachments:          attachments,
		SBOMAttachmentAggregates: aggregates,
		Profile:                  ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	digest := "sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name   string
		target string
	}{
		{name: "list", target: "/api/v0/supply-chain/sbom-attestations/attachments?subject_digest=" + digest + "&limit=10"},
		{name: "count", target: "/api/v0/supply-chain/sbom-attestations/attachments/count?subject_digest=" + digest},
		{name: "inventory", target: "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=artifact_kind&limit=10"},
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
				assertZeroSBOMAttachmentsResponse(t, rec.Body.Bytes())
			case "count":
				assertZeroSBOMAttachmentCountResponse(t, rec.Body.Bytes())
			case "inventory":
				assertEmptySBOMAttachmentInventoryResponse(t, rec.Body.Bytes())
			}
		})
	}
	if attachments.called {
		t.Fatal("attachment store was called for empty scoped grants")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for empty scoped grants (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestSBOMAttachmentScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead(t *testing.T) {
	t.Parallel()

	attachments := &failingSBOMAttachmentStore{}
	aggregates := &failingSBOMAttachmentAggregateStore{}
	handler := &SupplyChainHandler{
		Content:                  repositorySelectorReadModelContentStore(),
		SBOMAttachments:          attachments,
		SBOMAttachmentAggregates: aggregates,
		Profile:                  ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/sbom-attestations/attachments?repository_id=payments-api&limit=10",
		"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=payments-api",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory?repository_id=payments-api&limit=10",
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
	if attachments.called {
		t.Fatal("attachment store was called for out-of-grant selector")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for out-of-grant selector (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestSBOMAttachmentHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	attachments := &recordingSBOMAttestationAttachmentStore{}
	aggregates := &stubSBOMAttestationAttachmentAggregateStore{
		count: SBOMAttestationAttachmentAggregateCount{
			ByAttachmentStatus: map[string]int{},
			ByArtifactKind:     map[string]int{},
		},
	}
	handler := &SupplyChainHandler{
		Content:                  repositorySelectorReadModelContentStore(),
		SBOMAttachments:          attachments,
		SBOMAttachmentAggregates: aggregates,
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
	wantGrants := []string{"git-repository-scope:example/api", "repo://example/api"}

	listReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments?repository_id=payments-api&limit=10",
		nil,
	)
	listReq = listReq.WithContext(ContextWithAuthContext(listReq.Context(), auth))
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body = %s", got, want, listRec.Body.String())
	}
	if got, want := attachments.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("list RepositoryID = %q, want %q", got, want)
	}
	if got := attachments.lastFilter.AllowedSourceRepositoryIDs; !equalPacketStringSlices(got, wantGrants) {
		t.Fatalf("list AllowedSourceRepositoryIDs = %#v, want %#v", got, wantGrants)
	}

	countReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=payments-api",
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

func TestSBOMAttachmentSQLAppliesRepositoryGrantOverlap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		query      string
		beforeText string
		predicate  string
	}{
		{
			name:       "list",
			query:      listSBOMAttestationAttachmentsQuery,
			beforeText: "ORDER BY",
			predicate:  "fact.payload->'repository_ids' ?| $12::text[]",
		},
		{
			name:       "rollup",
			query:      sbomAttestationAttachmentAggregateRollupQuery,
			beforeText: "GROUP BY",
			predicate:  "fact.payload->'repository_ids' ?| $9::text[]",
		},
		{
			name:       "inventory",
			query:      sbomAttestationAttachmentInventoryQueryTemplate,
			beforeText: "GROUP BY",
			predicate:  "fact.payload->'repository_ids' ?| $9::text[]",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tc.query, tc.predicate) {
				t.Fatalf("query missing repository grant overlap %q:\n%s", tc.predicate, tc.query)
			}
			if strings.Index(tc.query, tc.predicate) > strings.Index(tc.query, tc.beforeText) {
				t.Fatalf("grant overlap %q appears after %s:\n%s", tc.predicate, tc.beforeText, tc.query)
			}
		})
	}
	// Missing-evidence probe must be grant-bounded in BOTH CTEs so a scoped
	// token cannot detect an out-of-grant image or attachment digest.
	if got := strings.Count(sbomAttestationAttachmentMissingEvidenceQuery, "?| $5::text[]"); got != 2 {
		t.Fatalf("missing-evidence query grant overlaps = %d, want 2 (active_images + active_attachments):\n%s", got, sbomAttestationAttachmentMissingEvidenceQuery)
	}
	if !strings.Contains(sbomAttestationAttachmentInventoryQueryTemplate, "LIMIT $10 OFFSET $11") {
		t.Fatalf("inventory limit/offset must shift to $10/$11 after the grant array:\n%s", sbomAttestationAttachmentInventoryQueryTemplate)
	}
}

func assertZeroSBOMAttachmentsResponse(t *testing.T, body []byte) {
	t.Helper()
	var resp struct {
		Attachments []json.RawMessage `json:"attachments"`
		Count       int               `json:"count"`
		Truncated   bool              `json:"truncated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode attachments response: %v; body = %s", err, string(body))
	}
	if resp.Count != 0 || len(resp.Attachments) != 0 || resp.Truncated {
		t.Fatalf("empty scoped attachments page = %#v, want zero", resp)
	}
}

func assertZeroSBOMAttachmentCountResponse(t *testing.T, body []byte) {
	t.Helper()
	var resp struct {
		TotalAttachments int `json:"total_attachments"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode count response: %v; body = %s", err, string(body))
	}
	if resp.TotalAttachments != 0 {
		t.Fatalf("total_attachments = %d, want 0", resp.TotalAttachments)
	}
}

func assertEmptySBOMAttachmentInventoryResponse(t *testing.T, body []byte) {
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
