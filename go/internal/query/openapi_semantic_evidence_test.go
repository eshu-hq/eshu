// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesSemanticEvidenceRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	for _, path := range []string{
		"/api/v0/semantic/documentation-observations",
		"/api/v0/semantic/code-hints",
	} {
		item := mustMapField(t, paths, path)
		get := mustMapField(t, item, "get")
		parameters := mustSliceField(t, get, "parameters")
		for _, want := range []string{
			"provider_profile_id",
			"provider_kind",
			"prompt_version",
			"redaction_version",
			"redaction_state",
			"source_class",
			"policy_state",
			"freshness_state",
			"q",
			"updated_since",
			"limit",
			"cursor",
		} {
			if !openAPIParameterNamesContain(parameters, want) {
				t.Fatalf("%s parameters missing %q: %#v", path, want, parameters)
			}
		}
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	rowSchema := mustMapField(t, schemas, "SemanticEvidenceRow")
	properties := mustMapField(t, rowSchema, "properties")
	for _, want := range []string{
		"truth_basis",
		"provider_profile_id",
		"prompt_version",
		"redaction_version",
		"policy_state",
		"freshness_state",
	} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("SemanticEvidenceRow schema missing %q", want)
		}
	}
}

func openAPIParameterNamesContain(parameters []any, want string) bool {
	for _, raw := range parameters {
		param, _ := raw.(map[string]any)
		if name, _ := param["name"].(string); name == want {
			return true
		}
	}
	return false
}

func mustSliceField(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	value, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%q type = %T, want []any", key, parent[key])
	}
	return value
}
