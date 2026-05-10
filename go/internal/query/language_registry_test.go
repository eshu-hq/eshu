package query

import "testing"

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
	if len(types) != 38 {
		t.Errorf("expected 38 supported entity types, got %d: %v", len(types), types)
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
		"sql_table": true, "sql_view": true, "sql_function": true,
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
}

func TestBuildExtensionFilter(t *testing.T) {
	tests := []struct {
		name string
		exts []string
		want string
	}{
		{"empty", nil, ""},
		{"single", []string{".go"}, " OR f.name ENDS WITH '.go'"},
		{"multiple", []string{".py", ".pyi"}, " OR f.name ENDS WITH '.py' OR f.name ENDS WITH '.pyi'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildExtensionFilter(tt.exts)
			if got != tt.want {
				t.Errorf("buildExtensionFilter(%v) = %q, want %q", tt.exts, got, tt.want)
			}
		})
	}
}
