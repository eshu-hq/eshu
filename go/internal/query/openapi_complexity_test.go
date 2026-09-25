// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecIncludesComplexityAmbiguityContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	complexityPath := testutil.MustMapField(t, paths, "/api/v0/code/complexity")
	complexityPost := testutil.MustMapField(t, complexityPath, "post")
	complexityBody := testutil.MustMapField(t, testutil.MustMapField(t, complexityPost, "requestBody"), "content")
	complexityJSON := testutil.MustMapField(t, complexityBody, "application/json")
	complexitySchema := testutil.MustMapField(t, testutil.MustMapField(t, complexityJSON, "schema"), "properties")
	for _, field := range []string{"entity_id", "function_name", "repo_id", "limit"} {
		if _, ok := complexitySchema[field]; !ok {
			t.Fatalf("code/complexity request schema missing %s", field)
		}
	}
	complexityResponses := testutil.MustMapField(t, complexityPost, "responses")
	if _, ok := complexityResponses["409"]; !ok {
		t.Fatal("code/complexity responses missing 409 ambiguity response")
	}
}
