// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestDeadLetterWorkItemsToolResolvesToAdminQueryRoute(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_dead_letter_work_items", map[string]any{
		"failure_class":  "projection_bug",
		"domain":         "runtime",
		"scope_id":       "scope-a",
		"collector_kind": "git",
		"updated_after":  "2026-07-06T13:00:00Z",
		"updated_before": "2026-07-06T14:00:00Z",
		"limit":          float64(25),
		"timeout_ms":     float64(5000),
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_dead_letter_work_items) error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/admin/dead-letters/query"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	body := route.body.(map[string]any)
	for _, key := range []string{
		"failure_class",
		"domain",
		"scope_id",
		"collector_kind",
		"updated_after",
		"updated_before",
		"limit",
		"timeout_ms",
	} {
		if _, ok := body[key]; !ok {
			t.Fatalf("body missing %q: %#v", key, body)
		}
	}
}

func TestDeadLetterWorkItemsToolRequiresLimitAndTimeout(t *testing.T) {
	t.Parallel()

	if _, err := resolveRoute("list_dead_letter_work_items", map[string]any{"limit": float64(10)}); err == nil {
		t.Fatal("resolveRoute without timeout_ms error = nil, want error")
	}
	if _, err := resolveRoute("list_dead_letter_work_items", map[string]any{"timeout_ms": float64(5000)}); err == nil {
		t.Fatal("resolveRoute without limit error = nil, want error")
	}
}

func TestReadOnlyToolsIncludesDeadLetterWorkItems(t *testing.T) {
	t.Parallel()

	for _, tool := range ReadOnlyTools() {
		if tool.Name == "list_dead_letter_work_items" {
			return
		}
	}
	t.Fatal("ReadOnlyTools() missing list_dead_letter_work_items")
}
