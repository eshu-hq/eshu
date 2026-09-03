// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	entityresolutiontools "github.com/eshu-hq/eshu/go/internal/mcp/entityresolution"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// entityResolutionRouteTools maps every tool the child package owns to the
// method and path it must select, pinned literally so this file stays
// independent of the child's own table. get_entity_context's path is pinned
// with a blank entity id; the populated escaped shape is pinned below.
var entityResolutionRouteTools = map[string]struct {
	method string
	path   string
}{
	"resolve_entity":     {method: "POST", path: "/api/v0/entities/resolve"},
	"get_entity_context": {method: "GET", path: "/api/v0/entities//context"},
	"get_entity_content": {method: "POST", path: "/api/v0/content/entities/read"},
}

func TestResolveRouteUsesExactEntityResolutionChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"query":       "svc-api",
			"name":        "exactName",
			"type":        "function",
			"types":       []any{"workload"},
			"repo_id":     "repo-1",
			"limit":       float64(5),
			"entity_id":   "content-entity:abc/def path",
			"environment": "prod",
		}},
		{name: "deprecated aliases only", args: map[string]any{
			"query": "svc-api",
			"types": []any{"workload", "function"},
		}},
		{name: "malformed", args: map[string]any{
			"query":       42,
			"name":        nil,
			"type":        7,
			"types":       []any{42},
			"repo_id":     42,
			"limit":       "17",
			"entity_id":   9,
			"environment": true,
		}},
	}

	for tool := range entityResolutionRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := entityresolutiontools.Route(tool, routecontract.Arguments(tt.args))
			if !handled {
				t.Fatalf("child Route(%s) handled = false, want true", tool)
			}
			want := &route{
				method: request.Method,
				path:   request.Path,
				body:   request.Body,
				query:  request.Query,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolveRoute(%s, %s) = %#v, want child request %#v", tool, tt.name, got, want)
			}
		}
	}
}

// TestEntityResolutionDispatchKeepsEveryWireShape proves the fields survive
// the adapter boundary on every route, against literal expectations that are
// deliberately independent of the child selector: the parity test above
// builds both of its sides from that selector, so it cannot notice a key the
// child itself dropped, misspelled, or stopped conditioning.
func TestEntityResolutionDispatchKeepsEveryWireShape(t *testing.T) {
	t.Parallel()

	resolve, err := resolveRoute("resolve_entity", map[string]any{
		"name":    "exactName",
		"type":    "function",
		"repo_id": "repo-1",
		"limit":   float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute(resolve_entity) error = %v, want nil", err)
	}
	if resolve.method != "POST" || resolve.path != "/api/v0/entities/resolve" {
		t.Errorf("resolve_entity route = %s %s, want POST /api/v0/entities/resolve", resolve.method, resolve.path)
	}
	if resolve.query != nil {
		t.Errorf("resolve_entity query = %#v, want nil", resolve.query)
	}
	wantResolveBody := map[string]any{"name": "exactName", "type": "function", "repo_id": "repo-1", "limit": 5}
	if !reflect.DeepEqual(resolve.body, wantResolveBody) {
		t.Errorf("resolve_entity body = %#v, want %#v", resolve.body, wantResolveBody)
	}

	// The bare call keeps every conditional key absent and the handler-matching
	// limit default of 10 — the handler substitutes 10 for a nonpositive limit
	// and caps above 100 at 100, so no limit value can 400, while a missing
	// name rejects with HTTP 400 at the handler.
	bare, err := resolveRoute("resolve_entity", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(resolve_entity bare) error = %v, want nil", err)
	}
	if want := map[string]any{"limit": 10}; !reflect.DeepEqual(bare.body, want) {
		t.Errorf("resolve_entity bare body = %#v, want %#v", bare.body, want)
	}

	// The advertised query argument still maps onto the wire name key, and the
	// deprecated types alias still fills a blank type from its first element.
	aliased, err := resolveRoute("resolve_entity", map[string]any{
		"query": "svc-api",
		"types": []any{"workload"},
	})
	if err != nil {
		t.Fatalf("resolveRoute(resolve_entity aliased) error = %v, want nil", err)
	}
	wantAliased := map[string]any{"name": "svc-api", "type": "workload", "limit": 10}
	if !reflect.DeepEqual(aliased.body, wantAliased) {
		t.Errorf("resolve_entity aliased body = %#v, want %#v", aliased.body, wantAliased)
	}

	// get_entity_context path-escapes the entity id and forwards environment
	// only when non-empty; the bare shape keeps the always-non-nil empty query
	// map the root arm built.
	context, err := resolveRoute("get_entity_context", map[string]any{
		"entity_id":   "content-entity:abc/def path",
		"environment": "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute(get_entity_context) error = %v, want nil", err)
	}
	if want := "/api/v0/entities/content-entity:abc%2Fdef%20path/context"; context.path != want {
		t.Errorf("get_entity_context path = %q, want %q", context.path, want)
	}
	if context.method != "GET" || context.body != nil {
		t.Errorf("get_entity_context method/body = %s %#v, want GET with nil body", context.method, context.body)
	}
	if want := map[string]string{"environment": "prod"}; !reflect.DeepEqual(context.query, want) {
		t.Errorf("get_entity_context query = %#v, want %#v", context.query, want)
	}
	bareContext, err := resolveRoute("get_entity_context", map[string]any{"entity_id": "e1", "environment": ""})
	if err != nil {
		t.Fatalf("resolveRoute(get_entity_context bare) error = %v, want nil", err)
	}
	if bareContext.query == nil || len(bareContext.query) != 0 {
		t.Errorf("get_entity_context blank environment query = %#v, want a non-nil empty map", bareContext.query)
	}

	// get_entity_content always sends entity_id, as an explicit empty string
	// when absent, and nothing else.
	content, err := resolveRoute("get_entity_content", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(get_entity_content) error = %v, want nil", err)
	}
	if want := map[string]any{"entity_id": ""}; !reflect.DeepEqual(content.body, want) {
		t.Errorf("get_entity_content bare body = %#v, want %#v", content.body, want)
	}
	if content.query != nil {
		t.Errorf("get_entity_content query = %#v, want nil", content.query)
	}
}

// TestResolveRouteStillOwnsItsArmsAfterEntityResolution proves the delegation
// added ahead of the switch claims only this family and leaves every
// neighbouring entity, workload, content, and code arm answered as before —
// search_entity_content above all, which shares the entity spelling but is
// routed by the content child on the shared contentSearchBody builder.
func TestResolveRouteStillOwnsItsArmsAfterEntityResolution(t *testing.T) {
	t.Parallel()

	// The two service arms reject empty selectors by design, so they carry
	// the minimal selector their root builders require; every other arm
	// resolves on empty arguments as before.
	neighbourArgs := map[string]map[string]any{
		"get_service_context": {"workload_id": "workload:checkout"},
		"get_service_story":   {"service_name": "checkout"},
	}
	for _, tool := range []string{
		"search_entity_content",
		"search_file_content",
		"get_file_content",
		"get_file_lines",
		"get_workload_context",
		"get_workload_story",
		"get_service_context",
		"get_service_story",
		"investigate_service",
		"get_incident_context",
		"list_work_item_evidence",
		"build_evidence_citation_packet",
		"find_code",
		"find_symbol",
	} {
		args := neighbourArgs[tool]
		if args == nil {
			args = map[string]any{}
		}
		if _, handled := entityResolutionRoute(tool, args); handled {
			t.Errorf("entityResolutionRoute(%s) handled = true, want false", tool)
		}
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Errorf("resolveRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if got == nil {
			t.Errorf("resolveRoute(%s) = nil, want a route", tool)
		}
	}

	// search_entity_content still resolves through the content child's
	// shared contentSearchBody builder, pinned here so the neighbouring arm
	// cannot silently ride along with a later entity-resolution change.
	staying, err := resolveRoute("search_entity_content", map[string]any{
		"pattern":  "needle",
		"repo_ids": []any{"r1"},
	})
	if err != nil {
		t.Fatalf("resolveRoute(search_entity_content) error = %v, want nil", err)
	}
	if want := "/api/v0/content/entities/search"; staying.path != want {
		t.Errorf("search_entity_content path = %q, want %q", staying.path, want)
	}
	wantStaying := map[string]any{"query": "needle", "repo_id": "r1", "limit": 10, "offset": 0}
	if !reflect.DeepEqual(staying.body, wantStaying) {
		t.Errorf("search_entity_content body = %#v, want %#v", staying.body, wantStaying)
	}

	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestEntityResolutionRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the three owned names are claimed, and
// near-miss names are not.
func TestEntityResolutionRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for tool := range entityResolutionRouteTools {
		if _, handled := entityResolutionRoute(tool, map[string]any{}); !handled {
			t.Errorf("entityResolutionRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
		"search_entity_content",
		"resolve_entities",
		"resolve_entity_extra",
		"entity_resolve",
		"get_entity_context_extra",
		"get_entity",
		"get_entity_contents",
		"get_workload_context",
		"RESOLVE_ENTITY",
	} {
		if _, handled := entityResolutionRoute(tool, map[string]any{}); handled {
			t.Errorf("entityResolutionRoute(%q) handled = true, want false", tool)
		}
	}
}
