// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesContainerImageList(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/images")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listContainerImages"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}

	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	byName := make(map[string]map[string]any, len(parameters))
	for _, parameter := range parameters {
		typed := parameter.(map[string]any)
		byName[typed["name"].(string)] = typed
	}
	for _, want := range []string{"digest", "repository_id", "tag", "limit", "offset"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("parameters missing %q", want)
		}
	}
	limitSchema := mustMapField(t, byName["limit"], "schema")
	if got, want := limitSchema["maximum"], float64(200); got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
	if got, want := limitSchema["default"], float64(50); got != want {
		t.Fatalf("limit default = %#v, want %#v", got, want)
	}

	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, content, "schema")
	properties := mustMapField(t, schema, "properties")
	images := mustMapField(t, properties, "images")
	items := mustMapField(t, images, "items")
	itemProperties := mustMapField(t, items, "properties")
	for _, want := range []string{"id", "digest", "repository_id", "registry", "repository", "tag"} {
		if _, ok := itemProperties[want]; !ok {
			t.Fatalf("image item properties missing %q", want)
		}
	}
}
