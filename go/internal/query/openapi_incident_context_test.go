// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncidentContextExposesEvidenceSlots(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/incidents/{incident_id}/context")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "getIncidentContext"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	ok := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, ok, "content")
	jsonContent := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, jsonContent, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	for _, field := range []string{
		"incident",
		"timeline",
		"related_changes",
		"evidence_path",
		"missing_evidence",
		"ambiguous_evidence",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("incident context schema missing %s", field)
		}
	}
}
