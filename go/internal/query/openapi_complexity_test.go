// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesComplexityAmbiguityContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	complexityPath := mustMapField(t, paths, "/api/v0/code/complexity")
	complexityPost := mustMapField(t, complexityPath, "post")
	complexityBody := mustMapField(t, mustMapField(t, complexityPost, "requestBody"), "content")
	complexityJSON := mustMapField(t, complexityBody, "application/json")
	complexitySchema := mustMapField(t, mustMapField(t, complexityJSON, "schema"), "properties")
	for _, field := range []string{"entity_id", "function_name", "repo_id", "limit"} {
		if _, ok := complexitySchema[field]; !ok {
			t.Fatalf("code/complexity request schema missing %s", field)
		}
	}
	complexityResponses := mustMapField(t, complexityPost, "responses")
	if _, ok := complexityResponses["409"]; !ok {
		t.Fatal("code/complexity responses missing 409 ambiguity response")
	}
}
