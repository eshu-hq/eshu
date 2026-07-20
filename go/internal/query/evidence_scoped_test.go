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

// This file holds the #5167 F-6 W6 access-scoping tests for
// GET /api/v0/evidence/relationships/{resolved_id} (evidence.go), split out of
// evidence_test.go to keep that file under the repository's 500-line cap.

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

// relationshipEvidenceGlobalTargetFixture builds a code-level IMPORTS
// relationship (a targetAttributable:false verb): source File in repo-app,
// target a shared Module with no repo_id. This is the class of evidence a
// scoped caller fully owns via the source but whose target carries no tenant
// attribution to protect.
func relationshipEvidenceGlobalTargetFixture() fakePortContentStore {
	return fakePortContentStore{
		relationshipEvidence: relationshipEvidenceReadModel{
			Available: true,
			Row: map[string]any{
				"lookup_basis":      "resolved_id",
				"resolved_id":       "resolved-import-1",
				"generation_id":     "gen-1",
				"relationship_type": "IMPORTS",
				"confidence":        0.9,
				"evidence_count":    1,
				"source": map[string]any{
					"repo_id":   "repo-app",
					"repo_name": "checkout-app",
				},
				"target": map[string]any{
					"repo_id":   "", // shared Module: no tenant attribution
					"repo_name": "lodash",
				},
			},
		},
	}
}

// TestEvidenceHandlerScopedTokenSourceOwnerReachesGlobalTargetEvidence is the
// #5167 P2#2 failing-then-green fix (review of 7b509786e): for a
// targetAttributable:false verb (IMPORTS/RUNS_ON/QUERIES_TABLE/
// INVOKES_CLOUD_ACTION) whose target is a shared/global entity with no
// repo_id, a scoped caller who owns the SOURCE must retrieve the evidence
// (previously a spurious 404 because access.allowsRepositoryID("") is always
// false). This mirrors the relationships/edges targetAttributable model:
// bind the target grant only when the target carries a tenant attribution.
func TestEvidenceHandlerScopedTokenSourceOwnerReachesGlobalTargetEvidence(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		Content: relationshipEvidenceGlobalTargetFixture(),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-import-1", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-app"}, // owns source only; target is global
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
	if got, want := resp["relationship_type"], "IMPORTS"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v (source-owner must see the row)", got, want)
	}
}

// TestEvidenceHandlerScopedTokenNonSourceOwnerDeniedGlobalTargetEvidence is
// the paired negative for the P2#2 fix: relaxing the target grant for a
// global target must NOT relax the source grant. A caller owning NEITHER
// endpoint still gets 404 (the source is always tenant-attributable).
func TestEvidenceHandlerScopedTokenNonSourceOwnerDeniedGlobalTargetEvidence(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{
		Content: relationshipEvidenceGlobalTargetFixture(),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-import-1", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-b",
		AllowedRepositoryIDs: []string{"repo-other"}, // owns neither endpoint
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "checkout-app") || strings.Contains(rec.Body.String(), "repo-app") {
		t.Fatalf("non-source-owner response leaked source repo identity: %s", rec.Body.String())
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
			name: "scoped caller with empty source repo_id fails closed",
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
		{
			// targetAttributable:false verb (IMPORTS) with a global target
			// (empty repo_id): source-owner alone is authorized (#5167 P2#2).
			name: "targetAttributable-false verb with global target: source grant suffices",
			row: map[string]any{
				"relationship_type": "IMPORTS",
				"source":            map[string]any{"repo_id": "repo-a"},
				"target":            map[string]any{"repo_id": ""},
			},
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-a"},
				allowed:              map[string]struct{}{"repo-a": {}},
			},
			want: true,
		},
		{
			// Same verb, but the caller does not own the source: still denied.
			name: "targetAttributable-false verb: non-source-owner denied",
			row: map[string]any{
				"relationship_type": "IMPORTS",
				"source":            map[string]any{"repo_id": "repo-a"},
				"target":            map[string]any{"repo_id": ""},
			},
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-other"},
				allowed:              map[string]struct{}{"repo-other": {}},
			},
			want: false,
		},
		{
			// targetAttributable:true verb (CALLS) to a target in repo-b: a
			// source-only grant is still insufficient (the target repo is a
			// real tenant that must be protected).
			name: "targetAttributable-true verb still requires target grant",
			row: map[string]any{
				"relationship_type": "CALLS",
				"source":            map[string]any{"repo_id": "repo-a"},
				"target":            map[string]any{"repo_id": "repo-b"},
			},
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-a"},
				allowed:              map[string]struct{}{"repo-a": {}},
			},
			want: false,
		},
		{
			// Unknown verb (not in the 16-verb catalog) with a non-empty
			// target repo_id falls back to fail-closed: require both grants.
			name: "unknown verb with attributable target requires target grant",
			row: map[string]any{
				"relationship_type": "SOME_FUTURE_VERB",
				"source":            map[string]any{"repo_id": "repo-a"},
				"target":            map[string]any{"repo_id": "repo-b"},
			},
			access: repositoryAccessFilter{
				allowedRepositoryIDs: []string{"repo-a"},
				allowed:              map[string]struct{}{"repo-a": {}},
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
