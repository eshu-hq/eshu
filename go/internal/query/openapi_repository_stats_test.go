// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIRepositoryStatsDocumentsTimeoutMetadata(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	statsPath := mustMapField(t, paths, "/api/v0/repositories/{repo_id}/stats")
	statsGet := mustMapField(t, statsPath, "get")
	responses := mustMapField(t, statsGet, "responses")
	if _, ok := responses["504"]; !ok {
		t.Fatal("stats responses missing 504 timeout contract")
	}

	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	properties := mustMapField(t, mustMapField(t, content, "schema"), "properties")
	coverage := mustMapField(t, properties, "coverage")
	coverageProperties := mustMapField(t, coverage, "properties")
	for _, field := range []string{"partial_results", "truncated", "timeout", "timeout_budget", "missing_evidence"} {
		if _, ok := coverageProperties[field]; !ok {
			t.Fatalf("coverage schema missing %s", field)
		}
	}
	for _, field := range []string{"result_limits", "partial_reasons"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("stats schema missing additive %s block", field)
		}
	}
}

func TestOpenAPIRepositoryDocumentsGroupEvidenceFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	repository := mustMapField(t, schemas, "Repository")
	properties := mustMapField(t, repository, "properties")
	for _, field := range []string{"group_key", "group_source", "group_truth", "group_kind", "group_reason"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("Repository schema missing %s", field)
		}
	}

	// group_source must enumerate dependency_cluster so clients accept the new
	// primary grouping value added by issue #3504.
	groupSource := mustMapField(t, properties, "group_source")
	groupSourceEnums := enumStrings(t, groupSource, "group_source")
	for _, want := range []string{"dependency_cluster", "repository_dependency_flag", "repo_slug_namespace", "remote_url_owner", "missing_evidence"} {
		if !containsString(groupSourceEnums, want) {
			t.Errorf("group_source enum missing %q; got %v", want, groupSourceEnums)
		}
	}

	// group_kind must enumerate cluster so clients accept the cluster value
	// emitted when group_source is dependency_cluster.
	groupKind := mustMapField(t, properties, "group_kind")
	groupKindEnums := enumStrings(t, groupKind, "group_kind")
	for _, want := range []string{"cluster", "source", "dependency", "unknown"} {
		if !containsString(groupKindEnums, want) {
			t.Errorf("group_kind enum missing %q; got %v", want, groupKindEnums)
		}
	}
}

// enumStrings extracts the JSON Schema "enum" array from a property map as a
// slice of strings. It fails the test if the field is absent or not a []any of
// strings.
func enumStrings(t *testing.T, prop map[string]any, field string) []string {
	t.Helper()
	raw, ok := prop["enum"]
	if !ok {
		t.Fatalf("%s schema missing enum array", field)
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s enum is not an array, got %T", field, raw)
	}
	out := make([]string, 0, len(items))
	for _, v := range items {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s enum item is not a string: %v (%T)", field, v, v)
		}
		out = append(out, s)
	}
	return out
}
