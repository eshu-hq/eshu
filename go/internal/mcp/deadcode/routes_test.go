// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcodetools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyPaths pins the three owned tool names to their internal paths,
// literally, so the ownership test cannot drift with the selector's own
// table.
var familyPaths = map[string]string{
	"find_dead_code":            "/api/v0/code/dead-code",
	"investigate_dead_code":     "/api/v0/code/dead-code/investigate",
	"find_cross_repo_dead_code": "/api/v0/code/dead-code/cross-repo",
}

func TestRouteOwnsExactlyTheDeadCodeFamily(t *testing.T) {
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
		"find_dead",
		"find_dead_codes",
		"find_dead_code_extra",
		"dead_code",
		"find_dead_iac",
		"investigate_dead",
		"find_cross_repo_dead",
		"analyze_code_relationships",
		"FIND_DEAD_CODE",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryDeadCodeBodyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			tool: "find_dead_code",
			args: routecontract.Arguments{
				"repo_id":                "repo-1",
				"limit":                  float64(7),
				"exclude_decorated_with": []any{"deprecated", "celery.task"},
			},
			want: map[string]any{
				"repo_id":                "repo-1",
				"limit":                  7,
				"exclude_decorated_with": []any{"deprecated", "celery.task"},
			},
		},
		{
			tool: "investigate_dead_code",
			args: routecontract.Arguments{
				"repo_id":                "repo-1",
				"language":               "python",
				"limit":                  float64(7),
				"offset":                 float64(3),
				"exclude_decorated_with": []any{"deprecated"},
			},
			want: map[string]any{
				"repo_id":                "repo-1",
				"language":               "python",
				"limit":                  7,
				"offset":                 3,
				"exclude_decorated_with": []any{"deprecated"},
			},
		},
		{
			tool: "find_cross_repo_dead_code",
			args: routecontract.Arguments{
				"repo_id":                "repo-producer",
				"consumer_repo_ids":      []any{"repo-consumer", "", 7, "repo-consumer-2"},
				"language":               "go",
				"limit":                  float64(7),
				"exclude_decorated_with": []any{"deprecated"},
			},
			want: map[string]any{
				"repo_id":                "repo-producer",
				"consumer_repo_ids":      []string{"repo-consumer", "repo-consumer-2"},
				"language":               "go",
				"limit":                  7,
				"exclude_decorated_with": []any{"deprecated"},
			},
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

func TestRouteAppliesDeadCodeDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	for tool := range familyPaths {
		request, handled := Route(tool, nil)
		if !handled {
			t.Fatalf("Route(%s, nil) handled = false, want true", tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tool, request.Body)
		}
		if got := body["limit"]; got != 100 {
			t.Errorf("%s absent limit -> %#v, want the handler-matching default 100", tool, got)
		}
		if got, present := body["repo_id"]; !present || got != "" {
			t.Errorf("%s absent repo_id -> (%#v, %v), want an explicit empty string", tool, got, present)
		}
	}

	investigate, _ := Route("investigate_dead_code", nil)
	investigateBody := investigate.Body.(map[string]any)
	if got := investigateBody["offset"]; got != 0 {
		t.Errorf("absent offset -> %#v, want 0, the first page", got)
	}
	if got, present := investigateBody["language"]; !present || got != "" {
		t.Errorf("absent language -> (%#v, %v), want an explicit empty string", got, present)
	}
}

// TestRouteSeparatesAbsentAndEmptyExclusions pins the wire difference the
// root helpers produced: an absent or malformed exclude_decorated_with
// travels as a nil []any and serializes as null, while a present empty list
// travels as a non-nil empty []any and serializes as []. A len comparison
// cannot tell these apart, so the assertions check nil-ness directly.
func TestRouteSeparatesAbsentAndEmptyExclusions(t *testing.T) {
	t.Parallel()

	for tool := range familyPaths {
		absent, _ := Route(tool, routecontract.Arguments{})
		absentBody := absent.Body.(map[string]any)
		excluded, present := absentBody["exclude_decorated_with"]
		if !present {
			t.Fatalf("%s dropped exclude_decorated_with entirely, want an explicit nil", tool)
		}
		if slice, ok := excluded.([]any); !ok || slice != nil {
			t.Errorf("%s absent exclusions -> %#v (%T), want a nil []any", tool, excluded, excluded)
		}

		malformed, _ := Route(tool, routecontract.Arguments{"exclude_decorated_with": "deprecated"})
		malformedBody := malformed.Body.(map[string]any)
		if slice, ok := malformedBody["exclude_decorated_with"].([]any); !ok || slice != nil {
			t.Errorf("%s malformed exclusions -> %#v, want a nil []any", tool, malformedBody["exclude_decorated_with"])
		}

		empty, _ := Route(tool, routecontract.Arguments{"exclude_decorated_with": []any{}})
		emptyBody := empty.Body.(map[string]any)
		slice, ok := emptyBody["exclude_decorated_with"].([]any)
		if !ok || slice == nil || len(slice) != 0 {
			t.Errorf("%s empty exclusions -> %#v, want a non-nil empty []any", tool, emptyBody["exclude_decorated_with"])
		}
	}
}

// TestRouteAlwaysSendsConsumerRepoIDsAsStrings pins the opposite contract for
// consumer_repo_ids: the root stringValues helper always returned a non-nil
// []string, dropping empty strings and non-string members, so an absent
// argument serializes as [] rather than null.
func TestRouteAlwaysSendsConsumerRepoIDsAsStrings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args routecontract.Arguments
		want []string
	}{
		{name: "absent", args: routecontract.Arguments{}, want: []string{}},
		{name: "nil arguments", args: nil, want: []string{}},
		{name: "malformed", args: routecontract.Arguments{"consumer_repo_ids": "repo-1"}, want: []string{}},
		{
			name: "mixed members",
			args: routecontract.Arguments{"consumer_repo_ids": []any{"repo-1", "", 7, nil, "repo-2"}},
			want: []string{"repo-1", "repo-2"},
		},
		{
			name: "typed strings",
			args: routecontract.Arguments{"consumer_repo_ids": []string{"repo-1", "repo-2"}},
			want: []string{"repo-1", "repo-2"},
		},
	}

	for _, tt := range cases {
		request, handled := Route("find_cross_repo_dead_code", tt.args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.name)
		}
		body := request.Body.(map[string]any)
		got, ok := body["consumer_repo_ids"].([]string)
		if !ok {
			t.Fatalf("%s consumer_repo_ids type = %T, want []string", tt.name, body["consumer_repo_ids"])
		}
		if got == nil {
			t.Fatalf("%s consumer_repo_ids is nil, want a non-nil slice that serializes as []", tt.name)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s consumer_repo_ids = %#v, want %#v", tt.name, got, tt.want)
		}
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
		{name: "string falls back", value: "17", want: 100},
		{name: "bool falls back", value: true, want: 100},
		{name: "nil falls back", value: nil, want: 100},
	}

	for _, tt := range cases {
		request, _ := Route("find_dead_code", routecontract.Arguments{"limit": tt.value})
		body := request.Body.(map[string]any)
		if got := body["limit"]; got != tt.want {
			t.Errorf("limit %s (%#v) -> %#v, want %d", tt.name, tt.value, got, tt.want)
		}
	}
}

// TestRouteBuildsAFreshBodyMap proves the selected body is not the caller's
// argument map: a probe key written through the body must stay invisible to
// the caller, so a later dispatch cannot see one call's mutation.
func TestRouteBuildsAFreshBodyMap(t *testing.T) {
	t.Parallel()

	for tool := range familyPaths {
		args := routecontract.Arguments{"repo_id": "repo-1"}
		request, _ := Route(tool, args)
		body := request.Body.(map[string]any)
		body["probe"] = "written-through-body"
		if _, leaked := args["probe"]; leaked {
			t.Errorf("Route(%s) body aliases the caller's argument map", tool)
		}
		if got := args["repo_id"]; got != "repo-1" {
			t.Errorf("Route(%s) mutated the caller's arguments: repo_id = %#v", tool, got)
		}
	}
}
