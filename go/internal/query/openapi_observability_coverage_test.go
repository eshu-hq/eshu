// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesObservabilityCoverageCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/observability/coverage/correlations")
	get := mustMapField(t, path, "get")
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
	sourceClassSchema := mustMapField(t, parametersByName["source_class"], "schema")
	sourceClassEnum, ok := sourceClassSchema["enum"].([]any)
	if !ok {
		t.Fatalf("source_class enum = %T, want []any", sourceClassSchema["enum"])
	}
	if !containsValue(sourceClassEnum, "declared") || !containsValue(sourceClassEnum, "mixed") {
		t.Fatalf("source_class enum = %#v, want declared and mixed", sourceClassEnum)
	}
	outcomeSchema := mustMapField(t, parametersByName["outcome"], "schema")
	outcomeEnum, ok := outcomeSchema["enum"].([]any)
	if !ok {
		t.Fatalf("outcome enum = %T, want []any", outcomeSchema["enum"])
	}
	if !containsValue(outcomeEnum, "drifted") || !containsValue(outcomeEnum, "permission_hidden") {
		t.Fatalf("outcome enum = %#v, want drifted and permission_hidden", outcomeEnum)
	}
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, content, "schema")
	properties := mustMapField(t, schema, "properties")
	correlations := mustMapField(t, properties, "correlations")
	items := mustMapField(t, correlations, "items")
	itemProperties := mustMapField(t, items, "properties")
	if got, want := mustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	if got, want := mustMapField(t, itemProperties, "coverage_status")["type"], "string"; got != want {
		t.Fatalf("coverage_status type = %#v, want %#v", got, want)
	}
	for _, want := range []string{"source_class", "source_classes", "resource_class", "freshness_state"} {
		if _, ok := itemProperties[want]; !ok {
			t.Fatalf("response properties missing %q", want)
		}
	}
}
