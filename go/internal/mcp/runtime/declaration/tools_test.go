// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimedeclarationtools

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

func TestToolSchemaBoundsMirrorTheHandler(t *testing.T) {
	t.Parallel()

	tools := Tools()
	if len(tools) != 1 || tools[0].Name != "list_runtime_version_declarations" {
		t.Fatalf("Tools() = %#v", tools)
	}
	schema := tools[0].InputSchema.(map[string]any)
	if _, required := schema["required"]; required {
		t.Errorf("no argument is required, got required = %#v", schema["required"])
	}
	properties := schema["properties"].(map[string]any)
	for _, field := range []string{"runtime", "major_below", "version_below", "source_kind", "repo_ids", "limit", "cursor"} {
		if _, ok := properties[field]; !ok {
			t.Errorf("properties missing %q", field)
		}
	}
	limit := properties["limit"].(map[string]any)
	if limit["default"] != 50 || limit["maximum"] != 200 || limit["minimum"] != 1 {
		t.Errorf("limit schema = %v, want default 50, range 1..200", limit)
	}
	if properties["repo_ids"].(map[string]any)["maxItems"] != 100 {
		t.Errorf("repo_ids maxItems = %v, want 100", properties["repo_ids"].(map[string]any)["maxItems"])
	}
	// The schema must use only keywords the router validator implements:
	// bool additionalProperties and maxLength are not among them (the
	// validator ignores both), so closed-ness and length caps live in the
	// HTTP handler (DisallowUnknownFields plus the
	// maxFilterLength/maxRepoIDLength/maxRepoIDs caps in request.go). This
	// pins the drift-guard contract locally for this tool.
	if findings := router.UnsupportedSchemaFeatures(schema); len(findings) != 0 {
		t.Errorf("tool schema uses features the router validator does not implement: %v", findings)
	}
	if properties["version_below"].(map[string]any)["pattern"] != `^[0-9]{1,9}(\.[0-9]{1,9}){0,2}$` {
		t.Errorf("version_below pattern = %v, want the OpenAPI digit bound", properties["version_below"].(map[string]any)["pattern"])
	}
	// Length caps are handler-enforced (maxFilterLength 128 for runtime and
	// source_kind, maxRepoIDLength 512 per repo_ids entry); the schema keeps
	// the "At most N characters" descriptions but no maxLength keyword, which
	// the validator does not implement.
}

func TestToolDescriptionTellsTheModelToReadPartialReasons(t *testing.T) {
	t.Parallel()

	description := Tools()[0].Description
	for _, want := range []string{"partial_reasons", "scope_dir", "excluded_from_comparison", "next_cursor", "needs runtime"} {
		if !strings.Contains(description, want) {
			t.Errorf("description does not mention %q", want)
		}
	}
}
