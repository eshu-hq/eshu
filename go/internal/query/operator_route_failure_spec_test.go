// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// unboundedAdminReadRoutes are the admin routes whose guarded PostgreSQL
// read runs with no route deadline: the work-item, decision (and its
// evidence), and replay-event reads, and the repository reindex catalog
// lookup. Nothing on them produces a deadline verdict, so they answer 503 on
// a reader verdict and 500 otherwise, never 504.
var unboundedAdminReadRoutes = []string{
	"/api/v0/admin/reindex",
	"/api/v0/admin/work-items/query",
	"/api/v0/admin/decisions/query",
	"/api/v0/admin/replay-events/query",
}

// adminSpecPaths returns the paths object of the published spec.
func adminSpecPaths(t *testing.T) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	return testutil.MustMapField(t, spec, "paths")
}

// adminRoutePost returns route's POST operation from paths.
func adminRoutePost(t *testing.T, paths map[string]any, route string) map[string]any {
	t.Helper()
	return testutil.MustMapField(t, testutil.MustMapField(t, paths, route), "post")
}

// TestOpenAPIUnboundedAdminReadsDeclareNo504 pins that the admin routes with
// an unbounded reader step do not promise a 504 (#7674 review F1). Their
// failure helper can map a graph-read deadline sentinel to 504, but nothing
// on these PostgreSQL reads produces one, so a declared 504 would document a
// status the routes never answer.
func TestOpenAPIUnboundedAdminReadsDeclareNo504(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	for _, route := range unboundedAdminReadRoutes {
		responses := testutil.MustMapField(t, adminRoutePost(t, paths, route), "responses")
		if _, ok := responses["504"]; ok {
			t.Errorf("%s: declares 504, but no route deadline or deadline verdict exists on it", route)
		}
		if _, ok := responses["503"]; !ok {
			t.Errorf("%s: no 503 declared for the reachable reader verdict", route)
		}
	}
}

// TestOpenAPIAdminRoutesDeclareTheirFailureStatuses pins the #7674 admin
// failure contract in the published spec: every admin route that answers a
// fixed-text server failure declares 500, every route that can answer 503
// (an unconfigured store or recovery handler, or a stale PostgreSQL reader on
// the inspection reads and the repository reindex catalog lookup) declares
// 503 through the shared ServiceUnavailable response, and says in its own
// description that a 503 without Retry-After is the configuration state
// (#7674 review F2). Only the three bounded reads declare 504, for their
// timeout_ms budget. 499 is never declared: the client has already gone.
func TestOpenAPIAdminRoutesDeclareTheirFailureStatuses(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	boundedReadRoutes := map[string]bool{
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
		post := adminRoutePost(t, paths, route)
		if description, _ := post["description"].(string); !strings.Contains(description, "A 503 without Retry-After means") {
			t.Errorf("%s: description does not say what a 503 without Retry-After means", route)
		}
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
		if _, ok := responses["504"]; ok != boundedReadRoutes[route] {
			t.Errorf("%s: 504 declared = %v, want %v", route, ok, boundedReadRoutes[route])
		}
		if _, ok := responses["499"]; ok {
			t.Errorf("%s: 499 declared; a client cancel is not a documented response", route)
		}
	}
}
