// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	replatformingtools "github.com/eshu-hq/eshu/go/internal/mcp/replatforming"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// replatformingRouteTools maps every tool the child package owns to the path
// it must select, pinned literally so this file stays independent of the
// child's own table.
var replatformingRouteTools = map[string]string{
	"compose_replatforming_plan": "/api/v0/replatforming/plans",
	"get_replatforming_rollups":  "/api/v0/replatforming/rollups",
}

// replatformingBodyKeys lists the body keys each route must still send
// through dispatch, per tool, so a dropped or misspelled key fails here even
// if the child and the parity test drift together.
var replatformingBodyKeys = map[string][]string{
	"compose_replatforming_plan": {
		"scope_kind", "scope_id", "account_id", "region", "service_name",
		"workload_id", "repo_id", "environment", "arn", "resource_id",
		"finding_kinds", "limit", "offset",
	},
	"get_replatforming_rollups": {
		"scope_id", "account_id", "region", "finding_kinds", "limit", "offset",
	},
}

func TestResolveRouteUsesExactReplatformingChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"scope_kind": "account", "scope_id": "aws:123456789012:us-east-1:lambda",
			"account_id": "123456789012", "region": "us-east-1",
			"service_name": "payments", "workload_id": "workload:payments-api",
			"repo_id": "repo-1", "environment": "prod",
			"arn":           "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
			"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
			"finding_kinds": []any{"orphaned_cloud_resource"},
			"limit":         float64(25), "offset": float64(50),
		}},
		{name: "scope only", args: map[string]any{
			"scope_id": "aws:123456789012:us-east-1:lambda",
		}},
		{name: "malformed", args: map[string]any{
			"scope_kind":    42,
			"scope_id":      nil,
			"limit":         "25",
			"offset":        "3",
			"finding_kinds": "orphaned_cloud_resource",
		}},
	}

	for tool := range replatformingRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := replatformingtools.Route(tool, routecontract.Arguments(tt.args))
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

// TestReplatformingDispatchKeepsEveryBodyKey proves the fields survive the
// adapter boundary on every route, against literal expectations that are
// deliberately independent of the child selector: the parity test above
// builds both of its sides from that selector, so it cannot notice a key the
// child itself dropped or misspelled.
func TestReplatformingDispatchKeepsEveryBodyKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"scope_kind": "account", "scope_id": "aws:123456789012:us-east-1:lambda",
		"account_id": "123456789012", "region": "us-east-1",
		"service_name": "payments", "workload_id": "workload:payments-api",
		"repo_id": "repo-1", "environment": "prod",
		"arn":           "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		"finding_kinds": []any{"orphaned_cloud_resource"},
		"limit":         float64(25), "offset": float64(50),
	}
	want := map[string]any{
		"scope_kind": "account", "scope_id": "aws:123456789012:us-east-1:lambda",
		"account_id": "123456789012", "region": "us-east-1",
		"service_name": "payments", "workload_id": "workload:payments-api",
		"repo_id": "repo-1", "environment": "prod",
		"arn":           "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		"finding_kinds": []any{"orphaned_cloud_resource"},
		"limit":         25, "offset": 50,
	}

	for tool, wantPath := range replatformingRouteTools {
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if got.method != "POST" {
			t.Errorf("%s method = %q, want POST", tool, got.method)
		}
		if got.path != wantPath {
			t.Errorf("%s path = %q, want %q", tool, got.path, wantPath)
		}
		if got.query != nil {
			t.Errorf("%s query = %#v, want nil", tool, got.query)
		}
		body, ok := got.body.(map[string]any)
		if !ok {
			t.Fatalf("%s body type = %T, want map[string]any", tool, got.body)
		}
		keys := replatformingBodyKeys[tool]
		if n, wantN := len(body), len(keys); n != wantN {
			t.Fatalf("%s body carries %d keys (%#v), want %d", tool, n, body, wantN)
		}
		for _, key := range keys {
			value, present := body[key]
			if !present {
				t.Errorf("%s dispatch dropped %q entirely", tool, key)
				continue
			}
			if !reflect.DeepEqual(value, want[key]) {
				t.Errorf("%s body[%s] = %#v, want %#v", tool, key, value, want[key])
			}
		}
	}

	// The defaults reach the handler unchanged when the caller sends nothing:
	// limit 100 matches the handler's own substitute for a nonpositive limit,
	// offset 0 is the first page, and the unset string filters still travel
	// as explicit empty strings.
	bare, err := resolveRoute("compose_replatforming_plan", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(bare) error = %v, want nil", err)
	}
	bareBody := bare.body.(map[string]any)
	if value := bareBody["limit"]; value != 100 {
		t.Errorf("absent limit -> %#v, want the default 100", value)
	}
	if value := bareBody["offset"]; value != 0 {
		t.Errorf("absent offset -> %#v, want 0", value)
	}
	for _, key := range []string{"scope_kind", "scope_id", "account_id", "arn"} {
		if value, present := bareBody[key]; !present || value != "" {
			t.Errorf("absent %s -> (%#v, %v), want an explicit empty string", key, value, present)
		}
	}

	// The rollup never carries arn, even when dispatched through resolveRoute
	// with one supplied, because the summary is account/environment/service
	// scoped rather than single-resource.
	rollup, err := resolveRoute("get_replatforming_rollups", map[string]any{
		"arn": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
	})
	if err != nil {
		t.Fatalf("resolveRoute(rollup with arn) error = %v, want nil", err)
	}
	rollupBody := rollup.body.(map[string]any)
	if _, present := rollupBody["arn"]; present {
		t.Errorf("get_replatforming_rollups dispatch carried arn: %#v", rollupBody)
	}
}

// TestResolveRouteStillOwnsItsArmsAfterReplatforming proves the delegation
// added ahead of the switch claims only this family and leaves every
// neighbouring code, IaC, relationship, and content arm answered as before.
func TestResolveRouteStillOwnsItsArmsAfterReplatforming(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_aws_runtime_drift_findings",
		"find_unmanaged_resource_owners",
		"find_unmanaged_resources",
		"get_iac_management_status",
		"find_dead_iac",
		"find_dead_code",
		"investigate_hardcoded_secrets",
		"get_file_content",
		"search_file_content",
	} {
		if _, handled := replatformingRoute(tool, map[string]any{}); handled {
			t.Errorf("replatformingRoute(%s) handled = true, want false", tool)
		}
		got, err := resolveRoute(tool, map[string]any{})
		if err != nil {
			t.Errorf("resolveRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if got == nil {
			t.Errorf("resolveRoute(%s) = nil, want a route", tool)
		}
	}

	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestReplatformingRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the two owned names are claimed, and
// near-miss names, including the sibling list_aws_runtime_drift_findings
// that stayed in dispatch_iac.go's switch, are not.
func TestReplatformingRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for tool := range replatformingRouteTools {
		if _, handled := replatformingRoute(tool, map[string]any{}); !handled {
			t.Errorf("replatformingRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
		"list_aws_runtime_drift_findings",
		"find_unmanaged_resource_owners",
		"compose_replatforming_plans",
		"get_replatforming_rollup",
		"COMPOSE_REPLATFORMING_PLAN",
	} {
		if _, handled := replatformingRoute(tool, map[string]any{}); handled {
			t.Errorf("replatformingRoute(%q) handled = true, want false", tool)
		}
	}
}
