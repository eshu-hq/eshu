// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// changedSinceTwoTenantRequest builds one GET
// /api/v0/freshness/changed-since request against
// querytestutil.ChangedSinceTwoTenantPriorGeneration, carrying auth in its
// context.
func changedSinceTwoTenantRequest(repository string, auth queryauth.AuthContext) *http.Request {
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/changed-since?repository="+repository+
			"&since_generation_id="+querytestutil.ChangedSinceTwoTenantPriorGeneration,
		nil,
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	return req.WithContext(queryauth.ContextWithAuthContext(req.Context(), auth))
}

func serveChangedSinceTwoTenant(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	rec, _ := serveChangedSinceTwoTenantWithReader(t, req)
	return rec
}

// serveChangedSinceTwoTenantWithReader also hands back the fake, so a case can
// assert on the filter the handler bound and not only on the response.
func serveChangedSinceTwoTenantWithReader(
	t *testing.T, req *http.Request,
) (*httptest.ResponseRecorder, *querytestutil.GrantMirroringChangedSince) {
	t.Helper()

	reader := &querytestutil.GrantMirroringChangedSince{Scopes: querytestutil.TwoTenantChangedSinceScopes()}
	handler := &Handler{
		ChangedSince: reader,
		Profile:      querycontract.ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec, reader
}

// TestChangedSinceTwoTenantGrantBoundary is the proof #5167 requires before
// GET /api/v0/freshness/changed-since may leave the pending row-filtering
// ledger and join the scoped-token allowlist: one scoped caller must get its
// own delta, must not get another tenant's, and must not be able to learn that
// the other tenant's scope exists at all. A scoped caller carrying no grant at
// all must read nothing. The shared-key operator view must stay whole.
func TestChangedSinceTwoTenantGrantBoundary(t *testing.T) {
	t.Parallel()

	t.Run("in grant returns the delta", func(t *testing.T) {
		t.Parallel()

		rec := serveChangedSinceTwoTenant(t, changedSinceTwoTenantRequest("repo-a", querytestutil.ScopedChangedSinceTenantA()))

		// Mutation-sensitive: drop filter.AllowedRepositoryIDs in
		// listChangedSince and the mirrored predicate admits nothing for a
		// Scoped filter, so the caller's OWN repository 404s. This assertion is
		// what separates "the grant is bound" from "the grant is bound too
		// tightly", which a deny-only test cannot see.
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; a granted repository must still resolve; body = %s",
				rec.Code, http.StatusOK, rec.Body.String())
		}
		data, _ := querytestutil.DecodeChangedSinceEnvelope(t, rec)
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

		rec := serveChangedSinceTwoTenant(t, changedSinceTwoTenantRequest("repo-b", querytestutil.ScopedChangedSinceTenantA()))

		// Mutation-sensitive: this is the cross-tenant read itself. Remove
		// filter.Scoped (or the SQL grant arm) and the other tenant's scope
		// resolves, so this becomes 200 with tenant B's delta.
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; an ungranted repository must not resolve; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		_, errEnvelope := querytestutil.DecodeChangedSinceEnvelope(t, rec)
		// Mutation-sensitive: a distinct code (403, or a "not authorized"
		// message) would turn the route into an existence oracle -- a caller
		// could enumerate which repositories exist in other tenants by the
		// shape of the refusal. It must be the ordinary scope-not-found.
		if got, want := errEnvelope["code"], string(querycontract.ErrorCodeScopeNotFound); got != want {
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
		absent := serveChangedSinceTwoTenant(t, changedSinceTwoTenantRequest("repo-absent", querytestutil.ScopedChangedSinceTenantA()))
		ungrantedShape := strings.ReplaceAll(rec.Body.String(), "repo-b", "SELECTOR")
		absentShape := strings.ReplaceAll(absent.Body.String(), "repo-absent", "SELECTOR")
		if absent.Code != rec.Code || absentShape != ungrantedShape {
			t.Fatalf("an ungranted repository is distinguishable from an absent one:\n ungranted: %d %s\n absent:    %d %s",
				rec.Code, ungrantedShape, absent.Code, absentShape)
		}
	})

	t.Run("empty grant reads nothing", func(t *testing.T) {
		t.Parallel()

		// The fail-closed half. A scoped caller carrying no repository and no
		// scope grant must still bind, because the shipped predicate is
		// restrictive on empty arrays: `= ANY('{}')` is false, so the caller
		// resolves nothing rather than everything.
		rec, reader := serveChangedSinceTwoTenantWithReader(t, changedSinceTwoTenantRequest(
			"repo-a",
			queryauth.AuthContext{Mode: queryauth.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
		))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; an empty grant must resolve nothing; body = %s",
				rec.Code, http.StatusNotFound, rec.Body.String())
		}
		_, errEnvelope := querytestutil.DecodeChangedSinceEnvelope(t, rec)
		// The refusal must be the same ordinary scope-not-found the ungranted
		// case gets, or an unprovisioned token learns it is unprovisioned by
		// the shape of the error.
		if got, want := errEnvelope["code"], string(querycontract.ErrorCodeScopeNotFound); got != want {
			t.Fatalf("error.code = %v, want %q; the refusal must be the ordinary scope-not-found", got, want)
		}
		for _, leak := range []string{"scope-a", "scope-b", "gen-current-repo-a", "gen-current-repo-b"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Fatalf("not-found body leaks a scope identity %q: %s", leak, rec.Body.String())
			}
		}

		// Mutation-sensitive: the half a status code cannot show. Drop
		// `filter.Scoped = access.Scoped()` and the query runs unbounded, so
		// this caller gets repo-a's delta; leave the assignment but skip the
		// reader entirely and the 404 would still be green while the grant
		// never reached the query at all.
		if !reader.Called {
			t.Fatal("the changed-since reader was never called; the grant is bound in the query, so the query must run")
		}
		if !reader.LastFilter.Scoped {
			t.Fatal("filter.Scoped = false for a scoped caller; an empty grant must bind, not fall through to unbounded")
		}
		if got := len(reader.LastFilter.AllowedRepositoryIDs); got != 0 {
			t.Fatalf("filter.AllowedRepositoryIDs has %d entries, want 0; an empty grant must reach the query empty", got)
		}
		if got := len(reader.LastFilter.AllowedScopeIDs); got != 0 {
			t.Fatalf("filter.AllowedScopeIDs has %d entries, want 0; an empty grant must reach the query empty", got)
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
					tc.repository, queryauth.AuthContext{Mode: queryauth.AuthModeShared},
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
				data, _ := querytestutil.DecodeChangedSinceEnvelope(t, rec)
				if got := data["scope_id"]; got != tc.wantScopeID {
					t.Fatalf("data[scope_id] = %v, want %q", got, tc.wantScopeID)
				}
			})
		}
	})
}
