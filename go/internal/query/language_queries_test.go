// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

// TestBuildLanguageCypher_Function, TestBuildLanguageCypher_Repository,
// TestBuildLanguageCypher_FunctionDoesNotDuplicateRepoNameAlias,
// TestBuildLanguageCypher_AllEntityTypes, TestJoinKeys, and TestSortStrings
// moved to language_query_cypher_builder_test.go (#6642): each calls
// buildLanguageCypher, joinKeys, or sortStrings directly -- unexported
// language-family free functions the mounted route does not observably
// cover -- so they belong in a family-owned white-box test file.
