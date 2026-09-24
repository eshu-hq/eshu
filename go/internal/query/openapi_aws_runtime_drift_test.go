// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecIncludesAWSRuntimeDriftFindings(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/aws/runtime-drift/findings")
	post := testutil.MustMapField(t, path, "post")
	if got, want := post["operationId"], "listAWSRuntimeDriftFindings"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := testutil.MustMapField(t, post, "responses")
	ok := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, ok, "content")
	jsonContent := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, jsonContent, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	if _, ok := properties["drift_findings"]; !ok {
		t.Fatal("aws runtime drift response schema missing drift_findings")
	}
	if _, ok := properties["outcome_groups"]; !ok {
		t.Fatal("aws runtime drift response schema missing outcome_groups")
	}
}
