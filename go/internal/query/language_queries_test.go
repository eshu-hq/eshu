package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildLanguageCypher_Function(t *testing.T) {
	cypher, params := buildLanguageCypher("python", "Function", "my_func", "repo:123", 10)

	if cypher == "" {
		t.Fatal("expected non-empty cypher")
	}
	// Must contain the Function label.
	if !searchString(cypher, "Function") {
		t.Error("cypher should contain Function label")
	}
	// Must have language param.
	if params["language"] != "python" {
		t.Errorf("language param = %v, want python", params["language"])
	}
	if params["repo_id"] != "repo:123" {
		t.Errorf("repo_id param = %v, want repo:123", params["repo_id"])
	}
	if params["query"] != "my_func" {
		t.Errorf("query param = %v, want my_func", params["query"])
	}
	if params["limit"] != 10 {
		t.Errorf("limit param = %v, want 10", params["limit"])
	}
	// Must filter by language.
	if !searchString(cypher, "$language") {
		t.Error("cypher should reference $language parameter")
	}
	// Must filter by repo.
	if !searchString(cypher, "$repo_id") {
		t.Error("cypher should reference $repo_id parameter")
	}
	// Must filter by name.
	if !searchString(cypher, "$query") {
		t.Error("cypher should reference $query parameter")
	}
	if !searchString(cypher, "e.type_annotation_count as type_annotation_count") {
		t.Error("cypher should project type_annotation_count")
	}
	if !searchString(cypher, "e.type_annotation_kinds as type_annotation_kinds") {
		t.Error("cypher should project type_annotation_kinds")
	}
}

func TestBuildLanguageCypher_Repository(t *testing.T) {
	cypher, params := buildLanguageCypher("go", "Repository", "", "", 25)

	if !searchString(cypher, "Repository") {
		t.Error("cypher should contain Repository label")
	}
	if params["limit"] != 25 {
		t.Errorf("limit param = %v, want 25", params["limit"])
	}
	// No repo_id or query filters when empty.
	if _, ok := params["repo_id"]; ok {
		t.Error("repo_id should not be set when empty")
	}
	if _, ok := params["query"]; ok {
		t.Error("query should not be set when empty")
	}
}

func TestBuildLanguageCypher_File(t *testing.T) {
	cypher, params := buildLanguageCypher("rust", "File", "main", "", 10)

	if !searchString(cypher, "File") {
		t.Error("cypher should contain File label")
	}
	// Rust extension filter.
	if !searchString(cypher, ".rs") {
		t.Error("cypher should contain .rs extension filter")
	}
	if params["query"] != "main" {
		t.Errorf("query param = %v, want main", params["query"])
	}
}

func TestBuildLanguageCypher_Directory(t *testing.T) {
	cypher, _ := buildLanguageCypher("java", "Directory", "", "repo:x", 5)

	if !searchString(cypher, "Directory") {
		t.Error("cypher should contain Directory label")
	}
	if !searchString(cypher, ".java") {
		t.Error("cypher should contain .java extension filter")
	}
}

func TestBuildLanguageCypher_FunctionDoesNotDuplicateRepoNameAlias(t *testing.T) {
	cypher, _ := buildLanguageCypher("python", "Function", "handler", "repo-1", 10)

	if got, want := strings.Count(cypher, " as repo_name"), 1; got != want {
		t.Fatalf("strings.Count(cypher, \" as repo_name\") = %d, want %d; cypher=%q", got, want, cypher)
	}
	if strings.Contains(cypher, "e.repo_name as repo_name") {
		t.Fatalf("cypher = %q, must not alias entity repo_name onto the canonical repo_name column", cypher)
	}
}

func TestHandleLanguageQuery_MissingLanguage(t *testing.T) {
	h := &LanguageQueryHandler{}
	mux := http.NewServeMux()
	h.Mount(mux)

	body := `{"entity_type":"function"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleLanguageQuery_MissingEntityType(t *testing.T) {
	h := &LanguageQueryHandler{}
	mux := http.NewServeMux()
	h.Mount(mux)

	body := `{"language":"python"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleLanguageQuery_UnsupportedLanguage(t *testing.T) {
	h := &LanguageQueryHandler{}
	mux := http.NewServeMux()
	h.Mount(mux)

	body := `{"language":"fortran","entity_type":"function"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	detail, _ := resp["detail"].(string)
	if !searchString(detail, "fortran") {
		t.Errorf("error should mention fortran, got: %s", detail)
	}
}

func TestHandleLanguageQuery_UnsupportedEntityType(t *testing.T) {
	h := &LanguageQueryHandler{}
	mux := http.NewServeMux()
	h.Mount(mux)

	body := `{"language":"python","entity_type":"interface"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	detail, _ := resp["detail"].(string)
	if !searchString(detail, "interface") {
		t.Errorf("error should mention interface, got: %s", detail)
	}
}

func TestHandleLanguageQuery_InvalidJSON(t *testing.T) {
	h := &LanguageQueryHandler{}
	mux := http.NewServeMux()
	h.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleLanguageQuery_ContentBackedEntityTypes(t *testing.T) {
	tests := []struct {
		name       string
		language   string
		entityType string
		query      string
		row        []driver.Value
		wantName   string
		wantKey    string
		wantValue  any
	}{
		{
			name:       "type alias from typescript content store",
			language:   "typescript",
			entityType: "type_alias",
			query:      "UserID",
			row: []driver.Value{
				"alias-1", "repo-1", "src/types.ts", "TypeAlias", "UserID",
				int64(3), int64(3), "typescript", "type UserID = string", []byte(`{"type":"string"}`),
			},
			wantName:  "UserID",
			wantKey:   "type",
			wantValue: "string",
		},
		{
			name:       "type annotation from python content store",
			language:   "python",
			entityType: "type_annotation",
			query:      "user_id",
			row: []driver.Value{
				"ann-1", "repo-1", "app/models.py", "TypeAnnotation", "user_id",
				int64(10), int64(10), "python", "user_id: str", []byte(`{"type":"str"}`),
			},
			wantName:  "user_id",
			wantKey:   "type",
			wantValue: "str",
		},
		{
			name:       "typedef from c content store",
			language:   "c",
			entityType: "typedef",
			query:      "my_int",
			row: []driver.Value{
				"typedef-1", "repo-1", "src/types.c", "Typedef", "my_int",
				int64(3), int64(3), "c", "typedef int my_int;", []byte(`{"type":"int"}`),
			},
			wantName:  "my_int",
			wantKey:   "type",
			wantValue: "int",
		},
		{
			name:       "component from tsx content store",
			language:   "typescript",
			entityType: "component",
			query:      "Button",
			row: []driver.Value{
				"component-1", "repo-1", "src/Button.tsx", "Component", "Button",
				int64(1), int64(12), "tsx", "export function Button() {}", []byte(`{"framework":"react"}`),
			},
			wantName:  "Button",
			wantKey:   "framework",
			wantValue: "react",
		},
		{
			name:       "annotation from java content store",
			language:   "java",
			entityType: "annotation",
			query:      "Logged",
			row: []driver.Value{
				"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
				int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
			},
			wantName:  "Logged",
			wantKey:   "target_kind",
			wantValue: "method_declaration",
		},
		{
			name:       "protocol from swift content store",
			language:   "swift",
			entityType: "protocol",
			query:      "Runnable",
			row: []driver.Value{
				"protocol-1", "repo-1", "Sources/Runnable.swift", "Protocol", "Runnable",
				int64(1), int64(8), "swift", "protocol Runnable {\n  func run()\n}\n", []byte(`{"module_kind":"protocol"}`),
			},
			wantName:  "Runnable",
			wantKey:   "module_kind",
			wantValue: "protocol",
		},
		{
			name:       "guard from elixir content store",
			language:   "elixir",
			entityType: "guard",
			query:      "is_even",
			row: []driver.Value{
				"guard-1", "repo-1", "lib/demo/macros.ex", "Function", "is_even",
				int64(10), int64(10), "elixir", "defguard is_even(value) when rem(value, 2) == 0", []byte(`{"semantic_kind":"guard"}`),
			},
			wantName:  "is_even",
			wantKey:   "semantic_kind",
			wantValue: "guard",
		},
		{
			name:       "protocol implementation from elixir content store",
			language:   "elixir",
			entityType: "protocol_implementation",
			query:      "Demo.Serializable",
			row: []driver.Value{
				"impl-1", "repo-1", "lib/demo/serializable.ex", "ProtocolImplementation", "Demo.Serializable",
				int64(1), int64(4), "elixir", "defimpl Demo.Serializable, for: Demo.Worker do\nend", []byte(`{"module_kind":"protocol_implementation","protocol":"Demo.Serializable","implemented_for":"Demo.Worker"}`),
			},
			wantName:  "Demo.Serializable",
			wantKey:   "module_kind",
			wantValue: "protocol_implementation",
		},
		{
			name:       "module attribute from elixir content store",
			language:   "elixir",
			entityType: "module_attribute",
			query:      "@timeout",
			row: []driver.Value{
				"attr-1", "repo-1", "lib/demo/worker.ex", "Variable", "@timeout",
				int64(2), int64(2), "elixir", "@timeout 5_000", []byte(`{"attribute_kind":"module_attribute","value":"5_000"}`),
			},
			wantName:  "@timeout",
			wantKey:   "attribute_kind",
			wantValue: "module_attribute",
		},
		{
			name:       "impl block from rust content store",
			language:   "rust",
			entityType: "impl_block",
			query:      "Point",
			row: []driver.Value{
				"impl-1", "repo-1", "src/point.rs", "ImplBlock", "Point",
				int64(10), int64(24), "rust", "impl Point {\n  fn x(&self) -> i32 { self.x }\n}\n", []byte(`{"kind":"inherent_impl","target":"Point"}`),
			},
			wantName:  "Point",
			wantKey:   "kind",
			wantValue: "inherent_impl",
		},
		{
			name:       "terragrunt dependency from content store",
			language:   "hcl",
			entityType: "terragrunt_dependency",
			query:      "vpc",
			row: []driver.Value{
				"tg-dep-1", "repo-1", "infra/terragrunt.hcl", "TerragruntDependency", "vpc",
				int64(5), int64(7), "hcl", "dependency \"vpc\" {\n  config_path = \"../vpc\"\n}\n", []byte(`{"config_path":"../vpc"}`),
			},
			wantName:  "vpc",
			wantKey:   "config_path",
			wantValue: "../vpc",
		},
		{
			name:       "terraform module from content store",
			language:   "hcl",
			entityType: "terraform_module",
			query:      "eks",
			row: []driver.Value{
				"tf-module-1", "repo-1", "infra/main.tf", "TerraformModule", "eks",
				int64(1), int64(8), "hcl", "module \"eks\" { source = \"tfr:///terraform-aws-modules/eks/aws?version=19.0.0\" }\n", []byte(`{"source":"tfr:///terraform-aws-modules/eks/aws?version=19.0.0","deployment_name":"comprehensive-cluster"}`),
			},
			wantName:  "eks",
			wantKey:   "source",
			wantValue: "tfr:///terraform-aws-modules/eks/aws?version=19.0.0",
		},
		{
			name:       "terragrunt config from content store",
			language:   "hcl",
			entityType: "terragrunt_config",
			query:      "terragrunt",
			row: []driver.Value{
				"tg-config-1", "repo-1", "infra/terragrunt.hcl", "TerragruntConfig", "terragrunt",
				int64(1), int64(12), "hcl", "terraform { source = \"../modules/app\" }\n", []byte(`{"terraform_source":"../modules/app","includes":"root","inputs":"image_tag"}`),
			},
			wantName:  "terragrunt",
			wantKey:   "terraform_source",
			wantValue: "../modules/app",
		},
		{
			name:       "terragrunt local from content store",
			language:   "hcl",
			entityType: "terragrunt_local",
			query:      "env",
			row: []driver.Value{
				"tg-local-1", "repo-1", "infra/terragrunt.hcl", "TerragruntLocal", "env",
				int64(9), int64(9), "hcl", "env = \"dev\"\n", []byte(`{"value":"dev"}`),
			},
			wantName:  "env",
			wantKey:   "value",
			wantValue: "dev",
		},
		{
			name:       "terragrunt input from content store",
			language:   "hcl",
			entityType: "terragrunt_input",
			query:      "image_tag",
			row: []driver.Value{
				"tg-input-1", "repo-1", "infra/terragrunt.hcl", "TerragruntInput", "image_tag",
				int64(13), int64(13), "hcl", "image_tag = \"latest\"\n", []byte(`{"value":"latest"}`),
			},
			wantName:  "image_tag",
			wantKey:   "value",
			wantValue: "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openContentReaderTestDB(t, []contentReaderQueryResult{
				{
					columns: []string{
						"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
						"start_line", "end_line", "language", "source_cache", "metadata",
					},
					rows: [][]driver.Value{tt.row},
				},
			})

			h := &LanguageQueryHandler{Content: NewContentReader(db)}
			mux := http.NewServeMux()
			h.Mount(mux)

			body := `{"language":"` + tt.language + `","entity_type":"` + tt.entityType + `","query":"` + tt.query + `","repo_id":"repo-1"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", bytes.NewBufferString(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			results, ok := resp["results"].([]any)
			if !ok || len(results) != 1 {
				t.Fatalf("results = %#v, want 1 result", resp["results"])
			}
			result, ok := results[0].(map[string]any)
			if !ok {
				t.Fatalf("result type = %T, want map[string]any", results[0])
			}
			if got, want := result["name"], tt.wantName; got != want {
				t.Fatalf("result[name] = %#v, want %#v", got, want)
			}
			if tt.entityType == "protocol_implementation" {
				labels, ok := result["labels"].([]any)
				if !ok || len(labels) != 1 {
					t.Fatalf("result[labels] = %#v, want one label", result["labels"])
				}
				if got, want := labels[0], "ProtocolImplementation"; got != want {
					t.Fatalf("result[labels][0] = %#v, want %#v", got, want)
				}
			}
			metadata, ok := result["metadata"].(map[string]any)
			if !ok {
				t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
			}
			if got, want := metadata[tt.wantKey], tt.wantValue; got != want {
				t.Fatalf("metadata[%s] = %#v, want %#v", tt.wantKey, got, want)
			}
			if tt.entityType == "component" {
				if got, want := result["semantic_summary"], "Component Button is associated with the react framework."; got != want {
					t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
				}
			}
		})
	}
}

func TestJoinKeys(t *testing.T) {
	m := map[string]bool{"c": true, "a": true, "b": true}
	got := joinKeys(m)
	if got != "a, b, c" {
		t.Errorf("joinKeys = %q, want %q", got, "a, b, c")
	}
}

func TestSortStrings(t *testing.T) {
	s := []string{"go", "c", "rust", "java", "dart"}
	sortStrings(s)
	for i := 1; i < len(s); i++ {
		if s[i] < s[i-1] {
			t.Errorf("not sorted at index %d: %v", i, s)
		}
	}
}

func TestLanguageFileExtensions_Coverage(t *testing.T) {
	// Every supported language should have direct mappings or a canonical alias.
	for lang := range supportedLanguages {
		exts, ok := languageFileExtensions[canonicalLanguage(lang)]
		if !ok || len(exts) == 0 {
			t.Errorf("language %q has no file extension mappings", lang)
		}
	}
}

func TestBuildLanguageCypher_AllEntityTypes(t *testing.T) {
	// Verify all entity types produce valid cypher.
	for typeName, label := range graphBackedEntityTypes {
		cypher, params := buildLanguageCypher("python", label, "", "", 10)
		if cypher == "" {
			t.Errorf("entity type %q produced empty cypher", typeName)
		}
		if params["language"] != "python" {
			t.Errorf("entity type %q: language param = %v", typeName, params["language"])
		}
	}
}
