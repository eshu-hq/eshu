// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesVulnerabilitySuppressionMutation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/suppressions")
	post := querytestutil.MustMapField(t, path, "post")
	if got, want := post["operationId"], "createVulnerabilitySuppression"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	if got, want := post["x-shared-key-only"], true; got != want {
		t.Fatalf("x-shared-key-only = %#v, want %#v", got, want)
	}

	requestBody := querytestutil.MustMapField(t, post, "requestBody")
	content := querytestutil.MustMapField(t, requestBody, "content")
	appJSON := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, appJSON, "schema")
	required := mustStringSliceField(t, schema, "required")
	for _, want := range []string{"suppression_id", "justification", "authored_at", "reason", "scope"} {
		if !containsOpenAPIEnumString(required, want) {
			t.Fatalf("request required = %#v, want %q", required, want)
		}
	}
	properties := querytestutil.MustMapField(t, schema, "properties")
	if _, ok := properties["author"]; ok {
		t.Fatal("request schema exposes server-derived author")
	}
	if _, ok := properties["source"]; ok {
		t.Fatal("request schema exposes server-derived source")
	}
	justification := querytestutil.MustMapField(t, properties, "justification")
	justificationEnum := mustStringSliceField(t, justification, "enum")
	for _, want := range []string{"not_affected", "accepted_risk", "false_positive", "ignored"} {
		if !containsOpenAPIEnumString(justificationEnum, want) {
			t.Fatalf("justification enum = %#v, want %q", justificationEnum, want)
		}
	}
	expiresAt := querytestutil.MustMapField(t, properties, "expires_at")
	if description, _ := expiresAt["description"].(string); !strings.Contains(description, "strictly later than authored_at") {
		t.Fatalf("expires_at description = %q, want strict authored_at ordering contract", description)
	}
	scope := querytestutil.MustMapField(t, properties, "scope")
	scopeDescription, _ := scope["description"].(string)
	for _, want := range []string{"discoverable identity anchor", "evidence_path", "environment", "workload_id", "service_id", "cannot stand alone"} {
		if !strings.Contains(scopeDescription, want) {
			t.Fatalf("scope description = %q, want %q", scopeDescription, want)
		}
	}
	scopeProperties := querytestutil.MustMapField(t, scope, "properties")
	for _, want := range []string{"environment", "workload_id", "service_id"} {
		property := querytestutil.MustMapField(t, scopeProperties, want)
		description, _ := property["description"].(string)
		if !strings.Contains(description, "narrows a suppression") {
			t.Fatalf("scope property %q description = %q, want narrowing-only contract", want, description)
		}
	}

	responses := querytestutil.MustMapField(t, post, "responses")
	for _, status := range []string{"200", "201", "400", "403", "503"} {
		if _, ok := responses[status]; !ok {
			t.Fatalf("responses missing %q", status)
		}
	}
}
