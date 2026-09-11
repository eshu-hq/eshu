// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestHandleLanguageQueryProjectsTypeScriptGraphMetadata is driven through
// the mounted route (#6642) rather than calling the unexported
// queryByLanguageWithSemanticFilter method directly: the fake reader's
// assertion on the Cypher text is the same either way (Neo4j.Run is called
// from inside the method regardless of entry point), and the merged
// semantic_summary/semantic_profile fields it proves are visible on the wire.
func TestHandleLanguageQueryProjectsTypeScriptGraphMetadata(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if got, want := querytestutil.BoundCanonicalLanguage(t, params), "typescript"; got != want {
					t.Fatalf("bound canonical language = %#v, want %#v", got, want)
				}
				for _, fragment := range []string{
					"e.type_parameters as type_parameters",
					"e.type_alias_kind as type_alias_kind",
					"e.declaration_merge_group as declaration_merge_group",
					"e.declaration_merge_count as declaration_merge_count",
					"e.declaration_merge_kinds as declaration_merge_kinds",
					"e.decorators as decorators",
					"e.component_type_assertion as component_type_assertion",
					"e.component_wrapper_kind as component_wrapper_kind",
					"e.jsx_fragment_shorthand as jsx_fragment_shorthand",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want %q", cypher, fragment)
					}
				}

				return []map[string]any{
					{
						"entity_id":       "alias-1",
						"name":            "ReadonlyMap",
						"labels":          []any{"TypeAlias"},
						"file_path":       "src/types.ts",
						"repo_id":         "repo-1",
						"repo_name":       "repo-1",
						"language":        "typescript",
						"start_line":      int64(2),
						"end_line":        int64(4),
						"type_alias_kind": "mapped_type",
						"type_parameters": []any{"T"},
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"typescript","entity_type":"type_alias","query":"ReadonlyMap","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one result", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "TypeAlias ReadonlyMap is a mapped type and declares type parameters T."; got != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "mapped_type_alias"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextProjectsTypeScriptGraphMetadata(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if got, want := params["entity_id"], "variable-tsx-1"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				for _, fragment := range []string{
					"e.type_parameters as type_parameters",
					"e.type_alias_kind as type_alias_kind",
					"e.declaration_merge_group as declaration_merge_group",
					"e.declaration_merge_count as declaration_merge_count",
					"e.declaration_merge_kinds as declaration_merge_kinds",
					"e.decorators as decorators",
					"e.component_type_assertion as component_type_assertion",
					"e.component_wrapper_kind as component_wrapper_kind",
					"e.jsx_fragment_shorthand as jsx_fragment_shorthand",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want %q", cypher, fragment)
					}
				}

				return map[string]any{
					"id":                       "variable-tsx-1",
					"labels":                   []any{"Variable"},
					"name":                     "Screen",
					"file_path":                "src/Screen.tsx",
					"repo_id":                  "repo-1",
					"repo_name":                "repo-1",
					"language":                 "tsx",
					"start_line":               int64(6),
					"end_line":                 int64(6),
					"component_type_assertion": "ComponentType",
					"jsx_fragment_shorthand":   true,
				}, nil
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/variable-tsx-1/context", nil)
	req.SetPathValue("entity_id", "variable-tsx-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["semantic_summary"], "Variable Screen narrows to ComponentType and uses JSX fragment shorthand."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "component_type_assertion"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}
