// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsComposeReplatformingPlan(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("compose_replatforming_plan", map[string]any{
		"scope_kind":    "account",
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"service_name":  "payments",
		"finding_kinds": []any{"orphaned_cloud_resource"},
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/replatforming/plans" {
		t.Fatalf("route.path = %q, want /api/v0/replatforming/plans", route.path)
	}
	if route.method != "POST" {
		t.Fatalf("route.method = %q, want POST", route.method)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["scope_kind"], "account"; got != want {
		t.Fatalf("body[scope_kind] = %#v, want %#v", got, want)
	}
	if got, want := body["account_id"], "123456789012"; got != want {
		t.Fatalf("body[account_id] = %#v, want %#v", got, want)
	}
	if got, want := body["service_name"], "payments"; got != want {
		t.Fatalf("body[service_name] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 50; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
	kinds := body["finding_kinds"].([]any)
	if len(kinds) != 1 || kinds[0] != "orphaned_cloud_resource" {
		t.Fatalf("finding_kinds = %#v, want orphaned_cloud_resource", kinds)
	}
}
