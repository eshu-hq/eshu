// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIRepositoryLanguageDocumentsCoverageFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	byLanguage := mustMapField(t, paths, "/api/v0/repositories/by-language")
	byLanguageGet := mustMapField(t, byLanguage, "get")
	byLanguageResponses := mustMapField(t, byLanguageGet, "responses")
	if got, want := mustMapField(t, byLanguageResponses, "503")["$ref"], "#/components/responses/ServiceUnavailable"; got != want {
		t.Fatalf("by-language 503 ref = %#v, want %#v", got, want)
	}

	okResponse := mustMapField(t, byLanguageResponses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	properties := mustMapField(t, mustMapField(t, content, "schema"), "properties")
	repositories := mustMapField(t, properties, "repositories")
	items := mustMapField(t, repositories, "items")
	allOf, ok := items["allOf"].([]any)
	if !ok || len(allOf) != 2 {
		t.Fatalf("repositories.items.allOf = %#v, want Repository plus coverage extension", items["allOf"])
	}
	extension, ok := allOf[1].(map[string]any)
	if !ok {
		t.Fatalf("coverage extension type = %T, want map[string]any", allOf[1])
	}
	extensionProperties := mustMapField(t, extension, "properties")
	for _, field := range []string{"file_count", "languages", "last_indexed_at"} {
		if _, ok := extensionProperties[field]; !ok {
			t.Fatalf("repositories item schema missing %s", field)
		}
	}

	inventory := mustMapField(t, paths, "/api/v0/repositories/language-inventory")
	inventoryResponses := mustMapField(t, mustMapField(t, inventory, "get"), "responses")
	if got, want := mustMapField(t, inventoryResponses, "503")["$ref"], "#/components/responses/ServiceUnavailable"; got != want {
		t.Fatalf("language-inventory 503 ref = %#v, want %#v", got, want)
	}
}
