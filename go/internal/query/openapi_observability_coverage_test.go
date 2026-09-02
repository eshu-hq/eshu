// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesObservabilityCoverageCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/observability/coverage/correlations")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listObservabilityCoverageCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	parametersByName := make(map[string]map[string]any, len(parameters))
	for _, parameter := range parameters {
		typed, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		name, ok := typed["name"].(string)
		if !ok {
			t.Fatalf("parameter name = %T, want string", typed["name"])
		}
		parametersByName[name] = typed
	}
	for _, want := range []string{"source_class", "resource_class"} {
		if _, ok := parametersByName[want]; !ok {
			t.Fatalf("parameters missing %q", want)
		}
	}
	sourceClassSchema := querytestutil.MustMapField(t, parametersByName["source_class"], "schema")
	sourceClassEnum, ok := sourceClassSchema["enum"].([]any)
	if !ok {
		t.Fatalf("source_class enum = %T, want []any", sourceClassSchema["enum"])
	}
	if !containsValue(sourceClassEnum, "declared") || !containsValue(sourceClassEnum, "mixed") {
		t.Fatalf("source_class enum = %#v, want declared and mixed", sourceClassEnum)
	}
	outcomeSchema := querytestutil.MustMapField(t, parametersByName["outcome"], "schema")
	outcomeEnum, ok := outcomeSchema["enum"].([]any)
	if !ok {
		t.Fatalf("outcome enum = %T, want []any", outcomeSchema["enum"])
	}
	if !containsValue(outcomeEnum, "drifted") || !containsValue(outcomeEnum, "permission_hidden") {
		t.Fatalf("outcome enum = %#v, want drifted and permission_hidden", outcomeEnum)
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := querytestutil.MustMapField(t, content, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	correlations := querytestutil.MustMapField(t, properties, "correlations")
	items := querytestutil.MustMapField(t, correlations, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")
	if got, want := querytestutil.MustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	if got, want := querytestutil.MustMapField(t, itemProperties, "coverage_status")["type"], "string"; got != want {
		t.Fatalf("coverage_status type = %#v, want %#v", got, want)
	}
	for _, want := range []string{"source_class", "source_classes", "resource_class", "freshness_state"} {
		if _, ok := itemProperties[want]; !ok {
			t.Fatalf("response properties missing %q", want)
		}
	}
}
