// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestGetEntityContextHydrationGraphReadCarriesQueryName pins the #7118
// review thread on the post-anchor repo-identity hydration read. That read
// runs after the anchor loop, on the request context rather than the loop's
// bounded window, and used to carry no graph query name, so its slow-read and
// deadline telemetry landed under "unnamed" instead of "entity.context". A
// Workload row with no repo_id or repo_name is the shape that makes hydration
// issue its graph read.
func TestGetEntityContextHydrationGraphReadCarriesQueryName(t *testing.T) {
	t.Parallel()

	hydrationReads := 0
	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
			return map[string]any{
				"id":            "workload-a",
				"labels":        []any{"Workload"},
				"name":          "payments-api",
				"relationships": []any{},
			}, nil
		},
		RunFn: func(ctx context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "requested_id") {
				t.Fatalf("unexpected multi-row graph read:\n%s", cypher)
			}
			hydrationReads++
			if got, want := querycontract.GraphQueryNameFromContext(ctx), "entity.context"; got != want {
				t.Errorf("hydration graph query name = %q, want %q", got, want)
			}
			return nil, nil
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/workload-a/context", nil)
	req.SetPathValue("entity_id", "workload-a")
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if hydrationReads != 1 {
		t.Fatalf("hydration graph reads = %d, want 1 (the Workload row has no repo identity)", hydrationReads)
	}
}
