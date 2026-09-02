// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entityresolutiontools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyRequests pins the three owned tool names to their methods and
// internal paths, literally, so the ownership test cannot drift with the
// selector's own table. get_entity_context's path is pinned with a blank
// entity id; the escaping test below pins the populated shape.
var familyRequests = map[string]struct {
	method string
	path   string
}{
	"resolve_entity":     {method: "POST", path: "/api/v0/entities/resolve"},
	"get_entity_context": {method: "GET", path: "/api/v0/entities//context"},
	"get_entity_content": {method: "POST", path: "/api/v0/content/entities/read"},
}

func TestRouteOwnsExactlyTheEntityResolutionFamily(t *testing.T) {
	t.Parallel()

	for tool, want := range familyRequests {
		request, handled := Route(tool, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tool)
		}
		if request.Method != want.method {
			t.Errorf("Route(%s) method = %q, want %q", tool, request.Method, want.method)
		}
		if request.Path != want.path {
			t.Errorf("Route(%s) path = %q, want %q", tool, request.Path, want.path)
		}
	}

	for _, tool := range []string{
		"",
		"search_entity_content",
		"search_file_content",
		"resolve_entities",
		"resolve_entity_extra",
		"entity_resolve",
		"get_entity_context_extra",
		"get_entity",
		"get_entity_contents",
		"get_workload_context",
		"get_file_content",
		"RESOLVE_ENTITY",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryEntityResolutionBodyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			tool: "resolve_entity",
			args: routecontract.Arguments{
				"name":    "exactName",
				"type":    "function",
				"repo_id": "repo-1",
				"limit":   float64(5),
			},
			want: map[string]any{
				"name":    "exactName",
				"type":    "function",
				"repo_id": "repo-1",
				"limit":   5,
			},
		},
		{
			tool: "get_entity_content",
			args: routecontract.Arguments{"entity_id": "content-entity:abc"},
			want: map[string]any{"entity_id": "content-entity:abc"},
		},
	}

	for _, tt := range cases {
		request, handled := Route(tt.tool, tt.args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tt.tool, request.Body)
		}
		if !reflect.DeepEqual(body, tt.want) {
			t.Errorf("Route(%s) body = %#v, want %#v", tt.tool, body, tt.want)
		}
	}
}

func TestRouteAppliesEntityResolutionDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	// resolve_entity sends only limit when every selector is absent: no name
	// key (the handler answers HTTP 400 "name is required"), no type key, and
	// no repo_id key. limit 10 matches the handler's own substitute for a
	// nonpositive limit before capping above 100 at 100.
	resolve, handled := Route("resolve_entity", nil)
	if !handled {
		t.Fatal("Route(resolve_entity, nil) handled = false, want true")
	}
	if want := map[string]any{"limit": 10}; !reflect.DeepEqual(resolve.Body, want) {
		t.Errorf("resolve_entity bare body = %#v, want %#v", resolve.Body, want)
	}
	if resolve.Query != nil {
		t.Errorf("resolve_entity query = %#v, want nil", resolve.Query)
	}

	// get_entity_content always sends entity_id, as an explicit empty string
	// when absent; the handler rejects it with HTTP 400 "entity_id is
	// required".
	content, handled := Route("get_entity_content", nil)
	if !handled {
		t.Fatal("Route(get_entity_content, nil) handled = false, want true")
	}
	if want := map[string]any{"entity_id": ""}; !reflect.DeepEqual(content.Body, want) {
		t.Errorf("get_entity_content bare body = %#v, want %#v", content.Body, want)
	}
	if content.Query != nil {
		t.Errorf("get_entity_content query = %#v, want nil", content.Query)
	}

	// get_entity_context carries no body and an always-non-nil query map,
	// empty when environment is absent — the same shape the root arm built.
	context, handled := Route("get_entity_context", nil)
	if !handled {
		t.Fatal("Route(get_entity_context, nil) handled = false, want true")
	}
	if context.Body != nil {
		t.Errorf("get_entity_context body = %#v, want nil", context.Body)
	}
	if context.Query == nil || len(context.Query) != 0 {
		t.Errorf("get_entity_context bare query = %#v, want a non-nil empty map", context.Query)
	}
}

// TestRouteMapsResolveEntityNameAndTypeConditionally pins resolve_entity's
// three conditional keys: name maps from the advertised query argument only
// when the deprecated name alias is blank and stays absent when both are
// blank; type prefers the single type argument and falls back to the first
// element of the deprecated types array — including to an explicit empty
// string when that first element is not a string, exactly as the root helper
// behaved; repo_id travels only when non-empty.
func TestRouteMapsResolveEntityNameAndTypeConditionally(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			name: "query maps to name when name is blank",
			args: routecontract.Arguments{"query": "svc-api"},
			want: map[string]any{"name": "svc-api", "limit": 10},
		},
		{
			name: "name wins over query",
			args: routecontract.Arguments{"name": "exact", "query": "loose"},
			want: map[string]any{"name": "exact", "limit": 10},
		},
		{
			name: "both blank leaves name absent",
			args: routecontract.Arguments{"name": "", "query": ""},
			want: map[string]any{"limit": 10},
		},
		{
			name: "type wins over types",
			args: routecontract.Arguments{"query": "svc", "type": "function", "types": []any{"workload"}},
			want: map[string]any{"name": "svc", "type": "function", "limit": 10},
		},
		{
			name: "types first element fills a blank type",
			args: routecontract.Arguments{"query": "svc", "types": []any{"workload", "function"}},
			want: map[string]any{"name": "svc", "type": "workload", "limit": 10},
		},
		{
			name: "non-string first types element still sets type, to empty",
			args: routecontract.Arguments{"query": "svc", "types": []any{42}},
			want: map[string]any{"name": "svc", "type": "", "limit": 10},
		},
		{
			name: "empty types array leaves type absent",
			args: routecontract.Arguments{"query": "svc", "types": []any{}},
			want: map[string]any{"name": "svc", "limit": 10},
		},
		{
			name: "repo_id travels only when non-empty",
			args: routecontract.Arguments{"query": "svc", "repo_id": ""},
			want: map[string]any{"name": "svc", "limit": 10},
		},
		{
			name: "populated repo_id travels",
			args: routecontract.Arguments{"query": "svc", "repo_id": "repo-1"},
			want: map[string]any{"name": "svc", "repo_id": "repo-1", "limit": 10},
		},
	}

	for _, tt := range cases {
		request, _ := Route("resolve_entity", tt.args)
		body := request.Body.(map[string]any)
		if !reflect.DeepEqual(body, tt.want) {
			t.Errorf("%s: body = %#v, want %#v", tt.name, body, tt.want)
		}
	}
}

// TestRouteEscapesEntityContextPathAndForwardsEnvironmentConditionally pins
// the family's one GET: the entity id is path-escaped into the URL, and
// environment travels as a query parameter only when the caller supplied a
// non-empty string.
func TestRouteEscapesEntityContextPathAndForwardsEnvironmentConditionally(t *testing.T) {
	t.Parallel()

	request, handled := Route("get_entity_context", routecontract.Arguments{
		"entity_id":   "content-entity:abc/def path",
		"environment": "prod",
	})
	if !handled {
		t.Fatal("Route(get_entity_context) handled = false, want true")
	}
	if want := "/api/v0/entities/content-entity:abc%2Fdef%20path/context"; request.Path != want {
		t.Errorf("path = %q, want %q", request.Path, want)
	}
	if want := map[string]string{"environment": "prod"}; !reflect.DeepEqual(request.Query, want) {
		t.Errorf("query = %#v, want %#v", request.Query, want)
	}

	for name, args := range map[string]routecontract.Arguments{
		"absent environment": {"entity_id": "e1"},
		"empty environment":  {"entity_id": "e1", "environment": ""},
		"wrong-typed":        {"entity_id": "e1", "environment": 7},
	} {
		request, _ := Route("get_entity_context", args)
		if request.Query == nil || len(request.Query) != 0 {
			t.Errorf("%s: query = %#v, want a non-nil empty map", name, request.Query)
		}
		if want := "/api/v0/entities/e1/context"; request.Path != want {
			t.Errorf("%s: path = %q, want %q", name, request.Path, want)
		}
	}
}

func TestRouteCoercesResolveEntityLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value any
		want  int
	}{
		{name: "int", value: int(9), want: 9},
		{name: "int64", value: int64(11), want: 11},
		{name: "float64", value: float64(13), want: 13},
		{name: "string falls back", value: "17", want: 10},
		{name: "bool falls back", value: true, want: 10},
		{name: "nil falls back", value: nil, want: 10},
	}

	for _, tt := range cases {
		request, _ := Route("resolve_entity", routecontract.Arguments{"limit": tt.value})
		body := request.Body.(map[string]any)
		if got := body["limit"]; got != tt.want {
			t.Errorf("limit %s (%#v) -> %#v, want %d", tt.name, tt.value, got, tt.want)
		}
	}
}

// TestRouteBuildsFreshBodyAndQueryMaps proves the selected body and query are
// not the caller's argument map: a probe key written through them must stay
// invisible to the caller, so a later dispatch cannot see one call's
// mutation.
func TestRouteBuildsFreshBodyAndQueryMaps(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"resolve_entity", "get_entity_content"} {
		args := routecontract.Arguments{"entity_id": "e1", "query": "svc"}
		request, _ := Route(tool, args)
		body := request.Body.(map[string]any)
		body["probe"] = "written-through-body"
		if _, leaked := args["probe"]; leaked {
			t.Errorf("Route(%s) body aliases the caller's argument map", tool)
		}
	}

	args := routecontract.Arguments{"entity_id": "e1", "environment": "qa"}
	request, _ := Route("get_entity_context", args)
	request.Query["probe"] = "written-through-query"
	if _, leaked := args["probe"]; leaked {
		t.Error("Route(get_entity_context) query aliases the caller's argument map")
	}
	if got := args["environment"]; got != "qa" {
		t.Errorf("Route(get_entity_context) mutated the caller's arguments: environment = %#v", got)
	}
}
