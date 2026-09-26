// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// impactPathRoundTripGraph answers the three #5167 impact path routes for the
// real-middleware round trip: the anchor resolves to a repo-b CloudResource,
// and every traversal would return a repo-b row if it ran.
func impactPathRoundTripGraph(t *testing.T, traversals *int) graph.FakeGraphReaderWithSingle {
	resolve := func(params map[string]any) map[string]any {
		for _, key := range []string{"start_id", "source_id", "target_id"} {
			if _, ok := params[key]; ok {
				return map[string]any{"label": "CloudResource", "id": "cr-b", "uid": "cr-b", "name": "bucket-b", "labels": []any{"CloudResource"}}
			}
		}
		return nil
	}
	run := func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "AS label, n.id AS id"):
			return []map[string]any{resolve(params)}, nil
		case strings.Contains(cypher, "$uids"):
			// The owner statement: repo-b's WorkloadInstance USES it, so the
			// grant (repo-a) does not own it.
			return []map[string]any{{"uid": "cr-b", "repo_id": "repo-b"}}, nil
		default:
			*traversals++
			return []map[string]any{{"repo_id": "repo-b", "repo_name": "svc-b", "depth": int64(1)}}, nil
		}
	}
	return graph.FakeGraphReaderWithSingle{
		RunFn: run,
		RunSingleFn: func(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
			rows, err := run(ctx, cypher, params)
			if err != nil || len(rows) == 0 {
				return nil, err
			}
			return rows[0], nil
		},
	}
}

// TestAuthMiddlewareWithScopedTokensAdmitsImpactPathRoutes is the #5167
// real-middleware round trip for the three promoted impact path routes: a
// scoped bearer reaches the handler (no 403), and the handler's grant check --
// not the middleware -- keeps repo-b's anchor out of the response and runs no
// traversal for it. Before the promotion these routes answered 403.
func TestAuthMiddlewareWithScopedTokensAdmitsImpactPathRoutes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path, body string
		wantStatus int
	}{
		{"/api/v0/impact/trace-resource-to-code", `{"start":"bucket-b"}`, http.StatusOK},
		{"/api/v0/impact/explain-dependency-path", `{"source":"bucket-b","target":"repo-b"}`, http.StatusNotFound},
		{"/api/v0/impact/trace-exposure-path", `{"source_entity_id":"fn-b"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			traversals := 0
			handler := &ImpactHandler{
				Neo4j:   impactPathRoundTripGraph(t, &traversals),
				Content: exposureSourceContentStore{entity: EntityContent{EntityID: "fn-b", RepoID: "repo-b", EntityName: "helperB"}},
				Profile: ProfileLocalAuthoritative,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)
			resolver := &fakeScopedTokenResolver{context: testutil.ScopedTestAuthContext("tenant-a", []string{"repo-a"}), ok: true}
			middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (a scoped bearer must reach the handler); body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			for _, leaked := range []string{"cr-b", "repo-b", "svc-b", "fn-b", "helperB"} {
				if strings.Contains(w.Body.String(), leaked) {
					t.Errorf("response leaks %q: %s", leaked, w.Body.String())
				}
			}
			if traversals != 0 {
				t.Errorf("traversals = %d, want 0 for an ungranted anchor", traversals)
			}
		})
	}
}
