// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncidentContextExposesEvidenceSlots(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/incidents/{incident_id}/context")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "getIncidentContext"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := mustMapField(t, get, "responses")
	ok := mustMapField(t, responses, "200")
	content := mustMapField(t, ok, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
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
