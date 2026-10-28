// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taxonomy

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestCov7ContentEntityTypeForResolve proves the resolve → graph → identity
// fallback chain behind entity-type resolution.
func TestCov7ContentEntityTypeForResolve(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		want string
	}{
		{"empty", ""},
		{"resolve map", "Annotation"},
		{"graph map", "Repository"},
		{"graph function", "Function"},
		{"unknown identity", "mystery_type"},
	}
	inputs := map[string]string{
		"empty": "", "resolve map": "annotation", "graph map": "repository",
		"graph function": "function", "unknown identity": "mystery_type",
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ContentEntityTypeForResolve(inputs[tc.name]); got != tc.want {
				t.Fatalf("ContentEntityTypeForResolve(%q) = %q, want %q", inputs[tc.name], got, tc.want)
			}
		})
	}
}

// TestCov7ElixirGraphSemanticEntityType proves the Elixir semantic lookup
// resolves its triple and rejects unknown types.
func TestCov7ElixirGraphSemanticEntityType(t *testing.T) {
	t.Parallel()

	label, key, value, ok := ElixirGraphSemanticEntityType("guard")
	if !ok || label != "Function" || key != "semantic_kind" || value != "guard" {
		t.Fatalf("guard = (%q, %q, %q, %v), want (Function, semantic_kind, guard, true)", label, key, value, ok)
	}
	label, key, value, ok = ElixirGraphSemanticEntityType("protocol_implementation")
	if !ok || label != "Module" || key != "module_kind" || value != "protocol_implementation" {
		t.Fatalf("protocol_implementation = (%q, %q, %q, %v), want module triple", label, key, value, ok)
	}
	if _, _, _, ok := ElixirGraphSemanticEntityType("plain_function"); ok {
		t.Fatalf("plain_function = ok, want false")
	}
}

// TestCov7GraphLabelToContentEntityType proves each label family maps to
// itself and unknown labels map to empty.
func TestCov7GraphLabelToContentEntityType(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"Annotation", "Function", "Variable", "SqlTable", "TerraformModule", "TerragruntConfig", "FluxHelmRelease"} {
		if got := GraphLabelToContentEntityType(label); got != label {
			t.Fatalf("GraphLabelToContentEntityType(%q) = %q, want identity", label, got)
		}
	}
	for _, label := range []string{"Repository", "NoSuchLabel", ""} {
		if got := GraphLabelToContentEntityType(label); got != "" {
			t.Fatalf("GraphLabelToContentEntityType(%q) = %q, want empty", label, got)
		}
	}
}

// TestCov7CanonicalLanguage proves trimming, lowercasing, and alias
// resolution to the canonical spelling.
func TestCov7CanonicalLanguage(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		" TSX ": "typescript", "JSX": "javascript", " Go ": "go", "": "",
		"C_SHARP": "c_sharp",
	}
	for input, want := range cases {
		if got := CanonicalLanguage(input); got != want {
			t.Fatalf("CanonicalLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestCov7NormalizedLanguageVariants proves alias variants expand while
// other languages stay singletons.
func TestCov7NormalizedLanguageVariants(t *testing.T) {
	t.Parallel()

	javascript := NormalizedLanguageVariants("JavaScript")
	if len(javascript) != 2 || javascript[0] != "javascript" || javascript[1] != "jsx" {
		t.Fatalf("javascript variants = %#v, want [javascript jsx]", javascript)
	}
	typescript := NormalizedLanguageVariants("tsx")
	if len(typescript) != 2 || typescript[1] != "tsx" {
		t.Fatalf("tsx variants = %#v, want [typescript tsx]", typescript)
	}
	csharp := NormalizedLanguageVariants("csharp")
	if len(csharp) != 2 || csharp[1] != "c_sharp" {
		t.Fatalf("csharp variants = %#v, want [csharp c_sharp]", csharp)
	}
	goVariants := NormalizedLanguageVariants("go")
	if len(goVariants) != 1 || goVariants[0] != "go" {
		t.Fatalf("go variants = %#v, want [go]", goVariants)
	}
}

// TestCov7CoverageLanguageMaps proves the coverage wire shape and the empty
// non-nil contract.
func TestCov7CoverageLanguageMaps(t *testing.T) {
	t.Parallel()

	if got := CoverageLanguageMaps(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil languages = %#v, want empty non-nil", got)
	}
	got := CoverageLanguageMaps([]querycontract.RepositoryLanguageCount{
		{Language: "go", FileCount: 7},
		{Language: "python", FileCount: 3},
	})
	if len(got) != 2 || got[0]["language"] != "go" || got[0]["file_count"] != 7 || got[1]["file_count"] != 3 {
		t.Fatalf("language maps = %#v, want language/file_count rows", got)
	}
}

// TestCov7GraphResultMetadataProjection proves present fields project (with
// repo-name renaming and slice retyping) while absent and zero values stay
// out, and an empty row yields nil.
func TestCov7GraphResultMetadataProjection(t *testing.T) {
	t.Parallel()

	if got := GraphResultMetadata(map[string]any{}); got != nil {
		t.Fatalf("empty row = %#v, want nil", got)
	}
	row := map[string]any{
		"docstring":                "does things",
		"class_context":            "Svc",
		"method_kind":              "handler",
		"constructor_kind":         "new",
		"annotation_kind":          "route",
		"context":                  "ctx",
		"type_annotation_count":    2,
		"type_annotation_kinds":    []any{"param"},
		"type_parameters":          []any{"T"},
		"dead_code_root_kinds":     []any{"entry"},
		"type_alias_kind":          "alias",
		"framework":                "gin",
		"module_kind":              "mod",
		"jsx_fragment_shorthand":   true,
		"component_type_assertion": "assert",
		"component_wrapper_kind":   "wrap",
		"protocol":                 "http",
		"implemented_for":          "svc",
		"attribute_kind":           "attr",
		"value":                    "v",
		"declaration_merge_group":  "g",
		"declaration_merge_count":  3,
		"declaration_merge_kinds":  []any{"k"},
		"kind":                     "function",
		"target_kind":              "target",
		"type":                     "func()",
		"decorators":               []any{"d"},
		"async":                    true,
		"semantic_kind":            "guard",
		"metaclass":                "meta",
		"source":                   "src",
		"terraform_source":         "tsrc",
		"config_path":              "cfg",
		"includes":                 []any{"i"},
		"inputs":                   []any{"in"},
		"locals":                   []any{"l"},
		"deployment_name":          "dep",
		"entity_repo_name":         "repo",
		"create_deploy":            "cd",
		"cluster_name":             "cluster",
		"zone_id":                  "zone",
		"deploy_entry_point":       "main",
		"qualified_name":           "q",
		"sql_entity_type":          "table",
		"schema":                   "public",
		"data_type":                "int",
		"table_name":               "t",
		"column_name":              "c",
		"routine_kind":             "proc",
		"function_language":        "sql",
		"unrelated":                "dropped",
	}
	got := GraphResultMetadata(row)
	if len(got) != 50 {
		t.Fatalf("projected keys = %d, want 50 (every known field, unrelated dropped)", len(got))
	}
	if got["repo_name"] != "repo" {
		t.Fatalf("repo_name = %#v, want renamed entity_repo_name", got["repo_name"])
	}
	if kinds, ok := got["type_annotation_kinds"].([]any); !ok || len(kinds) != 1 || kinds[0] != "param" {
		t.Fatalf("type_annotation_kinds = %#v, want retyped slice", got["type_annotation_kinds"])
	}
	if got["jsx_fragment_shorthand"] != true || got["async"] != true {
		t.Fatalf("bools = %#v, want carried booleans", got)
	}
	if got["type_annotation_count"] != 2 || got["declaration_merge_count"] != 3 {
		t.Fatalf("counts = %#v, want positive ints", got)
	}
	if _, ok := got["unrelated"]; ok {
		t.Fatalf("unrelated key projected, want dropped")
	}

	zero := GraphResultMetadata(map[string]any{
		"docstring":             "",
		"type_annotation_count": 0,
		"type_annotation_kinds": []any{},
		"async":                 false,
	})
	// async:false is a present boolean, so it projects; everything else drops.
	if len(zero) != 1 || zero["async"] != false {
		t.Fatalf("zero row = %#v, want only the present boolean", zero)
	}
}

// TestCov7LanguageResultKeys proves the match-key shape and content-type
// resolution from graph labels.
func TestCov7LanguageResultKeys(t *testing.T) {
	t.Parallel()

	if got := LanguageResultMatchKey("a.go", "function", "Foo", 12); got != "a.go|function|Foo|12" {
		t.Fatalf("match key = %q, want pipe-joined shape", got)
	}
	if got := ResultContentEntityType(map[string]any{"labels": []any{"Nope", "SqlView"}}); got != "SqlView" {
		t.Fatalf("content type = %q, want first known label", got)
	}
	if got := ResultContentEntityType(map[string]any{"labels": []any{"Nope"}}); got != "" {
		t.Fatalf("unknown labels = %q, want empty", got)
	}
	if got := ResultContentEntityType(map[string]any{}); got != "" {
		t.Fatalf("missing labels = %q, want empty", got)
	}
}
