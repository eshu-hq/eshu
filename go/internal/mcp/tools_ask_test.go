// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestAskToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "ask")
	if !strings.Contains(strings.ToLower(tool.Description), "natural-language") {
		t.Fatalf("description = %q, want natural-language guidance", tool.Description)
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"question", "format"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsAsk(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("ask", map[string]any{
		"question": "what is the deployment story for service X?",
		"format":   "markdown",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ask"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["question"], "what is the deployment story for service X?"; got != want {
		t.Fatalf("body[question] = %#v, want %#v", got, want)
	}
	if got, want := body["format"], "markdown"; got != want {
		t.Fatalf("body[format] = %#v, want %#v", got, want)
	}
}
