// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// codeownersCrossTenantRepo is the ungranted repository whose ownership and
// manifest owner must never leak to a scoped caller granted only repo-a.
const codeownersCrossTenantRepo = "repo-b"

// codeownersCrossTenantGraph returns repo-b's DECLARES_CODEOWNER row from Run
// and repo-b's CODEOWNERS last-match owner from RunSingle, regardless of which
// repository_id the caller requested -- the handler itself, not the graph
// double, is what must refuse to surface these rows for an ungranted caller.
func codeownersCrossTenantGraph() *recordingCodeownersGraphReader {
	return &recordingCodeownersGraphReader{
		runRows: []map[string]any{
			{"pattern": "*.go", "source_path": "CODEOWNERS", "order_index": int64(0), "owner_ref": "@org/team-b"},
		},
		singleRow: map[string]any{"owner_ref": "@org/team-b"},
	}
}

// codeownersCrossTenantCorrelations returns a manifest owner for repo-b with a
// resolved "exact" outcome, so a leak would surface through effective_owner's
// service-catalog precedence branch too, not only through ownership[].
func codeownersCrossTenantCorrelations() *fakeCodeownersCorrelationStore {
	return &fakeCodeownersCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{RepositoryID: codeownersCrossTenantRepo, OwnerRef: "@org/team-b-manifest", Outcome: "exact"},
		},
	}
}

// TestCodeownersOwnershipScopedCallerCannotReadUngrantedRepository is the
// #5419 Phase 4b cross-tenant leak proof: a scoped caller granted only repo-a
// must never see repo-b's CODEOWNERS ownership rows or effective_owner when it
// requests ?repository_id=repo-b, even though the graph and correlation store
// both hold real repo-b data. Before the fix, listOwnership ran the
// DECLARES_CODEOWNER read and resolveEffectiveRepositoryOwner unconditionally
// for whatever repository_id the caller supplied, so this case failed
// (leaked repo-b's row and manifest owner) until repositoryAccessFilterFromContext
// gated both read paths in listOwnership.
func TestCodeownersOwnershipScopedCallerCannotReadUngrantedRepository(t *testing.T) {
	t.Parallel()

	newReq := func(auth *AuthContext) (*httptest.ResponseRecorder, string) {
		mux := newCodeownersOwnershipMux(codeownersCrossTenantGraph(), codeownersCrossTenantCorrelations())
		req := httptest.NewRequest(http.MethodGet, "/api/v0/codeowners/ownership?repository_id="+codeownersCrossTenantRepo, nil)
		if auth != nil {
			req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w, w.Body.String()
	}

	decode := func(t *testing.T, body string) (ownership []CodeownersOwnershipRow, effectiveOwner EffectiveRepositoryOwner) {
		t.Helper()
		var resp struct {
			Ownership      []CodeownersOwnershipRow `json:"ownership"`
			EffectiveOwner EffectiveRepositoryOwner `json:"effective_owner"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v; body = %s", err, body)
		}
		return resp.Ownership, resp.EffectiveOwner
	}

	t.Run("scoped caller granted only repo-a sees no repo-b data", func(t *testing.T) {
		t.Parallel()

		scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
		w, body := newReq(&scoped)

		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, body)
		}
		ownership, effectiveOwner := decode(t, body)
		if len(ownership) != 0 {
			t.Fatalf("scoped caller granted only repo-a leaked repo-b ownership rows: %+v", ownership)
		}
		if want := (EffectiveRepositoryOwner{}); effectiveOwner != want {
			t.Fatalf("scoped caller granted only repo-a leaked repo-b effective_owner: %+v, want zero value %+v", effectiveOwner, want)
		}
	})

	t.Run("scoped caller granted repo-b sees repo-b data", func(t *testing.T) {
		t.Parallel()

		scoped := scopedTestAuthContext("tenant-b", []string{codeownersCrossTenantRepo})
		w, body := newReq(&scoped)

		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, body)
		}
		ownership, effectiveOwner := decode(t, body)
		if len(ownership) != 1 {
			t.Fatalf("scoped caller granted repo-b did not see repo-b ownership rows: %+v", ownership)
		}
		if want := (EffectiveRepositoryOwner{OwnerRef: "@org/team-b-manifest", Source: EffectiveOwnerSourceServiceCatalog}); effectiveOwner != want {
			t.Fatalf("scoped caller granted repo-b: effective_owner = %+v, want %+v", effectiveOwner, want)
		}
	})

	t.Run("unscoped caller sees repo-b data", func(t *testing.T) {
		t.Parallel()

		w, body := newReq(nil)

		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, body)
		}
		ownership, effectiveOwner := decode(t, body)
		if len(ownership) != 1 {
			t.Fatalf("unscoped caller did not see repo-b ownership rows: %+v", ownership)
		}
		if want := (EffectiveRepositoryOwner{OwnerRef: "@org/team-b-manifest", Source: EffectiveOwnerSourceServiceCatalog}); effectiveOwner != want {
			t.Fatalf("unscoped caller: effective_owner = %+v, want %+v", effectiveOwner, want)
		}
	})
}
