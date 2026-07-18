// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPICatalogDistinguishesWorkloadTruncation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := openAPIObject(t, spec, "paths")
	catalog := openAPIObject(t, paths, "/api/v0/catalog")
	get := openAPIObject(t, catalog, "get")
	responses := openAPIObject(t, get, "responses")
	ok := openAPIObject(t, responses, "200")
	content := openAPIObject(t, ok, "content")
	jsonContent := openAPIObject(t, content, "application/json")
	schema := openAPIObject(t, jsonContent, "schema")
	properties := openAPIObject(t, schema, "properties")
	workloadsTruncated := openAPIObject(t, properties, "workloads_truncated")
	if got, want := workloadsTruncated["type"], "boolean"; got != want {
		t.Fatalf("workloads_truncated.type = %#v, want %#v", got, want)
	}
}

func openAPIObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPISpec() %q type = %T, want object", key, parent[key])
	}
	return value
}
