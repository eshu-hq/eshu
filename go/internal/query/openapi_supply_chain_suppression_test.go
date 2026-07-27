// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesVulnerabilitySuppressionMutation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/suppressions")
	post := mustMapField(t, path, "post")
	if got, want := post["operationId"], "createVulnerabilitySuppression"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	if got, want := post["x-shared-key-only"], true; got != want {
		t.Fatalf("x-shared-key-only = %#v, want %#v", got, want)
	}

	requestBody := mustMapField(t, post, "requestBody")
	content := mustMapField(t, requestBody, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	required := mustStringSliceField(t, schema, "required")
	for _, want := range []string{"suppression_id", "justification", "authored_at", "reason", "scope"} {
		if !containsOpenAPIEnumString(required, want) {
			t.Fatalf("request required = %#v, want %q", required, want)
		}
	}
	properties := mustMapField(t, schema, "properties")
	if _, ok := properties["author"]; ok {
		t.Fatal("request schema exposes server-derived author")
	}
	if _, ok := properties["source"]; ok {
		t.Fatal("request schema exposes server-derived source")
	}
	justification := mustMapField(t, properties, "justification")
	justificationEnum := mustStringSliceField(t, justification, "enum")
	for _, want := range []string{"not_affected", "accepted_risk", "false_positive", "ignored"} {
		if !containsOpenAPIEnumString(justificationEnum, want) {
			t.Fatalf("justification enum = %#v, want %q", justificationEnum, want)
		}
	}

	responses := mustMapField(t, post, "responses")
	for _, status := range []string{"200", "201", "400", "403", "503"} {
		if _, ok := responses[status]; !ok {
			t.Fatalf("responses missing %q", status)
		}
	}
}
