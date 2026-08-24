// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"testing"
)

func TestReadOnlyToolsKeepsAskRegistrationPosition(t *testing.T) {
	t.Parallel()

	tools := ReadOnlyTools()
	for index, tool := range tools {
		if tool.Name != "ask" {
			continue
		}
		if got, want := index+1, 160; got != want {
			t.Fatalf("ask registration position = %d, want %d", got, want)
		}
		if index == 0 || index+1 >= len(tools) {
			t.Fatalf("ask registration index = %d, want two neighbors", index)
		}
		if got, want := tools[index-1].Name, "trace_exposure_path"; got != want {
			t.Fatalf("tool before ask = %q, want %q", got, want)
		}
		if got, want := tools[index+1].Name, "list_relationship_edges"; got != want {
			t.Fatalf("tool after ask = %q, want %q", got, want)
		}
		return
	}
	t.Fatal("ask tool is not registered")
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
