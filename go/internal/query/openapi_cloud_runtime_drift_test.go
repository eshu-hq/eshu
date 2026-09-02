// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesCloudRuntimeDriftFindings(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/cloud/runtime-drift/findings")
	post := querytestutil.MustMapField(t, path, "post")
	if got, want := post["operationId"], "listCloudRuntimeDriftFindings"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	requestBody := querytestutil.MustMapField(t, post, "requestBody")
	requestContent := querytestutil.MustMapField(t, requestBody, "content")
	requestJSON := querytestutil.MustMapField(t, requestContent, "application/json")
	requestSchema := querytestutil.MustMapField(t, requestJSON, "schema")
	requestProperties := querytestutil.MustMapField(t, requestSchema, "properties")
	for _, field := range []string{"scope_id", "account_id", "project_id", "subscription_id", "provider", "cloud_resource_uid", "finding_kinds", "limit", "offset"} {
		if _, ok := requestProperties[field]; !ok {
			t.Fatalf("cloud runtime drift request schema missing %q", field)
		}
	}

	responses := querytestutil.MustMapField(t, post, "responses")
	ok := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, ok, "content")
	jsonContent := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, jsonContent, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	for _, field := range []string{"drift_findings", "source_state_groups", "findings_count", "total_findings_count", "truncated", "next_offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("cloud runtime drift response schema missing %q", field)
		}
	}
}
