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

func scopedGenerationsRequest(t *testing.T, generationID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/freshness/generations?generation_id="+generationID,
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}))
}

// TestGenerationLifecycleTwoTenantGrantBoundary is the proof #5167 requires
// before this route may leave the pending row-filtering ledger: the same
// scoped caller must see its own generation and must not be able to learn that
// another tenant's generation exists.
func TestGenerationLifecycleTwoTenantGrantBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		generationID string
		wantStatus   int
	}{
		{name: "in grant returns the row", generationID: "gen-a", wantStatus: http.StatusOK},
		{name: "out of grant is not found", generationID: "gen-b", wantStatus: http.StatusNotFound},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := &FreshnessHandler{
				Generations: &grantMirroringGenerations{rows: twoTenantGenerationRows()},
				Profile:     ProfileLocalAuthoritative,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, scopedGenerationsRequest(t, tc.generationID))

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
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
