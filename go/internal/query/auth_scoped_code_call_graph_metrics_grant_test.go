// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// #5167 code-family batch 1, step 4: POST /api/v0/code/call-graph/metrics.
//
// This route is the one in the set that was not exploitable: repo_id is
// mandatory (callGraphMetricsRequest.validate) and the selector resolves it
// through the caller's grant, so a scoped caller could never reach an
// ungranted repository. What it lacked was the grant in the query text itself,
// which is what the allowlist asks for -- so the predicate here is
// defense-in-depth against a future caller that reaches the read without the
// selector, not a fix for a live leak.
//
// Because $repo_id is already grant-resolved, the added predicate is
// provably row-set-neutral: source.repo_id = $repo_id and $repo_id is in the
// grant, so source.repo_id IN $allowed_repository_ids cannot exclude a row the
// anchor admitted. TestCallGraphMetricsUnscopedCypherIsUnchanged pins the other
// half -- an unscoped caller's query text is byte-identical to before, which is
// what keeps the queryplan manifest's pinned cypher_sha256 and its accepted
// plan claim valid.

func captureCallGraphMetricsCypher(t *testing.T, auth *AuthContext, body map[string]any) (string, map[string]any, int) {
	t.Helper()

	var (
		captured string
		params   map[string]any
	)
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, gotParams map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "[call:CALLS]->") {
					captured = cypher
					params = gotParams
				}
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newCodeGrantRouteRequest(t, "/api/v0/code/call-graph/metrics", body, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return captured, params, rec.Code
}

func callGraphMetricsGrantBody() map[string]any {
	return map[string]any{"repo_id": codeGrantGrantedRepo, "metric_type": "hub_functions"}
}

func TestCallGraphMetricsBindsTheGrantOnBothCallEndpoints(t *testing.T) {
	t.Parallel()

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	captured, params, status := captureCallGraphMetricsCypher(t, &auth, callGraphMetricsGrantBody())

	if got, want := status, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if captured == "" {
		t.Fatal("no call-graph Cypher was captured; the edge scan did not run")
	}
	for _, alias := range []string{"source", "target"} {
		want := "(" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".repo_id IN $allowed_scope_ids)"
		if !strings.Contains(captured, want) {
			t.Fatalf("call-graph Cypher is missing %q on the %s endpoint:\n%s", want, alias, captured)
		}
	}
	if got, ok := params["allowed_repository_ids"].([]string); !ok || !slices.Equal(got, []string{codeGrantGrantedRepo}) {
		t.Fatalf("params[allowed_repository_ids] = %#v, want [%q]; the predicate references a parameter that is never bound", params["allowed_repository_ids"], codeGrantGrantedRepo)
	}
	if _, ok := params["allowed_scope_ids"]; !ok {
		t.Fatalf("params = %#v, want an allowed_scope_ids binding for the predicate's second disjunct", params)
	}
}

// TestCallGraphMetricsEmptyGrantSkipsTheEdgeScan covers both refusals, because
// they are independent. Over HTTP the mandatory repo_id is refused by the
// selector before the handler body runs; that is today's protection and the
// first sub-test pins it. The second calls callGraphMetricsData directly with
// an empty-grant context -- the selector bypassed -- so the access.Empty()
// short-circuit inside the read is exercised on its own and fails if it is
// removed.
func TestCallGraphMetricsEmptyGrantSkipsTheEdgeScan(t *testing.T) {
	t.Parallel()

	t.Run("route", func(t *testing.T) {
		t.Parallel()
		auth := codeGrantScopedAuthContext(nil)
		captured, _, _ := captureCallGraphMetricsCypher(t, &auth, callGraphMetricsGrantBody())
		if captured != "" {
			t.Fatalf("an empty scoped grant reached the edge scan; want no graph read at all:\n%s", captured)
		}
	})

	t.Run("read", func(t *testing.T) {
		t.Parallel()
		queried := false
		handler := &CodeHandler{
			Profile: ProfileLocalAuthoritative,
			Neo4j: fakeGraphReader{
				run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
					queried = true
					return nil, nil
				},
			},
		}
		ctx := ContextWithAuthContext(context.Background(), codeGrantScopedAuthContext(nil))
		data, err := handler.callGraphMetricsData(ctx, callGraphMetricsRequest{
			RepoID:     codeGrantGrantedRepo,
			MetricType: "hub_functions",
		})
		if err != nil {
			t.Fatalf("callGraphMetricsData() error = %v, want nil", err)
		}
		if queried {
			t.Fatal("queried = true, want false -- an empty scoped grant must skip the edge scan, not scan then filter to empty")
		}
		if got := IntVal(data, "count"); got != 0 {
			t.Fatalf("count = %d, want 0 for an empty-grant caller", got)
		}
	})
}

// TestCallGraphMetricsUnscopedCypherIsUnchanged is the counterweight to the
// grant test: the shared-key query text must stay exactly what the queryplan
// manifest pinned (QP-CALL-GRAPH-HUBS / QP-CALL-GRAPH-RECURSIVE,
// internal/queryplan/testdata/handler-hot-cypher.yaml), so the accepted plan
// evidence for this hot read still describes the query production emits.
func TestCallGraphMetricsUnscopedCypherIsUnchanged(t *testing.T) {
	t.Parallel()

	captured, params, status := captureCallGraphMetricsCypher(t, nil, callGraphMetricsGrantBody())
	if got, want := status, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if strings.Contains(captured, "allowed_repository_ids") {
		t.Fatalf("unscoped call-graph Cypher gained a grant predicate:\n%s", captured)
	}
	if strings.Contains(captured, "WHERE") {
		t.Fatalf("unscoped call-graph Cypher gained a WHERE clause:\n%s", captured)
	}
	if _, ok := params["allowed_repository_ids"]; ok {
		t.Fatalf("unscoped params = %#v, want no grant arrays", params)
	}
}

// TestGraphSummaryHotEntitiesRunTheGrantBoundEdgePass covers the second caller
// of callGraphMetricsEdgesCypher. POST /api/v0/ecosystem/graph-summary reuses
// the shared edge pass, so making that builder grant-aware changes the text
// that route emits for a scoped caller too. The route 404s an out-of-grant
// repo_id before the read, which makes the predicate row-set-neutral there, but
// nothing pinned the text it actually sends -- and the queryplan manifest pins
// only the unscoped form.
func TestGraphSummaryHotEntitiesRunTheGrantBoundEdgePass(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		auth  *AuthContext
		grant bool
	}{
		{name: "scoped", auth: ptrToCodeGrantAuthContext(codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})), grant: true},
		{name: "shared_key", auth: nil, grant: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var edgePass string
			handler := &InfraHandler{
				Profile: ProfileProduction,
				Neo4j: fakeGraphReader{
					run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
						if strings.Contains(cypher, "[call:CALLS]->") {
							edgePass = cypher
						}
						return nil, nil
					},
				},
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			body := map[string]any{"repo_id": codeGrantGrantedRepo, "limit": 5}
			req := newCodeGrantRouteRequest(t, "/api/v0/ecosystem/graph-summary", body, tc.auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if edgePass == "" {
				t.Fatal("no edge pass was captured; the hot-entity read did not run")
			}
			for _, alias := range []string{"source", "target"} {
				want := "(" + alias + ".repo_id IN $allowed_repository_ids OR " + alias + ".repo_id IN $allowed_scope_ids)"
				if got := strings.Contains(edgePass, want); got != tc.grant {
					t.Fatalf("edge pass contains %q = %t, want %t:\n%s", want, got, tc.grant, edgePass)
				}
			}
		})
	}
}

func ptrToCodeGrantAuthContext(auth AuthContext) *AuthContext { return &auth }
