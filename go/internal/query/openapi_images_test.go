// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesContainerImageList(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/images")
	get := querytestutil.MustMapField(t, path, "get")
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
	limitSchema := querytestutil.MustMapField(t, byName["limit"], "schema")
	if got, want := limitSchema["maximum"], float64(200); got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
	if got, want := limitSchema["default"], float64(50); got != want {
		t.Fatalf("limit default = %#v, want %#v", got, want)
	}

	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := querytestutil.MustMapField(t, content, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	images := querytestutil.MustMapField(t, properties, "images")
	items := querytestutil.MustMapField(t, images, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")
	for _, want := range []string{"id", "digest", "repository_id", "registry", "repository", "tag"} {
		if _, ok := itemProperties[want]; !ok {
			t.Fatalf("image item properties missing %q", want)
		}
	}
}
