// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesReplatformingSelectors(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/replatforming/selectors")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listReplatformingSelectors"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	if got, want := get["x-scoped-token-support"], true; got != want {
		t.Fatalf("x-scoped-token-support = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok || len(parameters) != 1 {
		t.Fatalf("parameters = %#v, want one bounded limit parameter", get["parameters"])
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, okResponse, "content")
	jsonContent := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, jsonContent, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	for _, field := range []string{
		"scopes",
		"count",
		"empty_scope_count",
		"supported_scope_kinds",
		"finding_kinds",
		"page_sizes",
		"readiness",
		"truncated",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("replatforming selectors response schema missing %s", field)
		}
	}
}
