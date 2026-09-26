// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestServeOpenAPI(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v0/openapi.json", nil)
	w := httptest.NewRecorder()

	ServeOpenAPI(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type application/json; charset=utf-8, got %s", contentType)
	}

	// Verify it's valid JSON
	var spec map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Verify required OpenAPI fields
	if spec["openapi"] != "3.0.3" {
		t.Errorf("expected openapi version 3.0.3, got %v", spec["openapi"])
	}

	info, ok := spec["info"].(map[string]interface{})
	if !ok {
		t.Fatal("info field missing or invalid")
	}

	if info["title"] != "Eshu API" {
		t.Errorf("unexpected title: %v", info["title"])
	}
	if got, want := info["version"], buildinfo.AppVersion(); got != want {
		t.Fatalf("expected info.version %v, got %v", want, got)
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("paths field missing or invalid")
	}

	// Verify some key endpoints exist
	expectedPaths := []string{
		"/health",
		"/api/v0/repositories",
		"/api/v0/repositories/by-language",
		"/api/v0/repositories/language-inventory",
		"/api/v0/repositories/{repo_id}/context",
		"/api/v0/entities/resolve",
		"/api/v0/evidence/relationships/{resolved_id}",
		"/api/v0/documentation/findings",
		"/api/v0/documentation/findings/{finding_id}/evidence-packet",
		"/api/v0/documentation/evidence-packets/{packet_id}/freshness",
		"/api/v0/package-registry/packages",
		"/api/v0/package-registry/versions",
		"/api/v0/package-registry/dependencies",
		"/api/v0/code/search",
		"/api/v0/code/bundles",
		"/api/v0/code/dead-code/investigate",
		"/api/v0/code/call-chain",
		"/api/v0/code/language-query",
		"/api/v0/content/files/read",
		"/api/v0/infra/resources/search",
		"/api/v0/iac/dead",
		"/api/v0/iac/unmanaged-resources",
		"/api/v0/iac/terraform-import-plan/candidates",
		"/api/v0/replatforming/plans",
		"/api/v0/impact/trace-deployment-chain",
		"/api/v0/impact/blast-radius",
		"/api/v0/impact/change-surface/investigate",
		"/api/v0/status/pipeline",
		"/api/v0/compare/environments",
		"/api/v0/ask",
		"/api/v0/openapi.json",
	}

	for _, path := range expectedPaths {
		if _, exists := paths[path]; !exists {
			t.Errorf("expected path %s not found in spec", path)
		}
	}

	// Verify components section exists
	components, ok := spec["components"].(map[string]interface{})
	if !ok {
		t.Fatal("components field missing or invalid")
	}

	schemas, ok := components["schemas"].(map[string]interface{})
	if !ok {
		t.Fatal("components.schemas missing or invalid")
	}

	// Verify some key schemas
	expectedSchemas := []string{"Repository", "EntityRef", "ErrorResponse", "Relationship"}
	for _, schema := range expectedSchemas {
		if _, exists := schemas[schema]; !exists {
			t.Errorf("expected schema %s not found", schema)
		}
	}

	repositoryContextPath := testutil.MustMapField(t, paths, "/api/v0/repositories/{repo_id}/context")
	repositoryContextGet := testutil.MustMapField(t, repositoryContextPath, "get")
	repositoryContextResponses := testutil.MustMapField(t, repositoryContextGet, "responses")
	repositoryContextOK := testutil.MustMapField(t, repositoryContextResponses, "200")
	repositoryContextContent := testutil.MustMapField(t, testutil.MustMapField(t, repositoryContextOK, "content"), "application/json")
	repositoryContextSchema := testutil.MustMapField(t, testutil.MustMapField(t, repositoryContextContent, "schema"), "properties")
	for _, field := range []string{
		"relationships",
		"relationship_overview",
		"api_surface",
		"consumers",
	} {
		if _, ok := repositoryContextSchema[field]; !ok {
			t.Fatalf("repositories/{repo_id}/context response schema missing %s", field)
		}
	}
}

func TestOpenAPIAskSSEDescribesValidatedTokenEvents(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v0/openapi.json", nil)
	w := httptest.NewRecorder()

	ServeOpenAPI(w, req)

	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	askPath := testutil.MustMapField(t, paths, "/api/v0/ask")
	askPost := testutil.MustMapField(t, askPath, "post")
	responses := testutil.MustMapField(t, askPost, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")

	responseDescription, ok := okResponse["description"].(string)
	if !ok {
		t.Fatal("ask 200 response description missing or not a string")
	}
	assertAskSSEValidatedTokenDescription(t, responseDescription)

	content := testutil.MustMapField(t, okResponse, "content")
	eventStream := testutil.MustMapField(t, content, "text/event-stream")
	eventStreamSchema := testutil.MustMapField(t, eventStream, "schema")
	eventStreamDescription, ok := eventStreamSchema["description"].(string)
	if !ok {
		t.Fatal("ask SSE schema description missing or not a string")
	}
	assertAskSSEValidatedTokenDescription(t, eventStreamDescription)
}

func TestOpenAPIAskDescribesRuntimeAnswerGuardrails(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	askPath := testutil.MustMapField(t, paths, "/api/v0/ask")
	askPost := testutil.MustMapField(t, askPath, "post")
	responses := testutil.MustMapField(t, askPost, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	responseDescription, ok := okResponse["description"].(string)
	if !ok {
		t.Fatal("ask 200 response description missing or not a string")
	}
	for _, required := range []string{
		"runtime citation-coverage and publish-safety guardrails",
		"suppress those fields",
		"partial=true",
	} {
		if !strings.Contains(responseDescription, required) {
			t.Fatalf("ask 200 response description missing %q: %s", required, responseDescription)
		}
	}
}

func assertAskSSEValidatedTokenDescription(t *testing.T, description string) {
	t.Helper()
	for _, forbidden := range []string{
		"per-provider-token",
		"provider text-token delta carrying assistant prose",
	} {
		if strings.Contains(description, forbidden) {
			t.Fatalf("Ask SSE OpenAPI description still contains stale provider-token wording %q: %s", forbidden, description)
		}
	}
	for _, required := range []string{
		"validated narration prose",
		"Raw provider text-token deltas are never emitted",
	} {
		if !strings.Contains(description, required) {
			t.Fatalf("Ask SSE OpenAPI description missing %q: %s", required, description)
		}
	}
}

func TestAPIRouter_OpenAPIEndpoint(t *testing.T) {
	router := &APIRouter{}
	mux := http.NewServeMux()
	router.Mount(mux)

	req := httptest.NewRequest("GET", "/api/v0/openapi.json", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if spec["openapi"] != "3.0.3" {
		t.Errorf("expected openapi version 3.0.3, got %v", spec["openapi"])
	}
}

func TestAPIRouter_OpenAPIDocumentationEndpoints(t *testing.T) {
	router := &APIRouter{}
	mux := http.NewServeMux()
	router.Mount(mux)

	tests := []struct {
		name     string
		path     string
		contains []string
	}{
		{
			name: "swagger ui",
			path: "/api/v0/docs",
			contains: []string{
				"Swagger UI",
				"/api/v0/openapi.json",
				"swagger-ui",
			},
		},
		{
			name: "redoc",
			path: "/api/v0/redoc",
			contains: []string{
				"ReDoc",
				"/api/v0/openapi.json",
				"redoc",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d", tt.path, w.Code, http.StatusOK)
			}
			if got, want := w.Header().Get("Content-Type"), "text/html; charset=utf-8"; got != want {
				t.Fatalf("GET %s Content-Type = %q, want %q", tt.path, got, want)
			}
			body := w.Body.String()
			for _, want := range tt.contains {
				if !strings.Contains(body, want) {
					t.Fatalf("GET %s body missing %q", tt.path, want)
				}
			}
		})
	}
}

func TestOpenAPISpec_ContentEntitySchemasExposeMetadata(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	readPath := testutil.MustMapField(t, paths, "/api/v0/content/entities/read")
	readPost := testutil.MustMapField(t, readPath, "post")
	readResponses := testutil.MustMapField(t, readPost, "responses")
	readOK := testutil.MustMapField(t, readResponses, "200")
	readContent := testutil.MustMapField(t, testutil.MustMapField(t, readOK, "content"), "application/json")
	readSchema := testutil.MustMapField(t, readContent, "schema")
	if got, want := readSchema["$ref"], "#/components/schemas/EntityContent"; got != want {
		t.Fatalf("content/entities/read schema ref = %#v, want %#v", got, want)
	}

	searchPath := testutil.MustMapField(t, paths, "/api/v0/content/entities/search")
	searchPost := testutil.MustMapField(t, searchPath, "post")
	searchRequestBody := testutil.MustMapField(t, searchPost, "requestBody")
	searchRequestContent := testutil.MustMapField(t, testutil.MustMapField(t, searchRequestBody, "content"), "application/json")
	searchRequestSchema := testutil.MustMapField(t, searchRequestContent, "schema")
	searchRequestProperties := testutil.MustMapField(t, searchRequestSchema, "properties")
	if _, ok := searchRequestProperties["repo_ids"]; !ok {
		t.Fatal("content/entities/search schema missing repo_ids property")
	}
	if _, ok := searchRequestProperties["pattern"]; !ok {
		t.Fatal("content/entities/search schema missing pattern property")
	}
	offsetSchema := testutil.MustMapField(t, searchRequestProperties, "offset")
	if got, want := int(offsetSchema["maximum"].(float64)), ContentSearchMaxOffset; got != want {
		t.Fatalf("content/entities/search offset maximum = %d, want %d", got, want)
	}
	searchRequestRequirements, ok := searchRequestSchema["anyOf"].([]any)
	if !ok || len(searchRequestRequirements) != 2 {
		t.Fatalf("content/entities/search schema anyOf = %#v, want 2 pattern requirement variants", searchRequestSchema["anyOf"])
	}

	searchResponses := testutil.MustMapField(t, searchPost, "responses")
	searchOK := testutil.MustMapField(t, searchResponses, "200")
	searchContent := testutil.MustMapField(t, testutil.MustMapField(t, searchOK, "content"), "application/json")
	searchSchema := testutil.MustMapField(t, searchContent, "schema")
	if got, want := searchSchema["$ref"], "#/components/schemas/EntityContentSearchResponse"; got != want {
		t.Fatalf("content/entities/search schema ref = %#v, want %#v", got, want)
	}

	components := testutil.MustMapField(t, spec, "components")
	schemas := testutil.MustMapField(t, components, "schemas")
	entitySearchSchema := testutil.MustMapField(t, schemas, "EntityContentSearchResponse")
	entitySearchProperties := testutil.MustMapField(t, entitySearchSchema, "properties")
	for _, property := range []string{"results", "count", "limit", "offset", "truncated", "source_backend"} {
		if _, ok := entitySearchProperties[property]; !ok {
			t.Fatalf("EntityContentSearchResponse missing property %q", property)
		}
	}
	entitySchema := testutil.MustMapField(t, schemas, "EntityContent")
	entityProperties := testutil.MustMapField(t, entitySchema, "properties")
	metadata := testutil.MustMapField(t, entityProperties, "metadata")
	if got, want := metadata["type"], "object"; got != want {
		t.Fatalf("EntityContent.metadata.type = %#v, want %#v", got, want)
	}

	entityRefSchema := testutil.MustMapField(t, schemas, "EntityRef")
	entityRefProperties := testutil.MustMapField(t, entityRefSchema, "properties")
	entityRefSemanticSummary := testutil.MustMapField(t, entityRefProperties, "semantic_summary")
	if got, want := entityRefSemanticSummary["type"], "string"; got != want {
		t.Fatalf("EntityRef.semantic_summary.type = %#v, want %#v", got, want)
	}
	entityRefSemanticProfile := testutil.MustMapField(t, entityRefProperties, "semantic_profile")
	if got, want := entityRefSemanticProfile["type"], "object"; got != want {
		t.Fatalf("EntityRef.semantic_profile.type = %#v, want %#v", got, want)
	}
	entityRefMetadata := testutil.MustMapField(t, entityRefProperties, "metadata")
	if got, want := entityRefMetadata["type"], "object"; got != want {
		t.Fatalf("EntityRef.metadata.type = %#v, want %#v", got, want)
	}

	entityContextPath := testutil.MustMapField(t, paths, "/api/v0/entities/{entity_id}/context")
	entityContextGet := testutil.MustMapField(t, entityContextPath, "get")
	entityContextResponses := testutil.MustMapField(t, entityContextGet, "responses")
	entityContextOK := testutil.MustMapField(t, entityContextResponses, "200")
	entityContextContent := testutil.MustMapField(t, testutil.MustMapField(t, entityContextOK, "content"), "application/json")
	entityContextSchema := testutil.MustMapField(t, entityContextContent, "schema")
	entityContextProperties := testutil.MustMapField(t, entityContextSchema, "properties")
	entityContextMetadata := testutil.MustMapField(t, entityContextProperties, "metadata")
	if got, want := entityContextMetadata["type"], "object"; got != want {
		t.Fatalf("entity context metadata.type = %#v, want %#v", got, want)
	}
	entityContextSemanticProfile := testutil.MustMapField(t, entityContextProperties, "semantic_profile")
	if got, want := entityContextSemanticProfile["type"], "object"; got != want {
		t.Fatalf("entity context semantic_profile.type = %#v, want %#v", got, want)
	}
	entityContextStory := testutil.MustMapField(t, entityContextProperties, "story")
	if got, want := entityContextStory["type"], "string"; got != want {
		t.Fatalf("entity context story.type = %#v, want %#v", got, want)
	}

	codeSearchPath := testutil.MustMapField(t, paths, "/api/v0/code/search")
	codeSearchPost := testutil.MustMapField(t, codeSearchPath, "post")
	codeSearchResponses := testutil.MustMapField(t, codeSearchPost, "responses")
	codeSearchOK := testutil.MustMapField(t, codeSearchResponses, "200")
	codeSearchContent := testutil.MustMapField(t, testutil.MustMapField(t, codeSearchOK, "content"), "application/json")
	codeSearchSchema := testutil.MustMapField(t, codeSearchContent, "schema")
	if got, want := codeSearchSchema["$ref"], "#/components/schemas/CodeSearchResponse"; got != want {
		t.Fatalf("code/search schema ref = %#v, want %#v", got, want)
	}
	codeSearchResultSchema := testutil.MustMapField(t, schemas, "CodeSearchResult")
	codeSearchResultProperties := testutil.MustMapField(t, codeSearchResultSchema, "properties")
	codeSearchSemanticProfile := testutil.MustMapField(t, codeSearchResultProperties, "semantic_profile")
	if got, want := codeSearchSemanticProfile["type"], "object"; got != want {
		t.Fatalf("CodeSearchResult.semantic_profile.type = %#v, want %#v", got, want)
	}
	// find_code, search_entity_content, and search_file_content all emit
	// search_backend=hybrid on content rows reordered by the bounded hybrid
	// re-rank, so every returned row type must document the marker.
	for _, schemaName := range []string{"CodeSearchResult", "EntityContent", "FileContent"} {
		properties := testutil.MustMapField(t, testutil.MustMapField(t, schemas, schemaName), "properties")
		searchBackend := testutil.MustMapField(t, properties, "search_backend")
		if got, want := searchBackend["type"], "string"; got != want {
			t.Fatalf("%s.search_backend.type = %#v, want %#v", schemaName, got, want)
		}
		searchBackendEnum, ok := searchBackend["enum"].([]any)
		if !ok || len(searchBackendEnum) != 1 || searchBackendEnum[0] != "hybrid" {
			t.Fatalf("%s.search_backend.enum = %#v, want [hybrid]", schemaName, searchBackend["enum"])
		}
	}

	symbolSearchPath := testutil.MustMapField(t, paths, "/api/v0/code/symbols/search")
	symbolSearchPost := testutil.MustMapField(t, symbolSearchPath, "post")
	symbolSearchBody := testutil.MustMapField(t, testutil.MustMapField(t, symbolSearchPost, "requestBody"), "content")
	symbolSearchJSON := testutil.MustMapField(t, symbolSearchBody, "application/json")
	symbolSearchRequestSchema := testutil.MustMapField(t, symbolSearchJSON, "schema")
	if _, ok := symbolSearchRequestSchema["required"]; ok {
		t.Fatal("symbol search request should not require only symbol when query alias is documented")
	}
	anyOf, ok := symbolSearchRequestSchema["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("symbol search request anyOf = %#v, want symbol/query alternatives", symbolSearchRequestSchema["anyOf"])
	}
	symbolSearchResponses := testutil.MustMapField(t, symbolSearchPost, "responses")
	symbolSearchOK := testutil.MustMapField(t, symbolSearchResponses, "200")
	symbolSearchContent := testutil.MustMapField(t, testutil.MustMapField(t, symbolSearchOK, "content"), "application/json")
	symbolSearchSchema := testutil.MustMapField(t, symbolSearchContent, "schema")
	if got, want := symbolSearchSchema["$ref"], "#/components/schemas/SymbolSearchResponse"; got != want {
		t.Fatalf("code/symbols/search schema ref = %#v, want %#v", got, want)
	}
	symbolSearchResultSchema := testutil.MustMapField(t, schemas, "SymbolSearchResult")
	symbolSearchResultProperties := testutil.MustMapField(t, symbolSearchResultSchema, "properties")
	if _, ok := symbolSearchResultProperties["source_handle"]; !ok {
		t.Fatal("SymbolSearchResult missing source_handle")
	}

	structuralInventoryPath := testutil.MustMapField(t, paths, "/api/v0/code/structure/inventory")
	structuralInventoryPost := testutil.MustMapField(t, structuralInventoryPath, "post")
	structuralInventoryBody := testutil.MustMapField(t, testutil.MustMapField(t, structuralInventoryPost, "requestBody"), "content")
	structuralInventoryJSON := testutil.MustMapField(t, structuralInventoryBody, "application/json")
	structuralInventoryRequestSchema := testutil.MustMapField(t, testutil.MustMapField(t, structuralInventoryJSON, "schema"), "properties")
	for _, field := range []string{"repo_id", "language", "inventory_kind", "entity_kind", "file_path", "symbol", "decorator", "method_name", "class_name", "limit", "offset"} {
		if _, ok := structuralInventoryRequestSchema[field]; !ok {
			t.Fatalf("code/structure/inventory request schema missing %s", field)
		}
	}
	structuralInventoryResponses := testutil.MustMapField(t, structuralInventoryPost, "responses")
	structuralInventoryOK := testutil.MustMapField(t, structuralInventoryResponses, "200")
	structuralInventoryContent := testutil.MustMapField(t, testutil.MustMapField(t, structuralInventoryOK, "content"), "application/json")
	structuralInventoryResponse := testutil.MustMapField(t, testutil.MustMapField(t, structuralInventoryContent, "schema"), "properties")
	for _, field := range []string{"results", "truncated", "next_offset", "source_backend"} {
		if _, ok := structuralInventoryResponse[field]; !ok {
			t.Fatalf("code/structure/inventory response schema missing %s", field)
		}
	}
	if _, ok := structuralInventoryResponse["matches"]; ok {
		t.Fatalf("code/structure/inventory response schema includes the removed matches alias (#7170)")
	}

	topicInvestigationPath := testutil.MustMapField(t, paths, "/api/v0/code/topics/investigate")
	topicInvestigationPost := testutil.MustMapField(t, topicInvestigationPath, "post")
	topicInvestigationBody := testutil.MustMapField(t, testutil.MustMapField(t, topicInvestigationPost, "requestBody"), "content")
	topicInvestigationJSON := testutil.MustMapField(t, topicInvestigationBody, "application/json")
	topicInvestigationRequest := testutil.MustMapField(t, testutil.MustMapField(t, topicInvestigationJSON, "schema"), "properties")
	for _, field := range []string{"topic", "intent", "repo_id", "language", "limit", "offset"} {
		if _, ok := topicInvestigationRequest[field]; !ok {
			t.Fatalf("code/topics/investigate request schema missing %s", field)
		}
	}
	topicInvestigationResponses := testutil.MustMapField(t, topicInvestigationPost, "responses")
	topicInvestigationOK := testutil.MustMapField(t, topicInvestigationResponses, "200")
	topicInvestigationContent := testutil.MustMapField(t, testutil.MustMapField(t, topicInvestigationOK, "content"), "application/json")
	topicInvestigationResponse := testutil.MustMapField(t, testutil.MustMapField(t, topicInvestigationContent, "schema"), "properties")
	for _, field := range []string{"evidence_groups", "matched_symbols", "call_graph_handles", "recommended_next_calls", "coverage", "truncated", "candidate_pool_truncated"} {
		if _, ok := topicInvestigationResponse[field]; !ok {
			t.Fatalf("code/topics/investigate response schema missing %s", field)
		}
	}

	relationshipStoryPath := testutil.MustMapField(t, paths, "/api/v0/code/relationships/story")
	relationshipStoryPost := testutil.MustMapField(t, relationshipStoryPath, "post")
	relationshipStoryBody := testutil.MustMapField(t, testutil.MustMapField(t, relationshipStoryPost, "requestBody"), "content")
	relationshipStoryJSON := testutil.MustMapField(t, relationshipStoryBody, "application/json")
	relationshipStorySchema := testutil.MustMapField(t, testutil.MustMapField(t, relationshipStoryJSON, "schema"), "properties")
	for _, field := range []string{"query_type", "target", "entity_id", "direction", "relationship_type", "relationship_types", "include_transitive", "max_depth", "limit", "offset", "token_budget"} {
		if _, ok := relationshipStorySchema[field]; !ok {
			t.Fatalf("code/relationships/story request schema missing %s", field)
		}
	}
	relationshipStoryResponses := testutil.MustMapField(t, relationshipStoryPost, "responses")
	relationshipStoryOK := testutil.MustMapField(t, relationshipStoryResponses, "200")
	relationshipStoryContent := testutil.MustMapField(t, testutil.MustMapField(t, relationshipStoryOK, "content"), "application/json")
	relationshipStoryResponse := testutil.MustMapField(t, testutil.MustMapField(t, relationshipStoryContent, "schema"), "properties")
	for _, field := range []string{"target_resolution", "relationships", "class_hierarchy", "override_story", "coverage"} {
		if _, ok := relationshipStoryResponse[field]; !ok {
			t.Fatalf("code/relationships/story response schema missing %s", field)
		}
	}

	callChainPath := testutil.MustMapField(t, paths, "/api/v0/code/call-chain")
	callChainPost := testutil.MustMapField(t, callChainPath, "post")
	callChainBody := testutil.MustMapField(t, testutil.MustMapField(t, callChainPost, "requestBody"), "content")
	callChainJSON := testutil.MustMapField(t, callChainBody, "application/json")
	callChainSchema := testutil.MustMapField(t, testutil.MustMapField(t, callChainJSON, "schema"), "properties")
	if _, ok := callChainSchema["start"]; !ok {
		t.Fatal("code/call-chain request schema missing start")
	}
	if _, ok := callChainSchema["end"]; !ok {
		t.Fatal("code/call-chain request schema missing end")
	}
	if _, ok := callChainSchema["start_entity_id"]; !ok {
		t.Fatal("code/call-chain request schema missing start_entity_id")
	}
	if _, ok := callChainSchema["end_entity_id"]; !ok {
		t.Fatal("code/call-chain request schema missing end_entity_id")
	}
	if _, ok := callChainSchema["repo_id"]; !ok {
		t.Fatal("code/call-chain request schema missing repo_id")
	}
	if _, ok := callChainSchema["max_depth"]; !ok {
		t.Fatal("code/call-chain request schema missing max_depth")
	}

	deadCodePath := testutil.MustMapField(t, paths, "/api/v0/code/dead-code")
	deadCodePost := testutil.MustMapField(t, deadCodePath, "post")
	deadCodeBody := testutil.MustMapField(t, testutil.MustMapField(t, deadCodePost, "requestBody"), "content")
	deadCodeJSON := testutil.MustMapField(t, deadCodeBody, "application/json")
	deadCodeSchema := testutil.MustMapField(t, testutil.MustMapField(t, deadCodeJSON, "schema"), "properties")
	if _, ok := deadCodeSchema["repo_id"]; !ok {
		t.Fatal("code/dead-code request schema missing repo_id")
	}
	if _, ok := deadCodeSchema["limit"]; !ok {
		t.Fatal("code/dead-code request schema missing limit")
	}
	if _, ok := deadCodeSchema["exclude_decorated_with"]; !ok {
		t.Fatal("code/dead-code request schema missing exclude_decorated_with")
	}
	deadCodeResponses := testutil.MustMapField(t, deadCodePost, "responses")
	deadCodeOK := testutil.MustMapField(t, deadCodeResponses, "200")
	deadCodeContent := testutil.MustMapField(t, testutil.MustMapField(t, deadCodeOK, "content"), "application/json")
	deadCodeResponse := testutil.MustMapField(t, testutil.MustMapField(t, deadCodeContent, "schema"), "properties")
	if _, ok := deadCodeResponse["analysis"]; !ok {
		t.Fatal("code/dead-code response schema missing analysis")
	}
	if _, ok := deadCodeResponse["truncated"]; !ok {
		t.Fatal("code/dead-code response schema missing truncated")
	}
	deadCodeAnalysis := testutil.MustMapField(t, testutil.MustMapField(t, deadCodeResponse, "analysis"), "properties")
	if _, ok := deadCodeAnalysis["modeled_public_api"]; !ok {
		t.Fatal("code/dead-code analysis schema missing modeled_public_api")
	}
	if _, ok := deadCodeAnalysis["dead_code_language_exactness_blockers"]; !ok {
		t.Fatal("code/dead-code analysis schema missing dead_code_language_exactness_blockers")
	}
	if _, ok := deadCodeAnalysis["dead_code_observed_exactness_blockers"]; !ok {
		t.Fatal("code/dead-code analysis schema missing dead_code_observed_exactness_blockers")
	}

	deadIaCPath := testutil.MustMapField(t, paths, "/api/v0/iac/dead")
	deadIaCPost := testutil.MustMapField(t, deadIaCPath, "post")
	deadIaCBody := testutil.MustMapField(t, testutil.MustMapField(t, deadIaCPost, "requestBody"), "content")
	deadIaCJSON := testutil.MustMapField(t, deadIaCBody, "application/json")
	deadIaCSchema := testutil.MustMapField(t, testutil.MustMapField(t, deadIaCJSON, "schema"), "properties")
	if _, ok := deadIaCSchema["repo_ids"]; !ok {
		t.Fatal("iac/dead request schema missing repo_ids")
	}
	if _, ok := deadIaCSchema["include_ambiguous"]; !ok {
		t.Fatal("iac/dead request schema missing include_ambiguous")
	}
	if _, ok := deadIaCSchema["offset"]; !ok {
		t.Fatal("iac/dead request schema missing offset")
	}
	deadIaCResponses := testutil.MustMapField(t, deadIaCPost, "responses")
	deadIaCOK := testutil.MustMapField(t, deadIaCResponses, "200")
	deadIaCContent := testutil.MustMapField(t, testutil.MustMapField(t, deadIaCOK, "content"), "application/json")
	deadIaCResponse := testutil.MustMapField(t, testutil.MustMapField(t, deadIaCContent, "schema"), "properties")
	if _, ok := deadIaCResponse["findings"]; !ok {
		t.Fatal("iac/dead response schema missing findings")
	}
	if _, ok := deadIaCResponse["total_findings_count"]; !ok {
		t.Fatal("iac/dead response schema missing total_findings_count")
	}
	if _, ok := deadIaCResponse["truncated"]; !ok {
		t.Fatal("iac/dead response schema missing truncated")
	}
	if _, ok := deadIaCResponse["next_offset"]; !ok {
		t.Fatal("iac/dead response schema missing next_offset")
	}

	unmanagedPath := testutil.MustMapField(t, paths, "/api/v0/iac/unmanaged-resources")
	unmanagedPost := testutil.MustMapField(t, unmanagedPath, "post")
	unmanagedBody := testutil.MustMapField(t, testutil.MustMapField(t, unmanagedPost, "requestBody"), "content")
	unmanagedJSON := testutil.MustMapField(t, unmanagedBody, "application/json")
	unmanagedSchema := testutil.MustMapField(t, testutil.MustMapField(t, unmanagedJSON, "schema"), "properties")
	if _, ok := unmanagedSchema["scope_id"]; !ok {
		t.Fatal("iac/unmanaged-resources request schema missing scope_id")
	}
	if _, ok := unmanagedSchema["account_id"]; !ok {
		t.Fatal("iac/unmanaged-resources request schema missing account_id")
	}
	if _, ok := unmanagedSchema["finding_kinds"]; !ok {
		t.Fatal("iac/unmanaged-resources request schema missing finding_kinds")
	}
	unmanagedResponses := testutil.MustMapField(t, unmanagedPost, "responses")
	unmanagedOK := testutil.MustMapField(t, unmanagedResponses, "200")
	unmanagedContent := testutil.MustMapField(t, testutil.MustMapField(t, unmanagedOK, "content"), "application/json")
	unmanagedResponse := testutil.MustMapField(t, testutil.MustMapField(t, unmanagedContent, "schema"), "properties")
	if _, ok := unmanagedResponse["findings"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing findings")
	}
	if _, ok := unmanagedResponse["total_findings_count"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing total_findings_count")
	}
	if _, ok := unmanagedResponse["graph_projection_note"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing graph_projection_note")
	}
	if _, ok := unmanagedResponse["story"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing story")
	}
	if _, ok := unmanagedResponse["finding_groups"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing finding_groups")
	}
	unmanagedFindings := testutil.MustMapField(t, unmanagedResponse, "findings")
	unmanagedFindingItems := testutil.MustMapField(t, unmanagedFindings, "items")
	unmanagedFindingProps := testutil.MustMapField(t, unmanagedFindingItems, "properties")
	for _, field := range []string{
		"management_status",
		"tags",
		"matched_terraform_state_address",
		"matched_terraform_config_file",
		"matched_terraform_module_path",
		"matched_other_iac_source",
		"service_candidates",
		"environment_candidates",
		"dependency_paths",
		"warning_flags",
	} {
		if _, ok := unmanagedFindingProps[field]; !ok {
			t.Fatalf("iac/unmanaged-resources finding schema missing %s", field)
		}
	}
	if _, ok := deadIaCResponse["limitations"]; !ok {
		t.Fatal("iac/dead response schema missing limitations")
	}

	statusPath := testutil.MustMapField(t, paths, "/api/v0/iac/management-status")
	statusPost := testutil.MustMapField(t, statusPath, "post")
	statusBody := testutil.MustMapField(t, testutil.MustMapField(t, statusPost, "requestBody"), "content")
	statusJSON := testutil.MustMapField(t, statusBody, "application/json")
	statusSchema := testutil.MustMapField(t, testutil.MustMapField(t, statusJSON, "schema"), "properties")
	if _, ok := statusSchema["arn"]; !ok {
		t.Fatal("iac/management-status request schema missing arn")
	}
	if _, ok := statusSchema["resource_id"]; !ok {
		t.Fatal("iac/management-status request schema missing resource_id")
	}
	statusResponses := testutil.MustMapField(t, statusPost, "responses")
	statusOK := testutil.MustMapField(t, statusResponses, "200")
	statusResponse := testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, statusOK, "content"), "application/json"), "schema")
	statusProps := testutil.MustMapField(t, statusResponse, "properties")
	if _, ok := statusProps["management_status"]; !ok {
		t.Fatal("iac/management-status response schema missing management_status")
	}
	if _, ok := statusProps["story"]; !ok {
		t.Fatal("iac/management-status response schema missing story")
	}

	explainPath := testutil.MustMapField(t, paths, "/api/v0/iac/management-status/explain")
	explainPost := testutil.MustMapField(t, explainPath, "post")
	explainResponses := testutil.MustMapField(t, explainPost, "responses")
	explainOK := testutil.MustMapField(t, explainResponses, "200")
	explainProps := testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, explainOK, "content"), "application/json"), "schema"), "properties")
	if _, ok := explainProps["evidence_groups"]; !ok {
		t.Fatal("iac/management-status/explain response schema missing evidence_groups")
	}

	relationshipsPath := testutil.MustMapField(t, paths, "/api/v0/code/relationships")
	relationshipsPost := testutil.MustMapField(t, relationshipsPath, "post")
	relationshipsBody := testutil.MustMapField(t, testutil.MustMapField(t, relationshipsPost, "requestBody"), "content")
	relationshipsJSON := testutil.MustMapField(t, relationshipsBody, "application/json")
	relationshipsSchema := testutil.MustMapField(t, testutil.MustMapField(t, relationshipsJSON, "schema"), "properties")
	if _, ok := relationshipsSchema["entity_id"]; !ok {
		t.Fatal("code/relationships request schema missing entity_id")
	}
	if _, ok := relationshipsSchema["name"]; !ok {
		t.Fatal("code/relationships request schema missing name")
	}
	if _, ok := relationshipsSchema["direction"]; !ok {
		t.Fatal("code/relationships request schema missing direction")
	}
	if _, ok := relationshipsSchema["relationship_type"]; !ok {
		t.Fatal("code/relationships request schema missing relationship_type")
	}
	if _, ok := relationshipsSchema["transitive"]; !ok {
		t.Fatal("code/relationships request schema missing transitive")
	}
	if _, ok := relationshipsSchema["max_depth"]; !ok {
		t.Fatal("code/relationships request schema missing max_depth")
	}

	traceDeploymentPath := testutil.MustMapField(t, paths, "/api/v0/impact/trace-deployment-chain")
	traceDeploymentPost := testutil.MustMapField(t, traceDeploymentPath, "post")
	traceDeploymentBody := testutil.MustMapField(t, testutil.MustMapField(t, traceDeploymentPost, "requestBody"), "content")
	traceDeploymentJSON := testutil.MustMapField(t, traceDeploymentBody, "application/json")
	traceDeploymentSchema := testutil.MustMapField(t, testutil.MustMapField(t, traceDeploymentJSON, "schema"), "properties")
	if _, ok := traceDeploymentSchema["service_name"]; !ok {
		t.Fatal("impact/trace-deployment-chain request schema missing service_name")
	}

	traceDeploymentResponses := testutil.MustMapField(t, traceDeploymentPost, "responses")
	traceDeploymentOK := testutil.MustMapField(t, traceDeploymentResponses, "200")
	traceDeploymentContent := testutil.MustMapField(t, testutil.MustMapField(t, traceDeploymentOK, "content"), "application/json")
	traceDeploymentResponse := testutil.MustMapField(t, testutil.MustMapField(t, traceDeploymentContent, "schema"), "properties")
	for _, field := range []string{
		"subject",
		"hostnames",
		"entrypoints",
		"network_paths",
		"observed_config_environments",
		"api_surface",
		"dependents",
		"deployment_sources",
		"cloud_resources",
		"k8s_resources",
		"image_refs",
		"k8s_relationships",
		"deployment_facts",
		"controller_driven_paths",
		"delivery_paths",
		"story_sections",
		"deployment_overview",
		"gitops_overview",
		"consumer_repositories",
		"provisioning_source_chains",
		"deployment_evidence",
		"documentation_overview",
		"support_overview",
		"controller_overview",
		"runtime_overview",
		"deployment_fact_summary",
		"drilldowns",
	} {
		if _, ok := traceDeploymentResponse[field]; !ok {
			t.Fatalf("impact/trace-deployment-chain response schema missing %s", field)
		}
	}
	controllerOverview := testutil.MustMapField(t, traceDeploymentResponse, "controller_overview")
	controllerOverviewProperties := testutil.MustMapField(t, controllerOverview, "properties")
	if _, ok := controllerOverviewProperties["entities"]; !ok {
		t.Fatal("impact/trace-deployment-chain controller_overview schema missing entities")
	}

	repositoryStoryPath := testutil.MustMapField(t, paths, "/api/v0/repositories/{repo_id}/story")
	repositoryStoryGet := testutil.MustMapField(t, repositoryStoryPath, "get")
	repositoryStoryResponses := testutil.MustMapField(t, repositoryStoryGet, "responses")
	repositoryStoryOK := testutil.MustMapField(t, repositoryStoryResponses, "200")
	repositoryStoryContent := testutil.MustMapField(t, testutil.MustMapField(t, repositoryStoryOK, "content"), "application/json")
	repositoryStorySchema := testutil.MustMapField(t, testutil.MustMapField(t, repositoryStoryContent, "schema"), "properties")
	for _, field := range []string{
		"repository",
		"subject",
		"story",
		"story_sections",
		"semantic_overview",
		"deployment_overview",
		"gitops_overview",
		"documentation_overview",
		"support_overview",
		"coverage_summary",
		"limitations",
		"drilldowns",
	} {
		if _, ok := repositoryStorySchema[field]; !ok {
			t.Fatalf("repositories/{repo_id}/story response schema missing %s", field)
		}
	}

	serviceContextPath := testutil.MustMapField(t, paths, "/api/v0/services/{service_name}/context")
	serviceContextGet := testutil.MustMapField(t, serviceContextPath, "get")
	serviceContextResponses := testutil.MustMapField(t, serviceContextGet, "responses")
	serviceContextOK := testutil.MustMapField(t, serviceContextResponses, "200")
	serviceContextContent := testutil.MustMapField(t, testutil.MustMapField(t, serviceContextOK, "content"), "application/json")
	serviceContextSchema := testutil.MustMapField(t, serviceContextContent, "schema")
	if got, want := serviceContextSchema["$ref"], "#/components/schemas/WorkloadContext"; got != want {
		t.Fatalf("services/{service_name}/context schema ref = %#v, want %#v", got, want)
	}
	workloadContextSchema := testutil.MustMapField(t, schemas, "WorkloadContext")
	workloadContextProperties := testutil.MustMapField(t, workloadContextSchema, "properties")
	for _, field := range []string{"deployment_evidence", "entrypoints", "network_paths", "dependents", "ingress_posture"} {
		if _, ok := workloadContextProperties[field]; !ok {
			t.Fatalf("WorkloadContext schema missing %s", field)
		}
	}

	repositoryCoveragePath := testutil.MustMapField(t, paths, "/api/v0/repositories/{repo_id}/coverage")
	repositoryCoverageGet := testutil.MustMapField(t, repositoryCoveragePath, "get")
	repositoryCoverageResponses := testutil.MustMapField(t, repositoryCoverageGet, "responses")
	repositoryCoverageOK := testutil.MustMapField(t, repositoryCoverageResponses, "200")
	repositoryCoverageContent := testutil.MustMapField(t, testutil.MustMapField(t, repositoryCoverageOK, "content"), "application/json")
	repositoryCoverageSchema := testutil.MustMapField(t, testutil.MustMapField(t, repositoryCoverageContent, "schema"), "properties")
	for _, field := range []string{
		"repo_id",
		"completeness_state",
		"graph_available",
		"server_content_available",
		"graph_gap_count",
		"content_gap_count",
		"file_count",
		"entity_count",
		"content_last_indexed_at",
		"last_error",
		"languages",
		"summary",
	} {
		if _, ok := repositoryCoverageSchema[field]; !ok {
			t.Fatalf("repositories/{repo_id}/coverage response schema missing %s", field)
		}
	}

	languageQueryPath := testutil.MustMapField(t, paths, "/api/v0/code/language-query")
	languageQueryPost := testutil.MustMapField(t, languageQueryPath, "post")
	languageQueryBody := testutil.MustMapField(t, testutil.MustMapField(t, languageQueryPost, "requestBody"), "content")
	languageQueryJSON := testutil.MustMapField(t, languageQueryBody, "application/json")
	languageQuerySchema := testutil.MustMapField(t, testutil.MustMapField(t, languageQueryJSON, "schema"), "properties")
	entityType := testutil.MustMapField(t, languageQuerySchema, "entity_type")
	enumValues, ok := entityType["enum"].([]any)
	if !ok {
		t.Fatalf("language-query entity_type enum type = %T, want []any", entityType["enum"])
	}
	if !containsValue(enumValues, "type_alias") ||
		!containsValue(enumValues, "annotation") ||
		!containsValue(enumValues, "protocol") ||
		!containsValue(enumValues, "impl_block") ||
		!containsValue(enumValues, "type_annotation") ||
		!containsValue(enumValues, "typedef") ||
		!containsValue(enumValues, "component") ||
		!containsValue(enumValues, "terraform_module") ||
		!containsValue(enumValues, "terragrunt_config") ||
		!containsValue(enumValues, "terragrunt_dependency") ||
		!containsValue(enumValues, "terragrunt_local") ||
		!containsValue(enumValues, "terragrunt_input") ||
		!containsValue(enumValues, "sql_table") ||
		!containsValue(enumValues, "sql_view") ||
		!containsValue(enumValues, "sql_function") ||
		!containsValue(enumValues, "sql_trigger") ||
		!containsValue(enumValues, "sql_index") ||
		!containsValue(enumValues, "sql_column") {
		t.Fatalf("language-query entity_type enum = %#v, want content-backed entity types", enumValues)
	}
}

func TestOpenAPISpecPackageRegistryPublishedAtIsDateTime(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	versionsPath := testutil.MustMapField(t, paths, "/api/v0/package-registry/versions")
	versionsGet := testutil.MustMapField(t, versionsPath, "get")
	responses := testutil.MustMapField(t, versionsGet, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, testutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := testutil.MustMapField(t, content, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	versions := testutil.MustMapField(t, properties, "versions")
	items := testutil.MustMapField(t, versions, "items")
	versionProperties := testutil.MustMapField(t, items, "properties")
	publishedAt := testutil.MustMapField(t, versionProperties, "published_at")
	if got, want := publishedAt["format"], "date-time"; got != want {
		t.Fatalf("published_at format = %#v, want %#v", got, want)
	}
}

// TestOpenAPISearchBundlesRejectsEmptyScope proves the bundle search schema
// enforces a non-empty, non-whitespace query or ecosystem so generated clients
// and docs reject blank scope the same way handleSearchBundles does (#3520
// follow-up). Without minLength/pattern the anyOf only requires the property to
// be present, so {"query": ""} would satisfy the schema while the handler trims
// and returns 400 — schema and handler must agree.
func TestOpenAPISearchBundlesRejectsEmptyScope(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	bundlesPath := testutil.MustMapField(t, paths, "/api/v0/code/bundles")
	post := testutil.MustMapField(t, bundlesPath, "post")
	requestBody := testutil.MustMapField(t, post, "requestBody")
	if required, _ := requestBody["required"].(bool); !required {
		t.Fatalf("bundles requestBody.required = %v, want true", requestBody["required"])
	}
	content := testutil.MustMapField(t, requestBody, "content")
	appJSON := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, appJSON, "schema")

	anyOf, ok := schema["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("bundles schema anyOf = %#v, want two scope alternatives", schema["anyOf"])
	}

	properties := testutil.MustMapField(t, schema, "properties")
	for _, field := range []string{"query", "ecosystem"} {
		prop := testutil.MustMapField(t, properties, field)
		minLen, ok := prop["minLength"].(float64)
		if !ok || minLen < 1 {
			t.Fatalf("bundles schema %q minLength = %#v, want >= 1", field, prop["minLength"])
		}
		pattern, ok := prop["pattern"].(string)
		if !ok || !strings.Contains(pattern, `\S`) {
			t.Fatalf("bundles schema %q pattern = %#v, want a non-whitespace (\\S) constraint", field, prop["pattern"])
		}
	}
}

func containsValue(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
