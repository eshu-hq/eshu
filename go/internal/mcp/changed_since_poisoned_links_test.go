// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestChangedSincePoisonedLinksToolResolvesToAdminQueryRoute(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_changed_since_poisoned_links", map[string]any{
		"status":     "poisoned",
		"scope_id":   "scope-a",
		"cursor":     "scope-0",
		"limit":      float64(25),
		"timeout_ms": float64(5000),
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_changed_since_poisoned_links) error = %v, want nil", err)
	}
	if got, want := route.Method, "POST"; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/admin/changed-since/poisoned-links/query"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	body := route.Body.(map[string]any)
	for _, key := range []string{"status", "scope_id", "cursor", "limit", "timeout_ms"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("body missing %q: %#v", key, body)
		}
	}
}

func TestChangedSincePoisonedLinksToolRequiresLimitAndTimeout(t *testing.T) {
	t.Parallel()

	if _, err := resolveRoute("list_changed_since_poisoned_links", map[string]any{"limit": float64(10)}); err == nil {
		t.Fatal("resolveRoute without timeout_ms error = nil, want error")
	}
	if _, err := resolveRoute("list_changed_since_poisoned_links", map[string]any{"timeout_ms": float64(5000)}); err == nil {
		t.Fatal("resolveRoute without limit error = nil, want error")
	}
}

func TestReadOnlyToolsIncludesChangedSincePoisonedLinks(t *testing.T) {
	t.Parallel()

	for _, tool := range ReadOnlyTools() {
		if tool.Name == "list_changed_since_poisoned_links" {
			return
		}
	}
	t.Fatal("ReadOnlyTools() missing list_changed_since_poisoned_links")
}
