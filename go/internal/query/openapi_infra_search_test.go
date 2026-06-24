// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIInfraSearchAllowsStructuredFilterScope(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/openapi.json", nil)
	w := httptest.NewRecorder()

	ServeOpenAPI(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	searchPath := mustMapField(t, paths, "/api/v0/infra/resources/search")
	post := mustMapField(t, searchPath, "post")
	requestBody := mustMapField(t, post, "requestBody")
	content := mustMapField(t, requestBody, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")

	for _, field := range []string{
		"query",
		"kind",
		"category",
		"provider",
		"environment",
		"resource_service",
		"resource_category",
		"limit",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("infra search request schema missing property %q", field)
		}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, field := range required {
			if field == "query" {
				t.Fatalf("required = %#v, want query omitted for structured filter searches", required)
			}
		}
	}
}
