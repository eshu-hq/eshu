// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// grantMirroringGenerations is the #5167 two-tenant fixture for the generation
// lifecycle route, in the shape the W4 family proof established
// (auth_scoped_iac_replatforming_grant_test.go): the fake does not merely
// record the filter, it applies the SAME intersection listGenerationLifecycleQuery
// applies, so a handler that stops binding the grant serves the other tenant's
// row and the assertion below fails.
//
// The mirrored predicate is the one in generation_lifecycle_sql.go:
//
//	$8 = false                                   -> unbounded
//	scope_kind = 'repository' AND source_key = ANY($9)  -> repository grant
//	generation.scope_id = ANY($10)               -> scope grant
//
// Both rows carry the same generation_id shape and differ only by owning scope,
// which is the harder case: a filter that keyed off generation_id alone cannot
// tell them apart, so the grant intersection is the only thing preventing the
// cross-tenant read.
type grantMirroringGenerations struct {
	rows []mirroredGenerationRow
	// lastFilter is what the handler actually asked for. The shared-key and
	// empty-grant cases assert against it because a status code alone cannot
	// tell "the grant was bound and matched nothing" from "the grant was never
	// bound at all" (#5167 review, P2-1).
	lastFilter status.GenerationLifecycleFilter
	called     bool
}

type mirroredGenerationRow struct {
	generationID string
	scopeID      string
	scopeKind    string
	sourceKey    string
}

func (g *grantMirroringGenerations) ListGenerationLifecycle(
	_ context.Context, filter status.GenerationLifecycleFilter,
) (status.GenerationLifecyclePage, error) {
	g.lastFilter = filter
	g.called = true

	var out []status.GenerationLifecycleRecord
	for _, row := range g.rows {
		if filter.GenerationID != "" && filter.GenerationID != row.generationID {
			continue
		}
		if filter.Scoped && !mirroredGrantAdmits(filter, row) {
			continue
		}
		out = append(out, status.GenerationLifecycleRecord{
			ScopeID:      row.scopeID,
			GenerationID: row.generationID,
			ScopeKind:    row.scopeKind,
			Status:       "active",
		})
	}
	return status.GenerationLifecyclePage{Records: out, Limit: filter.Limit}, nil
}

func mirroredGrantAdmits(filter status.GenerationLifecycleFilter, row mirroredGenerationRow) bool {
	if row.scopeKind == "repository" {
		for _, granted := range filter.AllowedRepositoryIDs {
			if granted == row.sourceKey {
				return true
			}
		}
	}
	for _, granted := range filter.AllowedScopeIDs {
		if granted == row.scopeID {
			return true
		}
	}
	return false
}

func twoTenantGenerationRows() []mirroredGenerationRow {
	return []mirroredGenerationRow{
		{generationID: "gen-a", scopeID: "scope-a", scopeKind: "repository", sourceKey: "repo-a"},
		{generationID: "gen-b", scopeID: "scope-b", scopeKind: "repository", sourceKey: "repo-b"},
	}
}

func generationsRequest(t *testing.T, generationID string, auth AuthContext) *http.Request {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/generations?generation_id="+generationID,
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req.WithContext(ContextWithAuthContext(req.Context(), auth))
}

// scopedGenerationsTenantA is the grant-bearing caller: repo-a and scope-a,
// which the fixture's first row carries and its second does not.
func scopedGenerationsTenantA() AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}
}

// TestGenerationLifecycleTwoTenantGrantBoundary is the proof #5167 requires
// before this route may leave the pending row-filtering ledger: the same
// scoped caller must see its own generation and must not be able to learn that
// another tenant's generation exists.
//
// It carries the same four caller shapes its sibling
// TestChangedSinceTwoTenantGrantBoundary carries (#5167 review, P2-1). The
// shared-key rows are the ones a deny-only test cannot see: bind the grant
// unconditionally (`filter.Scoped = true` in freshness_generations.go) and the
// two scoped rows still pass while an unscoped operator silently loses every
// generation, because an unscoped caller carries no allowed ids for the
// predicate to match.
func TestGenerationLifecycleTwoTenantGrantBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		generationID string
		auth         AuthContext
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
			auth:         scopedGenerationsTenantA(),
			wantStatus:   http.StatusOK,
			wantScoped:   true,
		},
		{
			name:         "out of grant is not found",
			generationID: "gen-b",
			auth:         scopedGenerationsTenantA(),
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
			auth:           AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
			wantStatus:     http.StatusNotFound,
			wantScoped:     true,
			wantEmptyGrant: true,
		},
		{
			name:         "shared key sees its own tenant",
			generationID: "gen-a",
			auth:         AuthContext{Mode: AuthModeShared},
			wantStatus:   http.StatusOK,
		},
		{
			name:         "shared key sees the other tenant",
			generationID: "gen-b",
			auth:         AuthContext{Mode: AuthModeShared},
			wantStatus:   http.StatusOK,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := &grantMirroringGenerations{rows: twoTenantGenerationRows()}
			handler := &FreshnessHandler{
				Generations: reader,
				Profile:     ProfileLocalAuthoritative,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, generationsRequest(t, tc.generationID, tc.auth))

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if !reader.called {
				t.Fatal("the lifecycle reader was never called; this route binds its grant in the query, so the query must run")
			}
			// Mutation-sensitive: `filter.Scoped = true` for every caller
			// fails the shared-key rows here as well as on their status code,
			// and `filter.Scoped = false` fails the scoped rows, so neither
			// direction can ship green.
			if got := reader.lastFilter.Scoped; got != tc.wantScoped {
				t.Fatalf("filter.Scoped = %t, want %t; the grant binding is what makes this route safe to allowlist",
					got, tc.wantScoped)
			}
			if tc.wantEmptyGrant {
				if got := len(reader.lastFilter.AllowedRepositoryIDs); got != 0 {
					t.Fatalf("filter.AllowedRepositoryIDs has %d entries, want 0; an empty grant must reach the query empty", got)
				}
				if got := len(reader.lastFilter.AllowedScopeIDs); got != 0 {
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
