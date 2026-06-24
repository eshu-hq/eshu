// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecServiceIntelligenceReportExposesReportFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	reportPath := mustMapField(t, paths, "/api/v0/services/{service_name}/intelligence-report")
	reportGet := mustMapField(t, reportPath, "get")
	responses := mustMapField(t, reportGet, "responses")
	ok := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, ok, "content"), "application/json")
	schema := mustMapField(t, mustMapField(t, content, "schema"), "properties")

	for _, field := range []string{
		"schema",
		"subject",
		"supported",
		"partial",
		"truth_class",
		"sections",
		"limitations",
		"recommended_next_calls",
		"suggested_investigations",
	} {
		if _, ok := schema[field]; !ok {
			t.Fatalf("intelligence-report response schema missing %s", field)
		}
	}

	// The capability and ambiguity error contracts must be declared.
	for _, code := range []string{"404", "409", "501"} {
		if _, ok := responses[code]; !ok {
			t.Fatalf("intelligence-report response missing %s", code)
		}
	}
}
