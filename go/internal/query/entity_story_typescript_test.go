package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetEntityContextUsesGraphTypeScriptClassFamilyWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if got, want := params["entity_id"], "class-ts-1"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				for _, fragment := range []string{
					"e.type_parameters as type_parameters",
					"e.declaration_merge_group as declaration_merge_group",
					"e.declaration_merge_count as declaration_merge_count",
					"e.declaration_merge_kinds as declaration_merge_kinds",
					"e.decorators as decorators",
				} {
					if !strings.Contains(cypher, fragment) {
						t.Fatalf("cypher = %q, want %q", cypher, fragment)
					}
				}
				return map[string]any{
					"id":              "class-ts-1",
					"labels":          []any{"Class"},
					"name":            "Demo",
					"file_path":       "src/decorators.ts",
					"repo_id":         "repo-1",
					"repo_name":       "repo-1",
					"language":        "typescript",
					"start_line":      int64(5),
					"end_line":        int64(20),
					"decorators":      []any{"@sealed"},
					"type_parameters": []any{"T"},
					"relationships":   []any{},
				}, nil
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/class-ts-1/context", nil)
	req.SetPathValue("entity_id", "class-ts-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	if got, want := resp["semantic_summary"], "Class Demo uses decorators @sealed and declares type parameters T."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	if got, want := resp["story"], "Class Demo uses decorators @sealed and declares type parameters T. Defined in src/decorators.ts (typescript)."; got != want {
		t.Fatalf("resp[story] = %#v, want %#v", got, want)
	}

	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "generic_declaration"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}

	decorators, ok := profile["decorators"].([]any)
	if !ok {
		t.Fatalf("semantic_profile[decorators] type = %T, want []any", profile["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@sealed" {
		t.Fatalf("semantic_profile[decorators] = %#v, want [@sealed]", decorators)
	}

	typeParameters, ok := profile["type_parameters"].([]any)
	if !ok {
		t.Fatalf("semantic_profile[type_parameters] type = %T, want []any", profile["type_parameters"])
	}
	if len(typeParameters) != 1 || typeParameters[0] != "T" {
		t.Fatalf("semantic_profile[type_parameters] = %#v, want [T]", typeParameters)
	}
}
