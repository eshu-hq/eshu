// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIIncludesFreshnessGenerationsRoute(t *testing.T) {
	t.Parallel()

	spec := OpenAPISpec()
	for _, want := range []string{
		`"/api/v0/freshness/generations"`,
		`"operationId": "listGenerationLifecycle"`,
		`"queue_status"`,
		`"latest_failure"`,
		`"current_active_generation_id"`,
		`"truncated"`,
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("OpenAPISpec() missing %q", want)
		}
	}
}

func TestOpenAPISpecIsValidJSONWithFreshnessRoute(t *testing.T) {
	t.Parallel()

	var parsed map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &parsed); err != nil {
		t.Fatalf("OpenAPISpec() is not valid JSON: %v", err)
	}
	paths, ok := parsed["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPISpec() missing paths object")
	}
	if _, ok := paths["/api/v0/freshness/generations"]; !ok {
		t.Fatal("OpenAPISpec() paths missing /api/v0/freshness/generations")
	}
	if _, ok := paths["/api/v0/freshness/changed-since"]; !ok {
		t.Fatal("OpenAPISpec() paths missing /api/v0/freshness/changed-since")
	}
}

func TestOpenAPIIncludesChangedSinceRoute(t *testing.T) {
	t.Parallel()

	spec := OpenAPISpec()
	for _, want := range []string{
		`"/api/v0/freshness/changed-since"`,
		`"operationId": "summarizeChangedSince"`,
		`"since_generation_id"`,
		`"superseded"`,
		`"content_entities"`,
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("OpenAPISpec() missing %q", want)
		}
	}
}
