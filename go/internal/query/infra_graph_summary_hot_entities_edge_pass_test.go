// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestGraphSummaryHotEntitiesEdgePassIsUnchanged moved out of
// codequery/auth_scoped_code_call_graph_metrics_grant_test.go at the #6060
// CodeHandler move: it tests InfraHandler.graphSummaryHotEntities (root,
// infra_graph_summary_packet.go), not CodeHandler, and cannot be called from
// another package.
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

	scopedAuth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
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
