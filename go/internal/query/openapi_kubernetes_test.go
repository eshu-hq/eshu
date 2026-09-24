// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecIncludesKubernetesCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/kubernetes/correlations")
	get := testutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listKubernetesCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := testutil.MustMapField(t, get, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, testutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := testutil.MustMapField(t, content, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	correlations := testutil.MustMapField(t, properties, "correlations")
	items := testutil.MustMapField(t, correlations, "items")
	itemProperties := testutil.MustMapField(t, items, "properties")
	if got, want := testutil.MustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	if got, want := testutil.MustMapField(t, itemProperties, "outcome")["type"], "string"; got != want {
		t.Fatalf("outcome type = %#v, want %#v", got, want)
	}
}
