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
// fixed-text server failure declares 500, and every route declares 503
// through the shared ServiceUnavailable response. Every admin POST route can
// answer 503: the request-checkpoint middleware runs in front of each of them
// (Retry-After on a failed checkpoint step, none when no checkpoint source is
// wired; the GET tuning report is exempt and is not in this list), an unwired
// store, recovery handler, or reindex requester answers a
// configuration 503, and the inspection reads and the repository reindex
// catalog lookup answer a stale PostgreSQL reader. Each description says what
// a 503 with and without Retry-After means, names the checkpoint, and never
// claims the route has no other 503 (#7674 review F2). Only the three bounded
// reads declare 504, for their timeout_ms budget. 499 is never declared: the
// client has already gone.
func TestOpenAPIAdminRoutesDeclareTheirFailureStatuses(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	boundedReadRoutes := map[string]bool{
		"/api/v0/admin/dead-letters/query":                 true,
		"/api/v0/admin/input-invalid-facts/query":          true,
		"/api/v0/admin/changed-since/poisoned-links/query": true,
	}
	// readerCapabilities is the capability-matrix key each reader route's
	// 503 envelope names (#8176).
	readerCapabilities := map[string]string{
		"/api/v0/admin/reindex":                            "operator.repository_catalog.lookup",
		"/api/v0/admin/work-items/query":                   "operator.work_items.list",
		"/api/v0/admin/decisions/query":                    "operator.decisions.list",
		"/api/v0/admin/replay-events/query":                "operator.replay_events.list",
		"/api/v0/admin/dead-letters/query":                 "operator.dead_letters.list",
		"/api/v0/admin/input-invalid-facts/query":          "operator.reducer_input_invalid_facts.list",
		"/api/v0/admin/changed-since/poisoned-links/query": "operator.changed_since_poisoned_links.list",
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
		"/api/v0/admin/repositories/retire",
		"/api/v0/admin/backfill",
		"/api/v0/admin/replay-events/query",
	}
	for _, route := range routes {
		post := adminRoutePost(t, paths, route)
		description, _ := post["description"].(string)
		for _, phrase := range []string{"A 503 with Retry-After means", "a 503 without Retry-After means", "checkpoint"} {
			if !strings.Contains(description, phrase) {
				t.Errorf("%s: description does not contain %q", route, phrase)
			}
		}
		if capability, ok := readerCapabilities[route]; ok && !strings.Contains(description, "names capability "+capability) {
			t.Errorf("%s: description does not name the reader-verdict capability %q", route, capability)
		}
		if strings.Contains(description, "no other 503") {
			t.Errorf("%s: description claims no other 503, but the checkpoint middleware answers 503 on every admin POST route", route)
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
