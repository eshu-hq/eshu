// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
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

	got, err := resolveRoute("ask", map[string]any{
		"question": "what is the deployment story for service X?",
		"format":   "markdown",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	want := &route{
		method: "POST",
		path:   "/api/v0/ask",
		body: map[string]any{
			"question": "what is the deployment story for service X?",
			"format":   "markdown",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveRoute(ask) route = %#v, want %#v", got, want)
	}
}
