// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesComplexityAmbiguityContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	complexityPath := querytestutil.MustMapField(t, paths, "/api/v0/code/complexity")
	complexityPost := querytestutil.MustMapField(t, complexityPath, "post")
	complexityBody := querytestutil.MustMapField(t, querytestutil.MustMapField(t, complexityPost, "requestBody"), "content")
	complexityJSON := querytestutil.MustMapField(t, complexityBody, "application/json")
	complexitySchema := querytestutil.MustMapField(t, querytestutil.MustMapField(t, complexityJSON, "schema"), "properties")
	for _, field := range []string{"entity_id", "function_name", "repo_id", "limit"} {
		if _, ok := complexitySchema[field]; !ok {
			t.Fatalf("code/complexity request schema missing %s", field)
		}
	}
	complexityResponses := querytestutil.MustMapField(t, complexityPost, "responses")
	if _, ok := complexityResponses["409"]; !ok {
		t.Fatal("code/complexity responses missing 409 ambiguity response")
	}
}
