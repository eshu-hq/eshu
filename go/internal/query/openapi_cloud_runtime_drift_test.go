// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesCloudRuntimeDriftFindings(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/cloud/runtime-drift/findings")
	post := mustMapField(t, path, "post")
	if got, want := post["operationId"], "listCloudRuntimeDriftFindings"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	requestBody := mustMapField(t, post, "requestBody")
	requestContent := mustMapField(t, requestBody, "content")
	requestJSON := mustMapField(t, requestContent, "application/json")
	requestSchema := mustMapField(t, requestJSON, "schema")
	requestProperties := mustMapField(t, requestSchema, "properties")
	for _, field := range []string{"scope_id", "account_id", "project_id", "subscription_id", "provider", "cloud_resource_uid", "finding_kinds", "limit", "offset"} {
		if _, ok := requestProperties[field]; !ok {
			t.Fatalf("cloud runtime drift request schema missing %q", field)
		}
	}

	responses := mustMapField(t, post, "responses")
	ok := mustMapField(t, responses, "200")
	content := mustMapField(t, ok, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	for _, field := range []string{"drift_findings", "source_state_groups", "findings_count", "total_findings_count", "truncated", "next_offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("cloud runtime drift response schema missing %q", field)
		}
	}
}
