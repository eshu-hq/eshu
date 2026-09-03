// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iacmanagementtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyPaths pins the seven owned tool names to their internal paths,
// literally, so the ownership test cannot drift with the selector's own
// table.
var familyPaths = map[string]string{
	"find_dead_iac":                              "/api/v0/iac/dead",
	"find_unmanaged_resources":                   "/api/v0/iac/unmanaged-resources",
	"get_iac_management_status":                  "/api/v0/iac/management-status",
	"explain_iac_management_status":              "/api/v0/iac/management-status/explain",
	"propose_terraform_import_plan":              "/api/v0/iac/terraform-import-plan/candidates",
	"list_terraform_config_state_drift_findings": "/api/v0/terraform/config-state-drift/findings",
	"find_unmanaged_resource_owners":             "/api/v0/replatforming/ownership-packets",
}

func TestRouteOwnsExactlyTheIaCManagementFamily(t *testing.T) {
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
		"compose_replatforming_plan",
		"list_aws_runtime_drift_findings",
		"get_replatforming_rollups",
		"find_dead_code",
		"find_cross_repo_dead_code",
		"analyze_infra_relationships",
		"FIND_DEAD_IAC",
		"find_dead_iac_extra",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryIaCManagementBodyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			tool: "find_dead_iac",
			args: routecontract.Arguments{
				"repo_id": "repo-1", "repo_ids": []any{"terraform-stack", "terraform-modules"},
				"families": []any{"terraform"}, "include_ambiguous": true,
				"limit": float64(25), "offset": float64(50),
			},
			want: map[string]any{
				"repo_id": "repo-1", "repo_ids": []any{"terraform-stack", "terraform-modules"},
				"families": []any{"terraform"}, "include_ambiguous": true,
				"limit": 25, "offset": 50,
			},
		},
		{
			tool: "find_unmanaged_resources",
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
		{
			tool: "get_iac_management_status",
			args: routecontract.Arguments{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"unmanaged_cloud_resource"},
			},
			want: map[string]any{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"unmanaged_cloud_resource"},
				"limit":         1, "offset": 0,
			},
		},
		{
			tool: "explain_iac_management_status",
			args: routecontract.Arguments{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"unmanaged_cloud_resource"},
			},
			want: map[string]any{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"unmanaged_cloud_resource"},
				"limit":         1, "offset": 0,
			},
		},
		{
			tool: "propose_terraform_import_plan",
			args: routecontract.Arguments{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"orphaned_cloud_resource"},
				"limit":         float64(25), "offset": float64(50),
			},
			want: map[string]any{
				"scope_id": "aws:123456789012:us-east-1:lambda", "account_id": "123456789012",
				"region": "us-east-1", "arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				"finding_kinds": []any{"orphaned_cloud_resource"},
				"limit":         25, "offset": 50,
			},
		},
		{
			tool: "list_terraform_config_state_drift_findings",
			args: routecontract.Arguments{
				"scope_id": "state_snapshot:s3:hash-1", "address": "aws_lambda_function.payments_api",
				"outcome": "exact", "drift_kinds": []any{"added_in_state"},
				"limit": float64(25), "offset": float64(50),
			},
			want: map[string]any{
				"scope_id": "state_snapshot:s3:hash-1", "address": "aws_lambda_function.payments_api",
				"outcome": "exact", "drift_kinds": []any{"added_in_state"},
				"limit": 25, "offset": 50,
			},
		},
		{
			tool: "find_unmanaged_resource_owners",
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

// iacManagementDefaults pins the fallback each tool substitutes for an
// absent numeric argument, matching the values the root switch sent before
// the extraction. get_iac_management_status and explain_iac_management_status
// are omitted here: they always send limit 1 / offset 0 regardless of the
// caller's argument, which TestRouteCarriesEveryIaCManagementBodyKey already
// covers.
var iacManagementDefaults = map[string]map[string]int{
	"find_dead_iac":                              {"limit": 100, "offset": 0},
	"find_unmanaged_resources":                   {"limit": 100, "offset": 0},
	"propose_terraform_import_plan":              {"limit": 100, "offset": 0},
	"list_terraform_config_state_drift_findings": {"limit": 100, "offset": 0},
	"find_unmanaged_resource_owners":             {"limit": 100, "offset": 0},
}

func TestRouteAppliesIaCManagementDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	for tool, defaults := range iacManagementDefaults {
		request, handled := Route(tool, nil)
		if !handled {
			t.Fatalf("Route(%s, nil) handled = false, want true", tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tool, request.Body)
		}
		for key, want := range defaults {
			if got := body[key]; got != want {
				t.Errorf("Route(%s) absent %s -> %#v, want %#v", tool, key, got, want)
			}
		}
	}

	dead, _ := Route("find_dead_iac", nil)
	deadBody := dead.Body.(map[string]any)
	if got, present := deadBody["include_ambiguous"]; !present || got != false {
		t.Errorf("find_dead_iac absent include_ambiguous -> (%#v, %v), want an explicit false", got, present)
	}
	if got, present := deadBody["repo_id"]; !present || got != "" {
		t.Errorf("find_dead_iac absent repo_id -> (%#v, %v), want an explicit empty string", got, present)
	}

	status, _ := Route("get_iac_management_status", nil)
	statusBody := status.Body.(map[string]any)
	if got := statusBody["limit"]; got != 1 {
		t.Errorf("get_iac_management_status absent limit -> %#v, want the fixed 1", got)
	}
	if got := statusBody["offset"]; got != 0 {
		t.Errorf("get_iac_management_status absent offset -> %#v, want the fixed 0", got)
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
		request, _ := Route("find_dead_iac", routecontract.Arguments{"limit": tt.value})
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
