// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replatformingtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyPaths pins the two owned tool names to their internal paths,
// literally, so the ownership test cannot drift with the selector's own
// table.
var familyPaths = map[string]string{
	"compose_replatforming_plan": "/api/v0/replatforming/plans",
	"get_replatforming_rollups":  "/api/v0/replatforming/rollups",
}

func TestRouteOwnsExactlyTheReplatformingFamily(t *testing.T) {
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
		"list_aws_runtime_drift_findings",
		"find_unmanaged_resource_owners",
		"find_unmanaged_resources",
		"find_dead_iac",
		"compose_replatforming_plans",
		"get_replatforming_rollup",
		"COMPOSE_REPLATFORMING_PLAN",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryReplatformingBodyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			tool: "compose_replatforming_plan",
			args: routecontract.Arguments{
				"scope_kind": "account", "scope_id": "aws:123456789012:us-east-1:lambda",
				"account_id": "123456789012", "region": "us-east-1",
				"service_name": "payments", "workload_id": "workload:payments-api",
				"repo_id": "repo-1", "environment": "prod",
				"arn":           "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"orphaned_cloud_resource"},
				"limit":         float64(25), "offset": float64(50),
			},
			want: map[string]any{
				"scope_kind": "account", "scope_id": "aws:123456789012:us-east-1:lambda",
				"account_id": "123456789012", "region": "us-east-1",
				"service_name": "payments", "workload_id": "workload:payments-api",
				"repo_id": "repo-1", "environment": "prod",
				"arn":           "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"orphaned_cloud_resource"},
				"limit":         25, "offset": 50,
			},
		},
		{
			tool: "get_replatforming_rollups",
			args: routecontract.Arguments{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "finding_kinds": []any{"unmanaged_cloud_resource"},
				"limit": float64(25), "offset": float64(50),
			},
			want: map[string]any{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "finding_kinds": []any{"unmanaged_cloud_resource"},
				"limit": 25, "offset": 50,
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

// TestReplatformingRollupsBodyNeverCarriesArn pins the deliberate asymmetry
// with compose_replatforming_plan: the rollup is account/environment/service
// scoped, not single-resource, so it must never forward an arn key that
// would narrow the summary to one resource, even when the caller sends one.
func TestReplatformingRollupsBodyNeverCarriesArn(t *testing.T) {
	t.Parallel()

	request, handled := Route("get_replatforming_rollups", routecontract.Arguments{
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
	})
	if !handled {
		t.Fatal("Route(get_replatforming_rollups) handled = false, want true")
	}
	body := request.Body.(map[string]any)
	if _, present := body["arn"]; present {
		t.Fatalf("body must not carry arn for the rollup: %#v", body)
	}
}

func TestRouteAppliesReplatformingDefaultsForAbsentArguments(t *testing.T) {
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
		if got := body["offset"]; got != 0 {
			t.Errorf("%s absent offset -> %#v, want 0, the first page", tool, got)
		}
		if got, present := body["scope_id"]; !present || got != "" {
			t.Errorf("%s absent scope_id -> (%#v, %v), want an explicit empty string", tool, got, present)
		}
		if got, present := body["account_id"]; !present || got != "" {
			t.Errorf("%s absent account_id -> (%#v, %v), want an explicit empty string", tool, got, present)
		}
	}

	plan, _ := Route("compose_replatforming_plan", nil)
	planBody := plan.Body.(map[string]any)
	if got, present := planBody["scope_kind"]; !present || got != "" {
		t.Errorf("compose_replatforming_plan absent scope_kind -> (%#v, %v), want an explicit empty string", got, present)
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
		request, _ := Route("compose_replatforming_plan", routecontract.Arguments{"limit": tt.value})
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
		args := routecontract.Arguments{"scope_id": "aws:123456789012:us-east-1:lambda"}
		request, _ := Route(tool, args)
		body := request.Body.(map[string]any)
		body["probe"] = "written-through-body"
		if _, leaked := args["probe"]; leaked {
			t.Errorf("Route(%s) body aliases the caller's argument map", tool)
		}
		if got := args["scope_id"]; got != "aws:123456789012:us-east-1:lambda" {
			t.Errorf("Route(%s) mutated the caller's arguments: scope_id = %#v", tool, got)
		}
	}
}
