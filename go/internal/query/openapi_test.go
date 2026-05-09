package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
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
		"/api/v0/repositories/{repo_id}/context",
		"/api/v0/entities/resolve",
		"/api/v0/evidence/relationships/{resolved_id}",
		"/api/v0/documentation/findings",
		"/api/v0/documentation/findings/{finding_id}/evidence-packet",
		"/api/v0/documentation/evidence-packets/{packet_id}/freshness",
		"/api/v0/code/search",
		"/api/v0/code/call-chain",
		"/api/v0/code/language-query",
		"/api/v0/content/files/read",
		"/api/v0/infra/resources/search",
		"/api/v0/iac/dead",
		"/api/v0/impact/trace-deployment-chain",
		"/api/v0/impact/blast-radius",
		"/api/v0/status/pipeline",
		"/api/v0/compare/environments",
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

	repositoryContextPath := mustMapField(t, paths, "/api/v0/repositories/{repo_id}/context")
	repositoryContextGet := mustMapField(t, repositoryContextPath, "get")
	repositoryContextResponses := mustMapField(t, repositoryContextGet, "responses")
	repositoryContextOK := mustMapField(t, repositoryContextResponses, "200")
	repositoryContextContent := mustMapField(t, mustMapField(t, repositoryContextOK, "content"), "application/json")
	repositoryContextSchema := mustMapField(t, mustMapField(t, repositoryContextContent, "schema"), "properties")
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

	paths := mustMapField(t, spec, "paths")
	readPath := mustMapField(t, paths, "/api/v0/content/entities/read")
	readPost := mustMapField(t, readPath, "post")
	readResponses := mustMapField(t, readPost, "responses")
	readOK := mustMapField(t, readResponses, "200")
	readContent := mustMapField(t, mustMapField(t, readOK, "content"), "application/json")
	readSchema := mustMapField(t, readContent, "schema")
	if got, want := readSchema["$ref"], "#/components/schemas/EntityContent"; got != want {
		t.Fatalf("content/entities/read schema ref = %#v, want %#v", got, want)
	}

	searchPath := mustMapField(t, paths, "/api/v0/content/entities/search")
	searchPost := mustMapField(t, searchPath, "post")
	searchRequestBody := mustMapField(t, searchPost, "requestBody")
	searchRequestContent := mustMapField(t, mustMapField(t, searchRequestBody, "content"), "application/json")
	searchRequestSchema := mustMapField(t, searchRequestContent, "schema")
	searchRequestProperties := mustMapField(t, searchRequestSchema, "properties")
	if _, ok := searchRequestProperties["repo_ids"]; !ok {
		t.Fatal("content/entities/search schema missing repo_ids property")
	}
	if _, ok := searchRequestProperties["pattern"]; !ok {
		t.Fatal("content/entities/search schema missing pattern property")
	}
	searchRequestRequirements, ok := searchRequestSchema["anyOf"].([]any)
	if !ok || len(searchRequestRequirements) != 2 {
		t.Fatalf("content/entities/search schema anyOf = %#v, want 2 pattern requirement variants", searchRequestSchema["anyOf"])
	}

	searchResponses := mustMapField(t, searchPost, "responses")
	searchOK := mustMapField(t, searchResponses, "200")
	searchContent := mustMapField(t, mustMapField(t, searchOK, "content"), "application/json")
	searchSchema := mustMapField(t, searchContent, "schema")
	if got, want := searchSchema["$ref"], "#/components/schemas/EntityContentSearchResponse"; got != want {
		t.Fatalf("content/entities/search schema ref = %#v, want %#v", got, want)
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	entitySchema := mustMapField(t, schemas, "EntityContent")
	entityProperties := mustMapField(t, entitySchema, "properties")
	metadata := mustMapField(t, entityProperties, "metadata")
	if got, want := metadata["type"], "object"; got != want {
		t.Fatalf("EntityContent.metadata.type = %#v, want %#v", got, want)
	}

	entityRefSchema := mustMapField(t, schemas, "EntityRef")
	entityRefProperties := mustMapField(t, entityRefSchema, "properties")
	entityRefSemanticSummary := mustMapField(t, entityRefProperties, "semantic_summary")
	if got, want := entityRefSemanticSummary["type"], "string"; got != want {
		t.Fatalf("EntityRef.semantic_summary.type = %#v, want %#v", got, want)
	}
	entityRefSemanticProfile := mustMapField(t, entityRefProperties, "semantic_profile")
	if got, want := entityRefSemanticProfile["type"], "object"; got != want {
		t.Fatalf("EntityRef.semantic_profile.type = %#v, want %#v", got, want)
	}
	entityRefMetadata := mustMapField(t, entityRefProperties, "metadata")
	if got, want := entityRefMetadata["type"], "object"; got != want {
		t.Fatalf("EntityRef.metadata.type = %#v, want %#v", got, want)
	}

	entityContextPath := mustMapField(t, paths, "/api/v0/entities/{entity_id}/context")
	entityContextGet := mustMapField(t, entityContextPath, "get")
	entityContextResponses := mustMapField(t, entityContextGet, "responses")
	entityContextOK := mustMapField(t, entityContextResponses, "200")
	entityContextContent := mustMapField(t, mustMapField(t, entityContextOK, "content"), "application/json")
	entityContextSchema := mustMapField(t, entityContextContent, "schema")
	entityContextProperties := mustMapField(t, entityContextSchema, "properties")
	entityContextMetadata := mustMapField(t, entityContextProperties, "metadata")
	if got, want := entityContextMetadata["type"], "object"; got != want {
		t.Fatalf("entity context metadata.type = %#v, want %#v", got, want)
	}
	entityContextSemanticProfile := mustMapField(t, entityContextProperties, "semantic_profile")
	if got, want := entityContextSemanticProfile["type"], "object"; got != want {
		t.Fatalf("entity context semantic_profile.type = %#v, want %#v", got, want)
	}
	entityContextStory := mustMapField(t, entityContextProperties, "story")
	if got, want := entityContextStory["type"], "string"; got != want {
		t.Fatalf("entity context story.type = %#v, want %#v", got, want)
	}

	codeSearchPath := mustMapField(t, paths, "/api/v0/code/search")
	codeSearchPost := mustMapField(t, codeSearchPath, "post")
	codeSearchResponses := mustMapField(t, codeSearchPost, "responses")
	codeSearchOK := mustMapField(t, codeSearchResponses, "200")
	codeSearchContent := mustMapField(t, mustMapField(t, codeSearchOK, "content"), "application/json")
	codeSearchSchema := mustMapField(t, codeSearchContent, "schema")
	if got, want := codeSearchSchema["$ref"], "#/components/schemas/CodeSearchResponse"; got != want {
		t.Fatalf("code/search schema ref = %#v, want %#v", got, want)
	}
	codeSearchResultSchema := mustMapField(t, schemas, "CodeSearchResult")
	codeSearchResultProperties := mustMapField(t, codeSearchResultSchema, "properties")
	codeSearchSemanticProfile := mustMapField(t, codeSearchResultProperties, "semantic_profile")
	if got, want := codeSearchSemanticProfile["type"], "object"; got != want {
		t.Fatalf("CodeSearchResult.semantic_profile.type = %#v, want %#v", got, want)
	}

	callChainPath := mustMapField(t, paths, "/api/v0/code/call-chain")
	callChainPost := mustMapField(t, callChainPath, "post")
	callChainBody := mustMapField(t, mustMapField(t, callChainPost, "requestBody"), "content")
	callChainJSON := mustMapField(t, callChainBody, "application/json")
	callChainSchema := mustMapField(t, mustMapField(t, callChainJSON, "schema"), "properties")
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

	deadCodePath := mustMapField(t, paths, "/api/v0/code/dead-code")
	deadCodePost := mustMapField(t, deadCodePath, "post")
	deadCodeBody := mustMapField(t, mustMapField(t, deadCodePost, "requestBody"), "content")
	deadCodeJSON := mustMapField(t, deadCodeBody, "application/json")
	deadCodeSchema := mustMapField(t, mustMapField(t, deadCodeJSON, "schema"), "properties")
	if _, ok := deadCodeSchema["repo_id"]; !ok {
		t.Fatal("code/dead-code request schema missing repo_id")
	}
	if _, ok := deadCodeSchema["limit"]; !ok {
		t.Fatal("code/dead-code request schema missing limit")
	}
	if _, ok := deadCodeSchema["exclude_decorated_with"]; !ok {
		t.Fatal("code/dead-code request schema missing exclude_decorated_with")
	}
	deadCodeResponses := mustMapField(t, deadCodePost, "responses")
	deadCodeOK := mustMapField(t, deadCodeResponses, "200")
	deadCodeContent := mustMapField(t, mustMapField(t, deadCodeOK, "content"), "application/json")
	deadCodeResponse := mustMapField(t, mustMapField(t, deadCodeContent, "schema"), "properties")
	if _, ok := deadCodeResponse["analysis"]; !ok {
		t.Fatal("code/dead-code response schema missing analysis")
	}
	if _, ok := deadCodeResponse["truncated"]; !ok {
		t.Fatal("code/dead-code response schema missing truncated")
	}
	deadCodeAnalysis := mustMapField(t, mustMapField(t, deadCodeResponse, "analysis"), "properties")
	if _, ok := deadCodeAnalysis["modeled_public_api"]; !ok {
		t.Fatal("code/dead-code analysis schema missing modeled_public_api")
	}

	deadIaCPath := mustMapField(t, paths, "/api/v0/iac/dead")
	deadIaCPost := mustMapField(t, deadIaCPath, "post")
	deadIaCBody := mustMapField(t, mustMapField(t, deadIaCPost, "requestBody"), "content")
	deadIaCJSON := mustMapField(t, deadIaCBody, "application/json")
	deadIaCSchema := mustMapField(t, mustMapField(t, deadIaCJSON, "schema"), "properties")
	if _, ok := deadIaCSchema["repo_ids"]; !ok {
		t.Fatal("iac/dead request schema missing repo_ids")
	}
	if _, ok := deadIaCSchema["include_ambiguous"]; !ok {
		t.Fatal("iac/dead request schema missing include_ambiguous")
	}
	if _, ok := deadIaCSchema["offset"]; !ok {
		t.Fatal("iac/dead request schema missing offset")
	}
	deadIaCResponses := mustMapField(t, deadIaCPost, "responses")
	deadIaCOK := mustMapField(t, deadIaCResponses, "200")
	deadIaCContent := mustMapField(t, mustMapField(t, deadIaCOK, "content"), "application/json")
	deadIaCResponse := mustMapField(t, mustMapField(t, deadIaCContent, "schema"), "properties")
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
	if _, ok := deadIaCResponse["limitations"]; !ok {
		t.Fatal("iac/dead response schema missing limitations")
	}

	relationshipsPath := mustMapField(t, paths, "/api/v0/code/relationships")
	relationshipsPost := mustMapField(t, relationshipsPath, "post")
	relationshipsBody := mustMapField(t, mustMapField(t, relationshipsPost, "requestBody"), "content")
	relationshipsJSON := mustMapField(t, relationshipsBody, "application/json")
	relationshipsSchema := mustMapField(t, mustMapField(t, relationshipsJSON, "schema"), "properties")
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

	traceDeploymentPath := mustMapField(t, paths, "/api/v0/impact/trace-deployment-chain")
	traceDeploymentPost := mustMapField(t, traceDeploymentPath, "post")
	traceDeploymentBody := mustMapField(t, mustMapField(t, traceDeploymentPost, "requestBody"), "content")
	traceDeploymentJSON := mustMapField(t, traceDeploymentBody, "application/json")
	traceDeploymentSchema := mustMapField(t, mustMapField(t, traceDeploymentJSON, "schema"), "properties")
	if _, ok := traceDeploymentSchema["service_name"]; !ok {
		t.Fatal("impact/trace-deployment-chain request schema missing service_name")
	}

	traceDeploymentResponses := mustMapField(t, traceDeploymentPost, "responses")
	traceDeploymentOK := mustMapField(t, traceDeploymentResponses, "200")
	traceDeploymentContent := mustMapField(t, mustMapField(t, traceDeploymentOK, "content"), "application/json")
	traceDeploymentResponse := mustMapField(t, mustMapField(t, traceDeploymentContent, "schema"), "properties")
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
	controllerOverview := mustMapField(t, traceDeploymentResponse, "controller_overview")
	controllerOverviewProperties := mustMapField(t, controllerOverview, "properties")
	if _, ok := controllerOverviewProperties["entities"]; !ok {
		t.Fatal("impact/trace-deployment-chain controller_overview schema missing entities")
	}

	repositoryStoryPath := mustMapField(t, paths, "/api/v0/repositories/{repo_id}/story")
	repositoryStoryGet := mustMapField(t, repositoryStoryPath, "get")
	repositoryStoryResponses := mustMapField(t, repositoryStoryGet, "responses")
	repositoryStoryOK := mustMapField(t, repositoryStoryResponses, "200")
	repositoryStoryContent := mustMapField(t, mustMapField(t, repositoryStoryOK, "content"), "application/json")
	repositoryStorySchema := mustMapField(t, mustMapField(t, repositoryStoryContent, "schema"), "properties")
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

	serviceContextPath := mustMapField(t, paths, "/api/v0/services/{service_name}/context")
	serviceContextGet := mustMapField(t, serviceContextPath, "get")
	serviceContextResponses := mustMapField(t, serviceContextGet, "responses")
	serviceContextOK := mustMapField(t, serviceContextResponses, "200")
	serviceContextContent := mustMapField(t, mustMapField(t, serviceContextOK, "content"), "application/json")
	serviceContextSchema := mustMapField(t, serviceContextContent, "schema")
	if got, want := serviceContextSchema["$ref"], "#/components/schemas/WorkloadContext"; got != want {
		t.Fatalf("services/{service_name}/context schema ref = %#v, want %#v", got, want)
	}
	workloadContextSchema := mustMapField(t, schemas, "WorkloadContext")
	workloadContextProperties := mustMapField(t, workloadContextSchema, "properties")
	for _, field := range []string{"deployment_evidence", "entrypoints", "network_paths", "dependents"} {
		if _, ok := workloadContextProperties[field]; !ok {
			t.Fatalf("WorkloadContext schema missing %s", field)
		}
	}

	repositoryCoveragePath := mustMapField(t, paths, "/api/v0/repositories/{repo_id}/coverage")
	repositoryCoverageGet := mustMapField(t, repositoryCoveragePath, "get")
	repositoryCoverageResponses := mustMapField(t, repositoryCoverageGet, "responses")
	repositoryCoverageOK := mustMapField(t, repositoryCoverageResponses, "200")
	repositoryCoverageContent := mustMapField(t, mustMapField(t, repositoryCoverageOK, "content"), "application/json")
	repositoryCoverageSchema := mustMapField(t, mustMapField(t, repositoryCoverageContent, "schema"), "properties")
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

	languageQueryPath := mustMapField(t, paths, "/api/v0/code/language-query")
	languageQueryPost := mustMapField(t, languageQueryPath, "post")
	languageQueryBody := mustMapField(t, mustMapField(t, languageQueryPost, "requestBody"), "content")
	languageQueryJSON := mustMapField(t, languageQueryBody, "application/json")
	languageQuerySchema := mustMapField(t, mustMapField(t, languageQueryJSON, "schema"), "properties")
	entityType := mustMapField(t, languageQuerySchema, "entity_type")
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

func mustMapField(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := parent[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	typed, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("key %q type = %T, want map[string]any", key, value)
	}
	return typed
}

func containsValue(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
