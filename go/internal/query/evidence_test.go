// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvidenceHandlerReturnsRelationshipEvidenceByResolvedID(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		Content: fakePortContentStore{
			relationshipEvidence: relationshipEvidenceReadModel{
				Available: true,
				Row: map[string]any{
					"lookup_basis":      "resolved_id",
					"resolved_id":       "resolved-1",
					"generation_id":     "gen-1",
					"relationship_type": "DEPLOYS_FROM",
					"confidence":        0.93,
					"evidence_count":    2,
					"evidence_kinds":    []string{"HELM_VALUES_REFERENCE", "ARGOCD_APPLICATIONSET_GENERATOR"},
					"source": map[string]any{
						"repo_id":   "repo-deploy",
						"repo_name": "platform-deployments",
					},
					"target": map[string]any{
						"repo_id":   "repo-service",
						"repo_name": "checkout-service",
					},
					"evidence_preview": []any{
						map[string]any{
							"kind":       "HELM_VALUES_REFERENCE",
							"confidence": 0.91,
							"details": map[string]any{
								"path":       "charts/checkout/values.yaml",
								"start_line": 12,
							},
						},
					},
				},
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["resolved_id"], "resolved-1"; got != want {
		t.Fatalf("resolved_id = %#v, want %#v", got, want)
	}
	if got, want := resp["lookup_basis"], "resolved_id"; got != want {
		t.Fatalf("lookup_basis = %#v, want %#v", got, want)
	}
	if got, want := resp["relationship_type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	if got, want := resp["confidence_basis"], "evidence_aggregate"; got != want {
		t.Fatalf("confidence_basis = %#v, want %#v", got, want)
	}
	source := resp["source"].(map[string]any)
	if got, want := source["repo_name"], "platform-deployments"; got != want {
		t.Fatalf("source.repo_name = %#v, want %#v", got, want)
	}
	target := resp["target"].(map[string]any)
	if got, want := target["repo_name"], "checkout-service"; got != want {
		t.Fatalf("target.repo_name = %#v, want %#v", got, want)
	}
	if !containsStringAny(resp["evidence_kinds"].([]any), "HELM_VALUES_REFERENCE") {
		t.Fatalf("evidence_kinds = %#v, want HELM_VALUES_REFERENCE", resp["evidence_kinds"])
	}
}

// relationshipEvidenceScopedFixture builds a two-tenant fixture: the resolved
// relationship connects source repo "repo-deploy" (tenant A) to target repo
// "repo-service" (also tenant A in the granted scenario, but tenant B's own
// repo in the out-of-grant scenario below).
func relationshipEvidenceScopedFixture() fakePortContentStore {
	return fakePortContentStore{
		relationshipEvidence: relationshipEvidenceReadModel{
			Available: true,
			Row: map[string]any{
				"lookup_basis":      "resolved_id",
				"resolved_id":       "resolved-1",
				"generation_id":     "gen-1",
				"relationship_type": "DEPLOYS_FROM",
				"confidence":        0.93,
				"evidence_count":    2,
				"source": map[string]any{
					"repo_id":   "repo-deploy",
					"repo_name": "platform-deployments",
				},
				"target": map[string]any{
					"repo_id":   "repo-service",
					"repo_name": "checkout-service",
				},
			},
		},
	}
}

// TestEvidenceHandlerScopedTokenWithBothEndpointsGrantedReturnsRealRowData
// proves the #5167 grant check is additive, not a blanket denial: a scoped
// caller granted BOTH endpoint repos sees the actual resolved row (source and
// target repo names), not just a 200 shape. Deleting
// relationshipEvidenceRowWithinAccess's check (or inverting it) would not
// break this test, but the paired out-of-grant test below would then wrongly
// pass -- the two tests together mutation-cover the predicate.
func TestEvidenceHandlerScopedTokenWithBothEndpointsGrantedReturnsRealRowData(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		Content: relationshipEvidenceScopedFixture(),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-1", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-deploy", "repo-service"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	source := resp["source"].(map[string]any)
	if got, want := source["repo_name"], "platform-deployments"; got != want {
		t.Fatalf("source.repo_name = %#v, want %#v (in-grant caller must see real row data)", got, want)
	}
	target := resp["target"].(map[string]any)
	if got, want := target["repo_name"], "checkout-service"; got != want {
		t.Fatalf("target.repo_name = %#v, want %#v (in-grant caller must see real row data)", got, want)
	}
}

// TestEvidenceHandlerScopedTokenMissingTargetGrantReturnsNotFound proves the
// "both endpoints" contract: a caller granted only the SOURCE repo (not the
// target) must not see the edge, because that would disclose the target
// tenant's repo_id/repo_name to an outsider. This is the mutation-kill test
// for relationshipEvidenceRowWithinAccess -- if the target-endpoint check were
// dropped, this request would incorrectly return 200 with tenant B's repo name.
func TestEvidenceHandlerScopedTokenMissingTargetGrantReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		Content: relationshipEvidenceScopedFixture(),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-1", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-deploy"}, // no grant for repo-service (target)
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "checkout-service") || strings.Contains(rec.Body.String(), "repo-service") {
		t.Fatalf("out-of-grant response leaked target repo identity: %s", rec.Body.String())
	}
}

// TestEvidenceHandlerScopedTokenEmptyGrantReturnsNotFound covers the
// zero-grant case distinctly from the out-of-grant case above.
func TestEvidenceHandlerScopedTokenEmptyGrantReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		Content: relationshipEvidenceScopedFixture(),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-1", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:     AuthModeScoped,
		TenantID: "tenant-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

// TestRelationshipEvidenceRowWithinAccessMutationCoverage unit-tests the
// #5167 predicate directly across the cases a mutation (dropped clause,
// inverted boolean, source/target swap) would flip.
func TestRelationshipEvidenceRowWithinAccessMutationCoverage(t *testing.T) {
	t.Parallel()

	bothGranted := map[string]any{
		"source": map[string]any{"repo_id": "repo-a"},
		"target": map[string]any{"repo_id": "repo-b"},
	}
	for _, tt := range []struct {
		name   string
		row    map[string]any
		access repositoryAccessFilter
		want   bool
	}{
		{
			name:   "unscoped caller always passes",
			row:    bothGranted,
			access: repositoryAccessFilter{allScopes: true},
			want:   true,
		},
		{
			name:   "scoped caller with no grants fails closed",
			row:    bothGranted,
			access: repositoryAccessFilter{},
			want:   false,
		},
		{
			name: "scoped caller with both endpoints granted passes",
			row:  bothGranted,
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-a", "repo-b"},
				allowed:              map[string]struct{}{"repo-a": {}, "repo-b": {}},
			},
			want: true,
		},
		{
			name: "scoped caller missing source grant fails",
			row:  bothGranted,
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-b"},
				allowed:              map[string]struct{}{"repo-b": {}},
			},
			want: false,
		},
		{
			name: "scoped caller missing target grant fails",
			row:  bothGranted,
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-a"},
				allowed:              map[string]struct{}{"repo-a": {}},
			},
			want: false,
		},
		{
			name: "scoped caller with empty endpoint repo_id fails closed",
			row: map[string]any{
				"source": map[string]any{"repo_id": ""},
				"target": map[string]any{"repo_id": "repo-b"},
			},
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-b"},
				allowed:              map[string]struct{}{"repo-b": {}},
			},
			want: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := relationshipEvidenceRowWithinAccess(tt.row, tt.access); got != tt.want {
				t.Fatalf("relationshipEvidenceRowWithinAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvidenceHandlerReturnsNotFoundForMissingRelationshipEvidence(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		Content: fakePortContentStore{},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/missing-resolved-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestContentReaderRelationshipEvidenceByResolvedIDHydratesDetails(t *testing.T) {
	t.Parallel()

	details := []byte(`{
		"evidence_kinds": ["GITHUB_ACTIONS_REUSABLE_WORKFLOW", "HELM_VALUES_REFERENCE"],
		"evidence_preview": [
			{
				"kind": "GITHUB_ACTIONS_REUSABLE_WORKFLOW",
				"confidence": 0.88,
				"details": {
					"path": ".github/workflows/deploy.yaml",
					"matched_value": "shared-deploy.yaml",
					"start_line": 7
				}
			}
		]
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: contentReaderRelationshipEvidenceColumns(),
			rows: [][]driver.Value{
				{
					"resolved-1",
					"gen-1",
					"repo-service",
					"checkout-service",
					"entity-service",
					"repo-platform",
					"platform-infra",
					"entity-platform",
					"DEPENDS_ON",
					0.88,
					int64(2),
					"matched deployment and runtime evidence",
					"relationship_resolver",
					details,
					"repository",
					"run-1",
					"active",
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := reader.relationshipEvidenceByResolvedID(t.Context(), "resolved-1")
	if err != nil {
		t.Fatalf("relationshipEvidenceByResolvedID() error = %v, want nil", err)
	}
	if !got.Available {
		t.Fatal("relationshipEvidenceByResolvedID().Available = false, want true")
	}
	row := got.Row
	if got, want := row["resolved_id"], "resolved-1"; got != want {
		t.Fatalf("resolved_id = %#v, want %#v", got, want)
	}
	if got, want := row["lookup_basis"], "resolved_id"; got != want {
		t.Fatalf("lookup_basis = %#v, want %#v", got, want)
	}
	if got, want := row["relationship_type"], "DEPENDS_ON"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	if got, want := row["confidence_basis"], "evidence_aggregate"; got != want {
		t.Fatalf("confidence_basis = %#v, want %#v", got, want)
	}
	source := row["source"].(map[string]any)
	if got, want := source["repo_name"], "checkout-service"; got != want {
		t.Fatalf("source.repo_name = %#v, want %#v", got, want)
	}
	target := row["target"].(map[string]any)
	if got, want := target["entity_id"], "entity-platform"; got != want {
		t.Fatalf("target.entity_id = %#v, want %#v", got, want)
	}
	if kinds := row["evidence_kinds"].([]string); len(kinds) != 2 || kinds[0] != "GITHUB_ACTIONS_REUSABLE_WORKFLOW" {
		t.Fatalf("evidence_kinds = %#v, want decoded details kinds", kinds)
	}
	if previews := row["evidence_preview"].([]any); len(previews) != 1 {
		t.Fatalf("evidence_preview = %#v, want one preview", previews)
	}
}
