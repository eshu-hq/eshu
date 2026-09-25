// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPIRepositoryLanguageDocumentsCoverageFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	byLanguage := testutil.MustMapField(t, paths, "/api/v0/repositories/by-language")
	byLanguageGet := testutil.MustMapField(t, byLanguage, "get")
	byLanguageResponses := testutil.MustMapField(t, byLanguageGet, "responses")
	if got, want := testutil.MustMapField(t, byLanguageResponses, "503")["$ref"], "#/components/responses/ServiceUnavailable"; got != want {
		t.Fatalf("by-language 503 ref = %#v, want %#v", got, want)
	}

	okResponse := testutil.MustMapField(t, byLanguageResponses, "200")
	content := testutil.MustMapField(t, testutil.MustMapField(t, okResponse, "content"), "application/json")
	properties := testutil.MustMapField(t, testutil.MustMapField(t, content, "schema"), "properties")
	repositories := testutil.MustMapField(t, properties, "repositories")
	items := testutil.MustMapField(t, repositories, "items")
	allOf, ok := items["allOf"].([]any)
	if !ok || len(allOf) != 2 {
		t.Fatalf("repositories.items.allOf = %#v, want Repository plus coverage extension", items["allOf"])
	}
	extension, ok := allOf[1].(map[string]any)
	if !ok {
		t.Fatalf("coverage extension type = %T, want map[string]any", allOf[1])
	}
	extensionProperties := testutil.MustMapField(t, extension, "properties")
	for _, field := range []string{"file_count", "languages", "last_indexed_at"} {
		if _, ok := extensionProperties[field]; !ok {
			t.Fatalf("repositories item schema missing %s", field)
		}
	}

	inventory := testutil.MustMapField(t, paths, "/api/v0/repositories/language-inventory")
	inventoryResponses := testutil.MustMapField(t, testutil.MustMapField(t, inventory, "get"), "responses")
	if got, want := testutil.MustMapField(t, inventoryResponses, "503")["$ref"], "#/components/responses/ServiceUnavailable"; got != want {
		t.Fatalf("language-inventory 503 ref = %#v, want %#v", got, want)
	}
}
