// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #5167 code-family batch 1, step 4: POST /api/v0/code/call-graph/metrics.
//
// This route is the one in the set that was never exploitable, and it is bound
// to the caller's grant without a predicate of its own. repo_id is mandatory
// (callGraphMetricsRequest.validate) and applyRepositorySelectorForCapability
// resolves it through the grant before the read, rejecting an ungranted one
// with 400 -- the same shape the impact family relies on. A grant predicate in
// the edge Cypher on top of that would be redundant by construction (both
// endpoints already match the grant-resolved $repo_id) while creating a second
// hot-path query shape with no NornicDB plan behind it.
//
// So the edge Cypher carries no grant and every caller runs the same text.
// TestCallGraphMetricsCypherIsTheSameForEveryCaller pins that, which is what
// keeps the queryplan manifest's cypher_sha256 and its accepted plan claim
// describing the query every caller actually emits.

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

func TestCallGraphMetricsCypherIsTheSameForEveryCaller(t *testing.T) {
	t.Parallel()

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	scoped, scopedParams, scopedStatus := captureCallGraphMetricsCypher(t, &auth, callGraphMetricsGrantBody())
	shared, sharedParams, sharedStatus := captureCallGraphMetricsCypher(t, nil, callGraphMetricsGrantBody())

	for name, status := range map[string]int{"scoped": scopedStatus, "shared_key": sharedStatus} {
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", name, status, http.StatusOK)
		}
	}
	if scoped == "" {
		t.Fatal("no call-graph Cypher was captured; the edge scan did not run")
	}
	if scoped != shared {
		t.Fatalf("a scoped caller runs a different edge shape than the one the plan fixture pins:\nscoped:\n%s\nshared key:\n%s", scoped, shared)
	}
	if strings.Contains(scoped, "allowed_repository_ids") || strings.Contains(scoped, "WHERE") {
		t.Fatalf("the edge Cypher gained a grant predicate; the route is bound by the mandatory repo_id selector, not by this query:\n%s", scoped)
	}
	for name, params := range map[string]map[string]any{"scoped": scopedParams, "shared_key": sharedParams} {
		if _, ok := params["allowed_repository_ids"]; ok {
			t.Fatalf("%s params = %#v, want no grant arrays", name, params)
		}
	}
}

// TestCallGraphMetricsRejectsAnUngrantedRepository is the binding this route
// actually has. The Cypher carries no grant, so the selector is the whole
// guard: an ungranted repo_id must be refused before the edge scan runs.
func TestCallGraphMetricsRejectsAnUngrantedRepository(t *testing.T) {
	t.Parallel()

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	captured, _, status := captureCallGraphMetricsCypher(t, &auth, map[string]any{
		"repo_id":     codeGrantOtherRepo,
		"metric_type": "hub_functions",
	})
	if got, want := status, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d for a repo_id outside the caller's grant", got, want)
	}
	if captured != "" {
		t.Fatalf("an ungranted repo_id reached the edge scan:\n%s", captured)
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

// TestCallGraphMetricsUnscopedCypherIsUnchanged pins the shared-key query text
// as exactly what the queryplan manifest recorded (QP-CALL-GRAPH-HUBS /
// QP-CALL-GRAPH-RECURSIVE, internal/queryplan/testdata/handler-hot-cypher.yaml),
// so the accepted plan evidence for this hot read still describes the query
// production emits.
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

// TestGraphSummaryHotEntitiesEdgePassIsUnchanged covers the second caller of
// callGraphMetricsEdgesCypher. POST /api/v0/ecosystem/graph-summary reuses the
// shared edge pass, so anything that made that builder grant-aware would change
// the text this route emits for a scoped caller too -- and the queryplan
// manifest pins one text, not two. This asserts the scoped caller's edge pass is
// the shared-key one, and that this route's own grant check, the not-found it
// answers for an out-of-grant repo_id, keeps that repository away from the read.
func TestGraphSummaryHotEntitiesEdgePassIsUnchanged(t *testing.T) {
	t.Parallel()

	captureEdgePass := func(t *testing.T, auth *AuthContext, repoID string) (string, int) {
		t.Helper()

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

		body := map[string]any{"repo_id": repoID, "limit": 5}
		req := newCodeGrantRouteRequest(t, "/api/v0/ecosystem/graph-summary", body, auth)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return edgePass, rec.Code
	}

	scopedAuth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	scoped, scopedStatus := captureEdgePass(t, &scopedAuth, codeGrantGrantedRepo)
	shared, sharedStatus := captureEdgePass(t, nil, codeGrantGrantedRepo)

	for name, status := range map[string]int{"scoped": scopedStatus, "shared_key": sharedStatus} {
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", name, status, http.StatusOK)
		}
	}
	if shared == "" {
		t.Fatal("no edge pass was captured; the hot-entity read did not run")
	}
	if scoped != shared {
		t.Fatalf("the scoped edge pass drifted from the text the plan fixture pins:\nscoped:\n%s\nshared key:\n%s", scoped, shared)
	}

	ungranted, ungrantedStatus := captureEdgePass(t, &scopedAuth, codeGrantOtherRepo)
	if got, want := ungrantedStatus, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d for a repo_id outside the caller's grant", got, want)
	}
	if ungranted != "" {
		t.Fatalf("an out-of-grant repo_id reached the edge pass:\n%s", ungranted)
	}
}
