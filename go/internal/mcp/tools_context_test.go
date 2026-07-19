// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestContextToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"resolve_entity", "get_entity_context", "get_workload_context", "get_workload_story", "get_service_context", "get_service_story", "investigate_service"} {
		tool := requireToolDefinition(t, name)
		_, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
	}
}

func TestResolveEntitySchemaDocumentsExactTypedGlobalContract(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "resolve_entity")
	schema, _ := tool.InputSchema.(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"query", "type", "types", "repo_id", "limit"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("resolve_entity schema missing %q", field)
		}
	}
	description := strings.ToLower(tool.Description)
	for _, fragment := range []string{"exact", "case-sensitive", "global", "content-entity"} {
		if !strings.Contains(description, fragment) {
			t.Fatalf("resolve_entity description missing %q: %s", fragment, tool.Description)
		}
	}
}

func TestResolveRouteMapsContextResolveEntity(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("resolve_entity", map[string]any{
		"query": "my-service-api",
		"type":  "function",
		"limit": float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/entities/resolve"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, _ := route.body.(map[string]any)
	if body["name"] != "my-service-api" || body["type"] != "function" {
		t.Fatalf("route body = %#v, want exact typed global resolve", body)
	}
}

func TestResolveRouteMapsGetEntityContext(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_entity_context", map[string]any{
		"entity_id":   "ent-1",
		"environment": "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/entities/ent-1/context"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetWorkloadContext(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_workload_context", map[string]any{
		"workload_id": "wl-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/workloads/wl-1/context"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetWorkloadStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_workload_story", map[string]any{
		"workload_id": "wl-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/workloads/wl-1/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetServiceContext(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_context", map[string]any{
		"workload_id": "svc-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/services/svc-1/context"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetServiceStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_story", map[string]any{
		"workload_id": "svc-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/services/svc-1/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsInvestigateServiceContextTool(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_service", map[string]any{
		"service_name": "my-svc",
		"environment":  "prod",
		"intent":       "incident",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/investigations/services/my-svc"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
