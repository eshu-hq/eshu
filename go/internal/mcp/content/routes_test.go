// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contenttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyPaths pins the five owned tool names to their internal paths and
// methods, literally, so the ownership test cannot drift with the
// selector's own table.
var familyPaths = map[string]string{
	"get_file_content":               "/api/v0/content/files/read",
	"get_file_lines":                 "/api/v0/content/files/lines",
	"build_evidence_citation_packet": "/api/v0/evidence/citations",
	"search_file_content":            "/api/v0/content/files/search",
	"search_entity_content":          "/api/v0/content/entities/search",
}

func TestRouteOwnsExactlyTheContentFamily(t *testing.T) {
	t.Parallel()

	for tool, wantPath := range familyPaths {
		request, handled := Route(tool, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tool)
		}
		if request.Method != "POST" {
			t.Errorf("Route(%s) method = %q, want POST", tool, request.Method)
		}
		if request.Path != wantPath {
			t.Errorf("Route(%s) path = %q, want %q", tool, request.Path, wantPath)
		}
		if request.Query != nil {
			t.Errorf("Route(%s) query = %#v, want nil", tool, request.Query)
		}
	}

	for _, tool := range []string{
		"",
		"get_file",
		"get_file_contents",
		"get_entity_content",
		"search_file",
		"search_entity",
		"search_entities_content",
		"build_evidence",
		"build_evidence_citation_packets",
		"resolve_entity",
		"find_code",
		"GET_FILE_CONTENT",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryContentBodyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			tool: "get_file_content",
			args: routecontract.Arguments{"repo_id": "repo-1", "relative_path": "src/main.go"},
			want: map[string]any{"repo_id": "repo-1", "relative_path": "src/main.go"},
		},
		{
			tool: "build_evidence_citation_packet",
			args: routecontract.Arguments{
				"subject":  map[string]any{"kind": "repo"},
				"question": "what is the auth flow?",
				"handles":  []any{map[string]any{"kind": "file", "repo_id": "repo-1"}},
				"limit":    float64(5),
			},
			want: map[string]any{
				"subject":  map[string]any{"kind": "repo"},
				"question": "what is the auth flow?",
				"handles":  []any{map[string]any{"kind": "file", "repo_id": "repo-1"}},
				"limit":    5,
			},
		},
		{
			tool: "search_file_content",
			args: routecontract.Arguments{"pattern": "logging", "repo_ids": []any{"repo-1"}, "limit": float64(20)},
			want: map[string]any{"query": "logging", "repo_id": "repo-1", "limit": 20, "offset": 0},
		},
		{
			tool: "search_entity_content",
			args: routecontract.Arguments{"query": "handler", "repo_ids": []any{"repo-1", "repo-2"}},
			want: map[string]any{"query": "handler", "repo_ids": []any{"repo-1", "repo-2"}, "limit": 10, "offset": 0},
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

// TestRouteForwardsGetFileLinesArgumentsUnchanged pins the one body shape in
// this family that is NOT a freshly built map: the root arm forwarded the
// caller's decoded arguments verbatim so the handler alone validates
// start_line/end_line, and this selector must keep that exact aliasing
// rather than silently copying it into a new map.
func TestRouteForwardsGetFileLinesArgumentsUnchanged(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{
		"repo_id":       "repo-1",
		"relative_path": "src/main.go",
		"start_line":    float64(10),
		"end_line":      float64(20),
	}
	request, handled := Route("get_file_lines", args)
	if !handled {
		t.Fatalf("Route(get_file_lines) handled = false, want true")
	}
	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("Route(get_file_lines) body type = %T, want map[string]any", request.Body)
	}
	if !reflect.DeepEqual(body, map[string]any(args)) {
		t.Errorf("Route(get_file_lines) body = %#v, want the caller's arguments verbatim %#v", body, args)
	}
	body["probe"] = "written-through-body"
	if _, leaked := args["probe"]; !leaked {
		t.Errorf("Route(get_file_lines) body no longer aliases the caller's argument map; this changes wire behavior")
	}
}

func TestRouteAppliesContentDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"search_file_content", "search_entity_content"} {
		request, handled := Route(tool, nil)
		if !handled {
			t.Fatalf("Route(%s, nil) handled = false, want true", tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tool, request.Body)
		}
		if got := body["limit"]; got != 10 {
			t.Errorf("%s absent limit -> %#v, want the handler-matching default 10", tool, got)
		}
		if got := body["offset"]; got != 0 {
			t.Errorf("%s absent offset -> %#v, want 0, the first page", tool, got)
		}
		if got, present := body["query"]; !present || got != "" {
			t.Errorf("%s absent query and pattern -> (%#v, %v), want an explicit empty string", tool, got, present)
		}
		if _, present := body["repo_id"]; present {
			t.Errorf("%s absent repo scope -> repo_id present, want omitted", tool)
		}
	}

	packet, handled := Route("build_evidence_citation_packet", nil)
	if !handled {
		t.Fatal("Route(build_evidence_citation_packet, nil) handled = false, want true")
	}
	packetBody := packet.Body.(map[string]any)
	if got := packetBody["limit"]; got != 10 {
		t.Errorf("absent limit -> %#v, want the handler-matching default 10", got)
	}
	if got := packetBody["subject"]; got != nil {
		t.Errorf("absent subject -> %#v, want nil", got)
	}
	if got := packetBody["handles"]; got != nil {
		t.Errorf("absent handles -> %#v, want nil", got)
	}
}

// TestRouteChoosesRepoScopeShapeByCardinality pins the three-way repo
// selector fan-out the root helper built: zero selectors omit repo_id
// entirely, exactly one collapses to a bare repo_id, and more than one
// switches to the repo_ids array -- the query handlers accept only one of
// the two shapes per call, so sending both or the wrong one is a silent
// wrong-scope query, not a type error.
func TestRouteChoosesRepoScopeShapeByCardinality(t *testing.T) {
	t.Parallel()

	zero, _ := Route("search_file_content", routecontract.Arguments{"query": "x"})
	zeroBody := zero.Body.(map[string]any)
	if _, present := zeroBody["repo_id"]; present {
		t.Errorf("zero repo selectors -> repo_id present, want omitted")
	}
	if _, present := zeroBody["repo_ids"]; present {
		t.Errorf("zero repo selectors -> repo_ids present, want omitted")
	}

	one, _ := Route("search_file_content", routecontract.Arguments{"query": "x", "repo_ids": []any{"repo-1"}})
	oneBody := one.Body.(map[string]any)
	if got, want := oneBody["repo_id"], "repo-1"; got != want {
		t.Errorf("one repo selector -> repo_id = %#v, want %#v", got, want)
	}
	if _, present := oneBody["repo_ids"]; present {
		t.Errorf("one repo selector -> repo_ids present, want collapsed to repo_id")
	}

	many, _ := Route("search_file_content", routecontract.Arguments{"query": "x", "repo_ids": []any{"repo-1", "repo-2"}})
	manyBody := many.Body.(map[string]any)
	if _, present := manyBody["repo_id"]; present {
		t.Errorf("many repo selectors -> repo_id present, want omitted")
	}
	if got, want := manyBody["repo_ids"], []any{"repo-1", "repo-2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("many repo selectors -> repo_ids = %#v, want %#v", got, want)
	}
}

func TestRouteCoercesIntegerArguments(t *testing.T) {
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
		request, _ := Route("search_file_content", routecontract.Arguments{"pattern": "x", "limit": tt.value})
		body := request.Body.(map[string]any)
		if got := body["limit"]; got != tt.want {
			t.Errorf("limit %s (%#v) -> %#v, want %d", tt.name, tt.value, got, tt.want)
		}
	}
}

// TestRouteBuildsAFreshBodyMap proves the search and citation-packet bodies
// are not the caller's argument map: a probe key written through the body
// must stay invisible to the caller. get_file_lines is excluded -- it
// deliberately aliases, see TestRouteForwardsGetFileLinesArgumentsUnchanged.
func TestRouteBuildsAFreshBodyMap(t *testing.T) {
	t.Parallel()

	for tool := range familyPaths {
		if tool == "get_file_lines" {
			continue
		}
		args := routecontract.Arguments{"repo_id": "repo-1", "pattern": "x", "query": "x"}
		request, _ := Route(tool, args)
		body := request.Body.(map[string]any)
		body["probe"] = "written-through-body"
		if _, leaked := args["probe"]; leaked {
			t.Errorf("Route(%s) body aliases the caller's argument map", tool)
		}
	}
}
