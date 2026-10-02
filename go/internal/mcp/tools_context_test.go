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
	if got, want := route.Method, "POST"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/entities/resolve"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	body, _ := route.Body.(map[string]any)
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
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/entities/ent-1/context"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
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
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/workloads/wl-1/context"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
}

// TestResolveRouteGetWorkloadContextEvidenceDetail proves the MCP default is
// handles (#7129), an explicit value wins, and a bad value travels verbatim so
// the handler rejects it with a 400; get_workload_story is left alone.
func TestResolveRouteGetWorkloadContextEvidenceDetail(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, arg, want string }{
		{"default", "", "handles"},
		{"explicit full", "full", "full"},
		{"unknown value is forwarded", "compact", "compact"},
	} {
		args := map[string]any{"workload_id": "wl-1"}
		if tc.arg != "" {
			args["evidence_detail"] = tc.arg
		}
		route, err := resolveRoute("get_workload_context", args)
		if err != nil {
			t.Fatalf("%s: resolveRoute() error = %v", tc.name, err)
		}
		if got := route.Query["evidence_detail"]; got != tc.want {
			t.Fatalf("%s: evidence_detail = %q, want %q", tc.name, got, tc.want)
		}
	}
	story, err := resolveRoute("get_workload_story", map[string]any{"workload_id": "wl-1"})
	if err != nil {
		t.Fatalf("resolveRoute(story) error = %v", err)
	}
	if _, has := story.Query["evidence_detail"]; has {
		t.Fatalf("story query = %#v, want no evidence_detail", story.Query)
	}
}

// TestContextToolsDocumentEvidenceDetail proves both context tools advertise
// the evidence_detail enum and say that handles is the default.
func TestContextToolsDocumentEvidenceDetail(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"get_workload_context", "get_service_context"} {
		tool := requireToolDefinition(t, name)
		schema, _ := tool.InputSchema.(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		detail, _ := properties["evidence_detail"].(map[string]any)
		if detail == nil {
			t.Fatalf("%s has no evidence_detail property", name)
		}
		if got, _ := detail["enum"].([]string); len(got) != 2 || got[0] != "full" || got[1] != "handles" {
			t.Fatalf("%s evidence_detail enum = %#v, want [full handles]", name, detail["enum"])
		}
		if description, _ := detail["description"].(string); !strings.Contains(description, "handles") || !strings.Contains(description, "default") {
			t.Fatalf("%s evidence_detail description = %q, want it to state the handles default", name, description)
		}
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
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/workloads/wl-1/story"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
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
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/services/svc-1/context"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
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
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/services/svc-1/story"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
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
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/investigations/services/my-svc"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
}
