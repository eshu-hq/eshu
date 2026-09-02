// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIRepositoryLanguageDocumentsCoverageFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	byLanguage := querytestutil.MustMapField(t, paths, "/api/v0/repositories/by-language")
	byLanguageGet := querytestutil.MustMapField(t, byLanguage, "get")
	byLanguageResponses := querytestutil.MustMapField(t, byLanguageGet, "responses")
	if got, want := querytestutil.MustMapField(t, byLanguageResponses, "503")["$ref"], "#/components/responses/ServiceUnavailable"; got != want {
		t.Fatalf("by-language 503 ref = %#v, want %#v", got, want)
	}

	okResponse := querytestutil.MustMapField(t, byLanguageResponses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	properties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, content, "schema"), "properties")
	repositories := querytestutil.MustMapField(t, properties, "repositories")
	items := querytestutil.MustMapField(t, repositories, "items")
	allOf, ok := items["allOf"].([]any)
	if !ok || len(allOf) != 2 {
		t.Fatalf("repositories.items.allOf = %#v, want Repository plus coverage extension", items["allOf"])
	}
	extension, ok := allOf[1].(map[string]any)
	if !ok {
		t.Fatalf("coverage extension type = %T, want map[string]any", allOf[1])
	}
	extensionProperties := querytestutil.MustMapField(t, extension, "properties")
	for _, field := range []string{"file_count", "languages", "last_indexed_at"} {
		if _, ok := extensionProperties[field]; !ok {
			t.Fatalf("repositories item schema missing %s", field)
		}
	}

	inventory := querytestutil.MustMapField(t, paths, "/api/v0/repositories/language-inventory")
	inventoryResponses := querytestutil.MustMapField(t, querytestutil.MustMapField(t, inventory, "get"), "responses")
	if got, want := querytestutil.MustMapField(t, inventoryResponses, "503")["$ref"], "#/components/responses/ServiceUnavailable"; got != want {
		t.Fatalf("language-inventory 503 ref = %#v, want %#v", got, want)
	}
}
