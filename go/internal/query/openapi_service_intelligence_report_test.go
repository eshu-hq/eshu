// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecServiceIntelligenceReportExposesReportFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	reportPath := querytestutil.MustMapField(t, paths, "/api/v0/services/{service_name}/intelligence-report")
	reportGet := querytestutil.MustMapField(t, reportPath, "get")
	responses := querytestutil.MustMapField(t, reportGet, "responses")
	ok := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, ok, "content"), "application/json")
	schema := querytestutil.MustMapField(t, querytestutil.MustMapField(t, content, "schema"), "properties")

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
