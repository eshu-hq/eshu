// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestOpenAPIWorkloadContextSchemaDeclaresTruncationFlags is a PR #5933
// review fix (Copilot, openapi_components_workload_session.go:37).
// GET /api/v0/workloads/{workload_id}/context and
// GET /api/v0/services/{service_name}/context both $ref the WorkloadContext
// schema and return dependents_truncated, consumer_repositories_truncated,
// and provisioning_source_chains_truncated at the top level
// (service_query_enrichment.go sets them directly on the workload context map
// WriteSuccess serializes). The schema had drifted from that wire payload by
// not declaring any of the three. Kept in its own file rather than
// openapi_test.go, which already sits at the repository's 500-line cap.
func TestOpenAPIWorkloadContextSchemaDeclaresTruncationFlags(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	components := querytestutil.MustMapField(t, spec, "components")
	schemas := querytestutil.MustMapField(t, components, "schemas")
	workloadContextSchema := querytestutil.MustMapField(t, schemas, "WorkloadContext")
	workloadContextProperties := querytestutil.MustMapField(t, workloadContextSchema, "properties")

	for _, field := range []string{
		"dependents_truncated",
		"consumer_repositories_truncated",
		"provisioning_source_chains_truncated",
	} {
		property, ok := workloadContextProperties[field].(map[string]any)
		if !ok {
			t.Fatalf("WorkloadContext schema missing %s", field)
		}
		if got, want := property["type"], "boolean"; got != want {
			t.Fatalf("WorkloadContext schema %s[type] = %#v, want %#v", field, got, want)
		}
	}
}
