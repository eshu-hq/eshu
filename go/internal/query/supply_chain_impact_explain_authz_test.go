// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSupplyChainImpactExplainScopedGrantsAcrossTenants is the #5167 W5
// two-tenant proof for GET /api/v0/supply-chain/impact/explain: tenant A's
// scoped grant threads its exact AllowedRepositoryIDs/AllowedScopeIDs into
// the store filter and resolves its own repository selector, while tenant
// B's disjoint grant is denied the same repository selector before any
// explanation store read (proven by the earlier
// TestSupplyChainImpactScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead
// case for this same route, in supply_chain_impact_scope_test.go). This test
// proves the granted side of that pair: the filter tenant A's request
// reaches the store carries only tenant A's grant, never tenant B's.
func TestSupplyChainImpactExplainScopedGrantsAcrossTenants(t *testing.T) {
	t.Parallel()

	explanations := &recordingSupplyChainImpactExplanationStore{
		row: exactManifestAndImageExplanationRow(),
	}
	handler := &SupplyChainHandler{
		Content:            repositorySelectorReadModelContentStore(),
		ImpactExplanations: explanations,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	tenantA := AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://example/api"},
		AllowedScopeIDs:      []string{"git-repository-scope:example/api"},
	}
	wantRepos := []string{"repo://example/api"}
	wantScopes := []string{"git-repository-scope:example/api"}

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?repository_id=payments-api&advisory_id=GHSA-test-1",
		nil,
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantA))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("tenant-a status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := explanations.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("tenant-a RepositoryID = %q, want %q", got, want)
	}
	if got := explanations.lastFilter.AllowedRepositoryIDs; !equalPacketStringSlices(got, wantRepos) {
		t.Fatalf("tenant-a AllowedRepositoryIDs = %#v, want %#v", got, wantRepos)
	}
	if got := explanations.lastFilter.AllowedScopeIDs; !equalPacketStringSlices(got, wantScopes) {
		t.Fatalf("tenant-a AllowedScopeIDs = %#v, want %#v", got, wantScopes)
	}
	if strings.Contains(rec.Body.String(), "git-repository-scope:example/api") {
		t.Fatalf("response echoed the grant list itself: %s", rec.Body.String())
	}

	// Tenant B's disjoint grant never resolves tenant A's repository selector
	// (repositorySelectorExactForAccess denies it before the explanation
	// store read), matching
	// TestSupplyChainImpactScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead.
	// This inline repeat keeps the granted/denied pair together for the W5
	// two-tenant proof instead of splitting it across files.
	tenantB := AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-b",
		WorkspaceID:          "workspace-b",
		AllowedRepositoryIDs: []string{"repo://example/other"},
	}
	explanations.lastFilter = SupplyChainImpactExplanationFilter{}
	reqB := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?repository_id=payments-api&advisory_id=GHSA-test-1",
		nil,
	)
	reqB = reqB.WithContext(ContextWithAuthContext(reqB.Context(), tenantB))
	recB := httptest.NewRecorder()
	mux.ServeHTTP(recB, reqB)
	if got, want := recB.Code, http.StatusNotFound; got != want {
		t.Fatalf("tenant-b status = %d, want %d; body = %s", got, want, recB.Body.String())
	}
	if explanations.lastFilter.RepositoryID != "" {
		t.Fatalf("tenant-b request reached the explanation store: %#v", explanations.lastFilter)
	}
	if strings.Contains(recB.Body.String(), "repo://example/api") {
		t.Fatalf("tenant-b out-of-grant response leaked repository id: %s", recB.Body.String())
	}
}

func assertEmptyImpactExplanationResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode explain response: %v; body = %s", err, string(body))
	}
	if resp.Outcome != "no_finding" {
		t.Fatalf("empty scoped explanation Outcome = %q, want %q", resp.Outcome, "no_finding")
	}
	if resp.Finding != nil {
		t.Fatalf("empty scoped explanation Finding = %#v, want nil", resp.Finding)
	}
	if resp.Readiness.State != ReadinessStateReadinessUnavailable {
		t.Fatalf("empty scoped explanation readiness state = %q, want %q", resp.Readiness.State, ReadinessStateReadinessUnavailable)
	}
	if resp.Advisory.CVEID != "" || resp.Advisory.AdvisoryID != "" || resp.Component.PackageID != "" {
		t.Fatalf("empty scoped explanation echoed requested anchors: %#v", resp)
	}
}
