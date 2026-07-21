// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

// This file holds the codeowners-ownership OpenAPI assertions rather than
// extending openapi_test.go: that file is already at 944 lines, over the
// repo's 500-line cap (golang-engineering skill), so any new test there would
// only grow an existing violation. mustMapField is defined in openapi_test.go
// and shared across the package's test files.

// TestOpenAPISpecIncludesCodeownersOwnershipPath is the codeowners-scoped
// equivalent of TestServeOpenAPI's expectedPaths check (openapi_test.go),
// kept in its own file for the file-cap reason above.
func TestOpenAPISpecIncludesCodeownersOwnershipPath(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	if _, ok := paths["/api/v0/codeowners/ownership"]; !ok {
		t.Fatal("expected path /api/v0/codeowners/ownership not found in spec")
	}
}

func TestOpenAPICodeownersOwnershipDescribesEffectiveOwnerSchema(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths := mustMapField(t, spec, "paths")
	ownershipPath := mustMapField(t, paths, "/api/v0/codeowners/ownership")
	ownershipGet := mustMapField(t, ownershipPath, "get")

	parameters, ok := ownershipGet["parameters"].([]any)
	if !ok {
		t.Fatal("codeowners ownership parameters missing or not an array")
	}
	var repositoryIDRequired bool
	for _, raw := range parameters {
		param, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if param["name"] == "repository_id" {
			repositoryIDRequired, _ = param["required"].(bool)
		}
	}
	if !repositoryIDRequired {
		t.Fatal("codeowners ownership repository_id parameter must be required")
	}

	responses := mustMapField(t, ownershipGet, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, mustMapField(t, content, "schema"), "properties")
	for _, field := range []string{"ownership", "repository_id", "count", "limit", "truncated", "next_cursor", "effective_owner"} {
		if _, ok := schema[field]; !ok {
			t.Fatalf("codeowners/ownership response schema missing %s", field)
		}
	}

	effectiveOwnerSchema := mustMapField(t, mustMapField(t, schema, "effective_owner"), "properties")
	for _, field := range []string{"owner_ref", "source"} {
		if _, ok := effectiveOwnerSchema[field]; !ok {
			t.Fatalf("codeowners/ownership effective_owner schema missing %s", field)
		}
	}
}
