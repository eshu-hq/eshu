// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestSemanticEvidenceToolsRouteToBoundedHTTPReads(t *testing.T) {
	t.Parallel()

	registered := map[string]ToolDefinition{}
	for _, tool := range ReadOnlyTools() {
		registered[tool.Name] = tool
	}
	for _, name := range []string{
		"list_semantic_documentation_observations",
		"list_semantic_code_hints",
	} {
		tool, ok := registered[name]
		if !ok {
			t.Fatalf("ReadOnlyTools missing %q", name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %q InputSchema = nil", name)
		}
	}

	observations, err := resolveRoute("list_semantic_documentation_observations", map[string]any{
		"repo":                "repo:payments",
		"provider_profile_id": "semantic-docs-default",
		"freshness_state":     "fresh",
		"admission_state":     "partial",
		"limit":               25,
		"cursor":              "25",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_semantic_documentation_observations) error = %v, want nil", err)
	}
	if got, want := observations.Method, "GET"; got != want {
		t.Fatalf("observations.Method = %q, want %q", got, want)
	}
	if got, want := observations.Path, "/api/v0/semantic/documentation-observations"; got != want {
		t.Fatalf("observations.Path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"repo":                "repo:payments",
		"provider_profile_id": "semantic-docs-default",
		"freshness_state":     "fresh",
		"admission_state":     "partial",
		"limit":               "25",
		"cursor":              "25",
	} {
		if got := observations.Query[key]; got != want {
			t.Fatalf("observations.Query[%q] = %q, want %q", key, got, want)
		}
	}

	hints, err := resolveRoute("list_semantic_code_hints", map[string]any{
		"repo":                "repo:payments",
		"relative_path":       "go/payments/handler.go",
		"entity_id":           "entity:payments.Handle",
		"provider_profile_id": "semantic-code-default",
		"corroboration_state": "uncorroborated",
		"limit":               10,
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_semantic_code_hints) error = %v, want nil", err)
	}
	if got, want := hints.Path, "/api/v0/semantic/code-hints"; got != want {
		t.Fatalf("hints.Path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"repo":                "repo:payments",
		"relative_path":       "go/payments/handler.go",
		"entity_id":           "entity:payments.Handle",
		"provider_profile_id": "semantic-code-default",
		"corroboration_state": "uncorroborated",
		"limit":               "10",
	} {
		if got := hints.Query[key]; got != want {
			t.Fatalf("hints.Query[%q] = %q, want %q", key, got, want)
		}
	}
}
