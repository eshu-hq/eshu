// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPIAdminRoutesDeclareTheirFailureStatuses pins the #7674 admin
// failure contract in the published spec: every admin route that answers a
// fixed-text server failure declares 500, every route that can answer 503
// (an unconfigured store or recovery handler, or a stale PostgreSQL reader on
// the inspection reads and the repository reindex catalog lookup) declares
// 503 through the shared ServiceUnavailable response, which documents
// Retry-After. The routes that read through the reader declare 504: the
// three bounded reads for their route timeout, and every reader route for the
// shared graph-read deadline verdict. 499 is never declared: the client has
// already gone.
func TestOpenAPIAdminRoutesDeclareTheirFailureStatuses(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")

	readerRoutes := map[string]bool{
		"/api/v0/admin/reindex":                            true,
		"/api/v0/admin/work-items/query":                   true,
		"/api/v0/admin/decisions/query":                    true,
		"/api/v0/admin/replay-events/query":                true,
		"/api/v0/admin/dead-letters/query":                 true,
		"/api/v0/admin/input-invalid-facts/query":          true,
		"/api/v0/admin/changed-since/poisoned-links/query": true,
	}
	routes := []string{
		"/api/v0/admin/refinalize",
		"/api/v0/admin/reindex",
		"/api/v0/admin/recover-generations",
		"/api/v0/admin/work-items/query",
		"/api/v0/admin/decisions/query",
		"/api/v0/admin/dead-letters/query",
		"/api/v0/admin/input-invalid-facts/query",
		"/api/v0/admin/changed-since/poisoned-links/query",
		"/api/v0/admin/dead-letter",
		"/api/v0/admin/skip",
		"/api/v0/admin/replay",
		"/api/v0/admin/reopen",
		"/api/v0/admin/backfill",
		"/api/v0/admin/replay-events/query",
	}
	for _, route := range routes {
		post := testutil.MustMapField(t, testutil.MustMapField(t, paths, route), "post")
		responses := testutil.MustMapField(t, post, "responses")
		if _, ok := responses["500"]; !ok {
			t.Errorf("%s: no 500 response declared", route)
		}
		unavailable, ok := responses["503"].(map[string]any)
		if !ok {
			t.Errorf("%s: no 503 response declared", route)
		} else if ref := unavailable["$ref"]; ref != "#/components/responses/ServiceUnavailable" {
			t.Errorf("%s: 503 = %v, want the shared ServiceUnavailable response", route, unavailable)
		}
		if _, ok := responses["504"]; ok != readerRoutes[route] {
			t.Errorf("%s: 504 declared = %v, want %v", route, ok, readerRoutes[route])
		}
		if _, ok := responses["499"]; ok {
			t.Errorf("%s: 499 declared; a client cancel is not a documented response", route)
		}
	}
}
