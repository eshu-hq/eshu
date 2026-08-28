// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshiptools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestEdgeRouteNormalizesAndForwardsSourceTool(t *testing.T) {
	t.Parallel()

	request, handled, err := EdgeRoute("list_relationship_edges", routecontract.Arguments{
		"verb":        "DEPENDS_ON",
		"source_tool": "  TeRrAfOrM  ",
		"limit":       int64(75),
	})
	if err != nil || !handled {
		t.Fatalf("EdgeRoute() = (_, %v, %v), want handled without error", handled, err)
	}
	want := routecontract.Request{Method: "POST", Path: "/api/v0/relationships/edges", Body: map[string]any{
		"verb": "DEPENDS_ON", "source_tool": "terraform", "limit": 75,
	}}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("EdgeRoute() = %#v, want %#v", request, want)
	}
}

func TestEdgeRouteEmptySourceToolOmitsFilter(t *testing.T) {
	t.Parallel()

	request, handled, err := EdgeRoute("list_relationship_edges", routecontract.Arguments{
		"verb":        "DEPLOYS_FROM",
		"source_tool": "   ",
	})
	if err != nil || !handled {
		t.Fatalf("EdgeRoute() = (_, %v, %v), want handled without error", handled, err)
	}
	body := requireRequestBody(t, request)
	if _, ok := body["source_tool"]; ok {
		t.Fatalf("body[source_tool] = %#v, want absent", body["source_tool"])
	}
	if got := body["limit"]; got != 50 {
		t.Fatalf("body[limit] = %#v, want 50", got)
	}
}

func TestEdgeRouteNonStringSourceToolOmitsFilter(t *testing.T) {
	t.Parallel()

	request, handled, err := EdgeRoute("list_relationship_edges", routecontract.Arguments{
		"verb":        "DEPLOYS_FROM",
		"source_tool": 7,
	})
	if err != nil || !handled {
		t.Fatalf("EdgeRoute() = (_, %v, %v), want handled without error", handled, err)
	}
	body := requireRequestBody(t, request)
	if _, ok := body["source_tool"]; ok {
		t.Fatalf("body[source_tool] = %#v, want absent for non-string input", body["source_tool"])
	}
}

func TestEdgeRouteRejectsUnknownSourceToolAndStaysClaimed(t *testing.T) {
	t.Parallel()

	request, handled, err := EdgeRoute("list_relationship_edges", routecontract.Arguments{
		"source_tool": "  Not_A_Real_Tool  ",
	})
	if !handled {
		t.Fatal("EdgeRoute() handled = false, want true")
	}
	const wantError = `unknown source_tool "not_a_real_tool": must be one of the canonical vocabulary values`
	if err == nil || err.Error() != wantError {
		t.Fatalf("EdgeRoute() error = %v, want %q", err, wantError)
	}
	if !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("EdgeRoute() request = %#v, want zero request", request)
	}
}

func TestEdgeRouteLimitPreservesIntOrMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  any
		set  bool
		want int
	}{
		{name: "missing", want: 50},
		{name: "int", raw: 11, set: true, want: 11},
		{name: "int64", raw: int64(12), set: true, want: 12},
		{name: "float64 truncates", raw: float64(13.9), set: true, want: 13},
		{name: "negative float64 truncates", raw: float64(-13.9), set: true, want: -13},
		{name: "float32 defaults", raw: float32(14.5), set: true, want: 50},
		{name: "uint defaults", raw: uint(14), set: true, want: 50},
		{name: "string defaults", raw: "15", set: true, want: 50},
		{name: "nil defaults", raw: nil, set: true, want: 50},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			args := routecontract.Arguments{"verb": "DEPENDS_ON"}
			if test.set {
				args["limit"] = test.raw
			}
			request, handled, err := EdgeRoute("list_relationship_edges", args)
			if err != nil || !handled {
				t.Fatalf("EdgeRoute() = (_, %v, %v), want handled without error", handled, err)
			}
			if got := requireRequestBody(t, request)["limit"]; got != test.want {
				t.Errorf("body[limit] = %#v, want %d", got, test.want)
			}
		})
	}
}

func TestEdgeRouteDoesNotClaimUnrelatedTools(t *testing.T) {
	t.Parallel()

	request, handled, err := EdgeRoute("get_code_relationship_story", nil)
	if err != nil || handled || !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("EdgeRoute(unrelated) = (%#v, %v, %v), want zero request, false, nil", request, handled, err)
	}
}
