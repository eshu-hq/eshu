// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteListCloudRuntimeDriftForwardsBoundedFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_cloud_runtime_drift_findings", map[string]any{
		"project_id":         "project-synthetic",
		"provider":           "gcp",
		"cloud_resource_uid": "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
		"finding_kinds":      []any{"orphaned_cloud_resource"},
		"limit":              25,
		"offset":             50,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/cloud/runtime-drift/findings"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["project_id"], "project-synthetic"; got != want {
		t.Fatalf("body[project_id] = %v, want %v", got, want)
	}
	if got, want := body["provider"], "gcp"; got != want {
		t.Fatalf("body[provider] = %v, want %v", got, want)
	}
	if got, want := body["cloud_resource_uid"], "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1"; got != want {
		t.Fatalf("body[cloud_resource_uid] = %v, want %v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %v, want %v", got, want)
	}
	if got, want := body["offset"], 50; got != want {
		t.Fatalf("body[offset] = %v, want %v", got, want)
	}
	kinds, ok := body["finding_kinds"].([]any)
	if !ok {
		t.Fatalf("body[finding_kinds] type = %T, want []any", body["finding_kinds"])
	}
	if len(kinds) != 1 || kinds[0] != "orphaned_cloud_resource" {
		t.Fatalf("body[finding_kinds] = %#v, want [orphaned_cloud_resource]", kinds)
	}
}

func TestCloudRuntimeDriftToolAdvertisesProviderAndScopeAliases(t *testing.T) {
	t.Parallel()

	tools := cloudRuntimeDriftTools()
	if got, want := len(tools), 1; got != want {
		t.Fatalf("len(cloudRuntimeDriftTools()) = %d, want %d", got, want)
	}
	schema, ok := tools[0].InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tools[0].InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, key := range []string{
		"scope_id", "account_id", "project_id", "subscription_id",
		"provider", "cloud_resource_uid", "finding_kinds", "limit", "offset",
	} {
		if _, present := properties[key]; !present {
			t.Fatalf("schema missing property %q", key)
		}
	}
}
