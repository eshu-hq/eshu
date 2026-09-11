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

func generationsRequest(t *testing.T, generationID string, auth queryauth.AuthContext) *http.Request {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/generations?generation_id="+generationID,
		nil,
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	return req.WithContext(queryauth.ContextWithAuthContext(req.Context(), auth))
}

// TestGenerationLifecycleTwoTenantGrantBoundary is the proof #5167 requires
// before this route may leave the pending row-filtering ledger: the same
// scoped caller must see its own generation and must not be able to learn that
// another tenant's generation exists.
//
// It carries the same four caller shapes its sibling
// TestChangedSinceTwoTenantGrantBoundary carries (#5167 review, P2-1). The
// shared-key rows are the ones a deny-only test cannot see: bind the grant
// unconditionally (`filter.Scoped = true` in generations.go) and the
// two scoped rows still pass while an unscoped operator silently loses every
// generation, because an unscoped caller carries no allowed ids for the
// predicate to match.
func TestGenerationLifecycleTwoTenantGrantBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		generationID string
		auth         queryauth.AuthContext
		wantStatus   int
		// wantScoped is what the handler must have put on the filter, which
		// is the half of the binding a status code cannot show.
		wantScoped bool
		// wantEmptyGrant asserts the filter reached the store carrying no
		// allowed repository and no allowed scope, so the query it stands for
		// can match nothing.
		wantEmptyGrant bool
	}{
		{
			name:         "in grant returns the row",
			generationID: "gen-a",
			auth:         querytestutil.ScopedChangedSinceTenantA(),
			wantStatus:   http.StatusOK,
			wantScoped:   true,
		},
		{
			name:         "out of grant is not found",
			generationID: "gen-b",
			auth:         querytestutil.ScopedChangedSinceTenantA(),
			wantStatus:   http.StatusNotFound,
			wantScoped:   true,
		},
		{
			// The fail-closed half. An empty grant must still bind, because
			// unlike the service-catalog correlation filter this predicate is
			// restrictive on empty arrays: `= ANY('{}')` is false, so the
			// caller resolves nothing rather than everything.
			name:           "empty grant binds and matches nothing",
			generationID:   "gen-a",
			auth:           queryauth.AuthContext{Mode: queryauth.AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
			wantStatus:     http.StatusNotFound,
			wantScoped:     true,
			wantEmptyGrant: true,
		},
		{
			name:         "shared key sees its own tenant",
			generationID: "gen-a",
			auth:         queryauth.AuthContext{Mode: queryauth.AuthModeShared},
			wantStatus:   http.StatusOK,
		},
		{
			name:         "shared key sees the other tenant",
			generationID: "gen-b",
			auth:         queryauth.AuthContext{Mode: queryauth.AuthModeShared},
			wantStatus:   http.StatusOK,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := &querytestutil.GrantMirroringGenerations{Rows: querytestutil.TwoTenantGenerationRows()}
			handler := &Handler{
				Generations: reader,
				Profile:     querycontract.ProfileLocalAuthoritative,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, generationsRequest(t, tc.generationID, tc.auth))

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if !reader.Called {
				t.Fatal("the lifecycle reader was never called; this route binds its grant in the query, so the query must run")
			}
			// Mutation-sensitive: `filter.Scoped = true` for every caller
			// fails the shared-key rows here as well as on their status code,
			// and `filter.Scoped = false` fails the scoped rows, so neither
			// direction can ship green.
			if got := reader.LastFilter.Scoped; got != tc.wantScoped {
				t.Fatalf("filter.Scoped = %t, want %t; the grant binding is what makes this route safe to allowlist",
					got, tc.wantScoped)
			}
			if tc.wantEmptyGrant {
				if got := len(reader.LastFilter.AllowedRepositoryIDs); got != 0 {
					t.Fatalf("filter.AllowedRepositoryIDs has %d entries, want 0; an empty grant must reach the query empty", got)
				}
				if got := len(reader.LastFilter.AllowedScopeIDs); got != 0 {
					t.Fatalf("filter.AllowedScopeIDs has %d entries, want 0; an empty grant must reach the query empty", got)
				}
			}
			// The refusal must be shape-identical to a missing generation: a
			// distinct code here would turn the route into an existence oracle
			// for another tenant's generation ids.
			if tc.wantStatus == http.StatusNotFound {
				body := w.Body.String()
				if body == "" {
					t.Fatalf("expected a not-found contract error body, got an empty response")
				}
				for _, leak := range []string{"scope-b", "repo-b"} {
					if strings.Contains(body, leak) {
						t.Fatalf("not-found body leaks the other tenant's identity %q: %s", leak, body)
					}
				}
			}
		})
	}
}
