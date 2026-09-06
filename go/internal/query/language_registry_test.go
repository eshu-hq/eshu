// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"testing"
)

func TestSupportedLanguages(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) != 22 {
		t.Errorf("expected 22 supported languages, got %d: %v", len(langs), langs)
	}
	for i := 1; i < len(langs); i++ {
		if langs[i] < langs[i-1] {
			t.Errorf("languages not sorted: %q comes after %q", langs[i], langs[i-1])
		}
	}

	expected := map[string]bool{
		"go": true, "python": true, "rust": true, "typescript": true, "tsx": true,
		"javascript": true, "jsx": true, "hcl": true, "kotlin": true, "php": true, "elixir": true,
		"sql": true,
	}
	langSet := make(map[string]bool, len(langs))
	for _, lang := range langs {
		langSet[lang] = true
	}
	for want := range expected {
		if !langSet[want] {
			t.Errorf("expected language %q not found", want)
		}
	}
}

func TestSupportedEntityTypes(t *testing.T) {
	types := SupportedEntityTypes()
	if len(types) != 39 {
		t.Errorf("expected 39 supported entity types, got %d: %v", len(types), types)
	}
	expected := map[string]bool{
		"repository": true, "directory": true, "file": true,
		"function": true, "class": true, "struct": true,
		"type_alias": true, "type_annotation": true, "typedef": true, "component": true,
		"annotation": true, "protocol": true, "impl_block": true,
		"guard": true, "protocol_implementation": true, "module_attribute": true,
		"terraform_module": true, "terragrunt_config": true,
		"terraform_backend": true, "terraform_import": true, "terraform_moved_block": true,
		"terraform_removed_block": true, "terraform_check": true, "terraform_lock_provider": true,
		"terragrunt_dependency": true, "terragrunt_local": true, "terragrunt_input": true,
		"sql_table": true, "sql_view": true, "sql_function": true, "sql_migration": true,
		"sql_trigger": true, "sql_index": true, "sql_column": true,
	}
	typeSet := make(map[string]bool, len(types))
	for _, typ := range types {
		typeSet[typ] = true
	}
	for want := range expected {
		if !typeSet[want] {
			t.Errorf("expected entity type %q not found", want)
		}
	}
	// Atlantis entities carry language "yaml", which supportedLanguages does not
	// accept, so they must NOT be advertised in the language-query entity_type
	// enum (they resolve via resolve_entity/get_entity_context instead). #5369.
	// The four Flux typed entities are entity_context-only (#5360 PR A) and
	// likewise carry language "yaml", so they must not leak into the enum either.
	for _, absent := range []string{
		"atlantis_project", "atlantis_workflow",
		"flux_kustomization", "flux_git_repository", "flux_oci_repository", "flux_bucket",
	} {
		if typeSet[absent] {
			t.Errorf("entity type %q must not be language-queryable (no yaml language)", absent)
		}
	}
}

// TestGraphLanguageSpellings pins the value list behind the graph builders'
// `language IN $languages` predicate: the parser's own spelling for every
// language whose parser key differs from the query name, each also
// Title-cased for Python-era rows, canonical spelling first, no duplicates.
func TestGraphLanguageSpellings(t *testing.T) {
	tests := []struct {
		language string
		want     []string
	}{
		{"go", []string{"go", "Go"}},
		{"Python", []string{"python", "Python"}},
		{"typescript", []string{"typescript", "Typescript", "tsx", "Tsx"}},
		{"tsx", []string{"typescript", "Typescript", "tsx", "Tsx"}},
		{"jsx", []string{"javascript", "Javascript", "jsx", "Jsx"}},
		{"csharp", []string{"csharp", "Csharp", "c_sharp", "C_sharp"}},
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			got := graphLanguageSpellings(tt.language)
			if !slices.Equal(got, tt.want) {
				t.Errorf("graphLanguageSpellings(%q) = %v, want %v", tt.language, got, tt.want)
			}
		})
	}
}
