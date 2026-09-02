// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// grantMirroringChangedSince is the #5167 two-tenant fixture for the
// changed-since delta route, in the shape the W4 family proof established and
// freshness_generations_two_tenant_test.go carries for the sibling route: the
// fake does not merely record the filter it was handed, it applies the SAME
// intersection resolveChangedSinceScopeQuery applies, so a handler that stops
// binding the caller grant resolves the other tenant's scope here exactly as it
// would in Postgres, and the assertions below fail.
//
// The mirrored predicate is the shipped one in
// go/internal/storage/postgres/changed_since_sql.go:49-51:
//
//	$3::boolean = false                                             -> unbounded
//	scope.scope_kind = 'repository' AND scope.source_key = ANY($4)  -> repository grant
//	scope.scope_id = ANY($5)                                        -> scope grant
//
// Both scopes are repository-kind and differ only by which tenant owns them,
// which is the harder case: the selector the caller types resolves either one
// equally well, so the grant intersection is the only thing standing between a
// scoped caller and the other tenant's delta.
type grantMirroringChangedSince struct {
	scopes []mirroredChangedSinceScope
}

// mirroredChangedSinceScope is one ingestion_scopes row reduced to the three
// columns the grant predicate reads: the scope id it matches AllowedScopeIDs
// against, the scope_kind that gates the repository arm, and the source_key a
// repository grant authorizes the scope through.
type mirroredChangedSinceScope struct {
	scopeID   string
	scopeKind string
	sourceKey string
}

func (g *grantMirroringChangedSince) ComputeChangedSinceDelta(
	_ context.Context, filter status.ChangedSinceFilter,
) (status.ChangedSinceSummary, error) {
	for _, scope := range g.scopes {
		// The selector arms of the shipped query: ($1 = '' OR scope_id = $1)
		// AND ($2 = '' OR (scope_kind = 'repository' AND source_key = $2)).
		if filter.ScopeID != "" && filter.ScopeID != scope.scopeID {
			continue
		}
		if filter.Repository != "" &&
			(scope.scopeKind != "repository" || scope.sourceKey != filter.Repository) {
			continue
		}
		if filter.Scoped && !mirroredChangedSinceGrantAdmits(filter, scope) {
			continue
		}
		return status.ChangedSinceSummary{
			ScopeID:                   scope.scopeID,
			ScopeKind:                 scope.scopeKind,
			Repository:                scope.sourceKey,
			SinceGenerationID:         changedSinceTwoTenantPriorGeneration,
			CurrentActiveGenerationID: "gen-current-" + scope.sourceKey,
			SampleLimit:               filter.SampleLimit,
			Categories: []status.ChangedSinceCategoryDelta{{
				Category: status.ChangedSinceCategoryFiles,
				Counts:   status.ChangedSinceCounts{Added: 1},
			}},
		}, nil
	}
	// No row: the ungranted scope is indistinguishable from a missing one,
	// which is the whole point of binding the grant in the WHERE clause
	// instead of comparing strings in the handler.
	return status.ChangedSinceSummary{}, nil
}

// mirroredChangedSinceGrantAdmits is the $3/$4/$5 arm of the shipped
// predicate: a repository-kind scope is admitted by source_key membership in
// the repository grant, and any scope is admitted by scope_id membership in
// the scope grant.
func mirroredChangedSinceGrantAdmits(
	filter status.ChangedSinceFilter, scope mirroredChangedSinceScope,
) bool {
	if scope.scopeKind == "repository" {
		for _, granted := range filter.AllowedRepositoryIDs {
			if granted == scope.sourceKey {
				return true
			}
		}
	}
	for _, granted := range filter.AllowedScopeIDs {
		if granted == scope.scopeID {
			return true
		}
	}
	return false
}

const changedSinceTwoTenantPriorGeneration = "gen-prior"

func twoTenantChangedSinceScopes() []mirroredChangedSinceScope {
	return []mirroredChangedSinceScope{
		{scopeID: "scope-a", scopeKind: "repository", sourceKey: "repo-a"},
		{scopeID: "scope-b", scopeKind: "repository", sourceKey: "repo-b"},
	}
}

func changedSinceTwoTenantRequest(repository string, auth AuthContext) *http.Request {
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/changed-since?repository="+repository+
			"&since_generation_id="+changedSinceTwoTenantPriorGeneration,
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req.WithContext(ContextWithAuthContext(req.Context(), auth))
}

func serveChangedSinceTwoTenant(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	handler := &FreshnessHandler{
		ChangedSince: &grantMirroringChangedSince{scopes: twoTenantChangedSinceScopes()},
		Profile:      ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func scopedChangedSinceTenantA() AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}
}

// decodeChangedSinceEnvelope returns the response envelope's data map and error
// object so an assertion can name the field it depends on rather than matching
// a substring of the whole body.
func decodeChangedSinceEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (map[string]any, map[string]any) {
	t.Helper()

	var envelope struct {
		Data  map[string]any `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v; body = %s", err, rec.Body.String())
	}
	return envelope.Data, envelope.Error
}

// TestChangedSinceTwoTenantGrantBoundary is the proof #5167 requires before
// GET /api/v0/freshness/changed-since may leave the pending row-filtering
// ledger and join the scoped-token allowlist: one scoped caller must get its
// own delta, must not get another tenant's, and must not be able to learn that
// the other tenant's scope exists at all. The shared-key operator view must
// stay whole.
func TestChangedSinceTwoTenantGrantBoundary(t *testing.T) {
	t.Parallel()

	t.Run("in grant returns the delta", func(t *testing.T) {
		t.Parallel()

		rec := serveChangedSinceTwoTenant(t, changedSinceTwoTenantRequest("repo-a", scopedChangedSinceTenantA()))

		// Mutation-sensitive: drop filter.AllowedRepositoryIDs in
		// listChangedSince and the mirrored predicate admits nothing for a
		// Scoped filter, so the caller's OWN repository 404s. This assertion is
		// what separates "the grant is bound" from "the grant is bound too
		// tightly", which a deny-only test cannot see.
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; a granted repository must still resolve; body = %s",
				rec.Code, http.StatusOK, rec.Body.String())
		}
		data, _ := decodeChangedSinceEnvelope(t, rec)
		// Mutation-sensitive: if the handler resolved the scope from the
		// selector instead of from the grant-bound row, a change that widened
		// the predicate would still return 200 here. Pinning the resolved
		// scope_id ties the assertion to the row the query actually admitted.
		if got, want := data["scope_id"], "scope-a"; got != want {
			t.Fatalf("data[scope_id] = %v, want %q; the delta must come from the granted scope's row", got, want)
		}
	})

	t.Run("out of grant is not found", func(t *testing.T) {
		t.Parallel()

		rec := serveChangedSinceTwoTenant(t, changedSinceTwoTenantRequest("repo-b", scopedChangedSinceTenantA()))

		// Mutation-sensitive: this is the cross-tenant read itself. Remove
		// filter.Scoped (or the SQL grant arm) and the other tenant's scope
		// resolves, so this becomes 200 with tenant B's delta.
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; an ungranted repository must not resolve; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		_, errEnvelope := decodeChangedSinceEnvelope(t, rec)
		// Mutation-sensitive: a distinct code (403, or a "not authorized"
		// message) would turn the route into an existence oracle -- a caller
		// could enumerate which repositories exist in other tenants by the
		// shape of the refusal. It must be the ordinary scope-not-found.
		if got, want := errEnvelope["code"], string(ErrorCodeScopeNotFound); got != want {
			t.Fatalf("error.code = %v, want %q; the refusal must be the ordinary scope-not-found", got, want)
		}
		// Mutation-sensitive: the not-found message echoes the selector the
		// caller typed (repo-b), which the caller already knows. It must not
		// carry the internal identity of the row it declined to return.
		for _, leak := range []string{"scope-b", "gen-current-repo-b"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Fatalf("not-found body leaks the other tenant's identity %q: %s", leak, rec.Body.String())
			}
		}

		// Mutation-sensitive: the strongest form of the oracle assertion. The
		// refusal for a repository that EXISTS but is ungranted must be
		// byte-identical, once the caller's own echoed selector is normalized,
		// to the refusal for a repository that does not exist anywhere. Any
		// future divergence -- an added detail field, a different message
		// branch -- reintroduces the oracle and fails here.
		absent := serveChangedSinceTwoTenant(t, changedSinceTwoTenantRequest("repo-absent", scopedChangedSinceTenantA()))
		ungrantedShape := strings.ReplaceAll(rec.Body.String(), "repo-b", "SELECTOR")
		absentShape := strings.ReplaceAll(absent.Body.String(), "repo-absent", "SELECTOR")
		if absent.Code != rec.Code || absentShape != ungrantedShape {
			t.Fatalf("an ungranted repository is distinguishable from an absent one:\n ungranted: %d %s\n absent:    %d %s",
				rec.Code, ungrantedShape, absent.Code, absentShape)
		}
	})

	t.Run("all scope shared key sees both tenants", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct{ repository, wantScopeID string }{
			{repository: "repo-a", wantScopeID: "scope-a"},
			{repository: "repo-b", wantScopeID: "scope-b"},
		} {
			tc := tc
			t.Run(tc.repository, func(t *testing.T) {
				t.Parallel()

				rec := serveChangedSinceTwoTenant(t, changedSinceTwoTenantRequest(
					tc.repository, AuthContext{Mode: AuthModeShared},
				))

				// Mutation-sensitive: bind the grant unconditionally -- set
				// filter.Scoped = true for every caller rather than from
				// access.Scoped() -- and the shared-key operator silently loses
				// every scope, because an unscoped caller carries no allowed
				// ids at all. That failure is invisible to the two scoped
				// cases above.
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d; the shared key must stay unbounded across tenants; body = %s",
						rec.Code, http.StatusOK, rec.Body.String())
				}
				data, _ := decodeChangedSinceEnvelope(t, rec)
				if got := data["scope_id"]; got != tc.wantScopeID {
					t.Fatalf("data[scope_id] = %v, want %q", got, tc.wantScopeID)
				}
			})
		}
	})
}
