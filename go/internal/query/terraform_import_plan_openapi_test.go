// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAPITerraformImportPlanCandidatesIncludesFindingKinds stays in root
// (#6642 Part A) though the import-plan handler moved to iac/import_plan.go: it drives
// ServeOpenAPI/OpenAPISpec (openapi.go), a root-owned "openapi*" file this
// move must not edit, so a leaf package cannot reach it without an import
// cycle.
func TestOpenAPITerraformImportPlanCandidatesIncludesFindingKinds(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/openapi.json", nil)
	w := httptest.NewRecorder()

	ServeOpenAPI(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v0/iac/terraform-import-plan/candidates"].(map[string]any)
	post := path["post"].(map[string]any)
	responses := post["responses"].(map[string]any)
	okResponse := responses["200"].(map[string]any)
	content := okResponse["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	schema := jsonContent["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["finding_kinds"]; !ok {
		t.Fatal("terraform import-plan OpenAPI response schema missing finding_kinds")
	}
	candidatesSchema := properties["candidates"].(map[string]any)
	itemSchema := candidatesSchema["items"].(map[string]any)
	itemProps := itemSchema["properties"].(map[string]any)
	if _, ok := itemProps["config_shape_hint"]; !ok {
		t.Fatal("terraform import-plan candidate schema missing config_shape_hint")
	}
}
