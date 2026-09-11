// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

func TestHandleLanguageQuery_TypeScriptClassFamilyUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		entityType   string
		wantLabel    string
		query        string
		row          map[string]any
		wantSummary  string
		wantSurface  string
		wantMetadata map[string]any
	}{
		{
			name:       "class decorators and generics",
			entityType: "class",
			wantLabel:  "Class",
			query:      "Demo",
			row: map[string]any{
				"entity_id":       "graph-ts-class-1",
				"name":            "Demo",
				"labels":          []any{"Class"},
				"file_path":       "src/decorators.ts",
				"repo_id":         "repo-1",
				"repo_name":       "repo-1",
				"language":        "typescript",
				"start_line":      int64(5),
				"end_line":        int64(20),
				"decorators":      []any{"@sealed"},
				"type_parameters": []any{"T"},
			},
			wantSummary: "Class Demo uses decorators @sealed and declares type parameters T.",
			wantSurface: "generic_declaration",
			wantMetadata: map[string]any{
				"decorators":      []any{"@sealed"},
				"type_parameters": []any{"T"},
			},
		},
		{
			name:       "interface declaration merge",
			entityType: "interface",
			wantLabel:  "Interface",
			query:      "Service",
			row: map[string]any{
				"entity_id":               "graph-ts-interface-1",
				"name":                    "Service",
				"labels":                  []any{"Interface"},
				"file_path":               "src/service.ts",
				"repo_id":                 "repo-1",
				"repo_name":               "repo-1",
				"language":                "typescript",
				"start_line":              int64(20),
				"end_line":                int64(32),
				"declaration_merge_group": "Service",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"class", "interface"},
			},
			wantSummary: "Interface Service participates in TypeScript declaration merging with class Service.",
			wantSurface: "declaration_merge",
			wantMetadata: map[string]any{
				"declaration_merge_group": "Service",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"class", "interface"},
			},
		},
		{
			name:       "enum declaration merge",
			entityType: "enum",
			wantLabel:  "Enum",
			query:      "ServiceKind",
			row: map[string]any{
				"entity_id":               "graph-ts-enum-1",
				"name":                    "ServiceKind",
				"labels":                  []any{"Enum"},
				"file_path":               "src/service.ts",
				"repo_id":                 "repo-1",
				"repo_name":               "repo-1",
				"language":                "typescript",
				"start_line":              int64(34),
				"end_line":                int64(42),
				"declaration_merge_group": "ServiceKind",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"enum", "namespace"},
			},
			wantSummary: "Enum ServiceKind participates in TypeScript declaration merging with namespace ServiceKind.",
			wantSurface: "declaration_merge",
			wantMetadata: map[string]any{
				"declaration_merge_group": "ServiceKind",
				"declaration_merge_count": int64(2),
				"declaration_merge_kinds": []any{"enum", "namespace"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &LanguageQueryHandler{
				Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{tt.row}},
			}

			results, _, err := handler.queryByLanguageWithSemanticFilter(
				context.Background(),
				"typescript",
				tt.wantLabel,
				tt.query,
				"repo-1",
				10,
				"",
				"",
				unscopedLanguageQueryGrant(),
			)
			if err != nil {
				t.Fatalf("queryByLanguageWithSemanticFilter() error = %v, want nil", err)
			}
			if got, want := len(results), 1; got != want {
				t.Fatalf("len(results) = %d, want %d", got, want)
			}

			result := results[0]
			if got, want := result["semantic_summary"], tt.wantSummary; got != want {
				t.Fatalf("results[0][semantic_summary] = %#v, want %#v", got, want)
			}

			profile, ok := result["semantic_profile"].(map[string]any)
			if !ok {
				t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
			}
			if got, want := profile["surface_kind"], tt.wantSurface; got != want {
				t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
			}

			metadata, ok := result["metadata"].(map[string]any)
			if !ok {
				t.Fatalf("results[0][metadata] type = %T, want map[string]any", result["metadata"])
			}
			for key, want := range tt.wantMetadata {
				if got := metadata[key]; got == nil {
					t.Fatalf("metadata[%s] = nil, want %#v", key, want)
				}
			}
		})
	}
}
