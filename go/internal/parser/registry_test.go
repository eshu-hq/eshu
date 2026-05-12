package parser

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDefaultRegistryLookupByExtensionAndPath(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()

	t.Run("typescript jsx extension", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByExtension(".tsx")
		if !ok {
			t.Fatalf("expected .tsx to resolve")
		}
		if definition.ParserKey != "tsx" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "tsx")
		}
		if definition.Language != "tsx" {
			t.Fatalf("Language = %q, want %q", definition.Language, "tsx")
		}
	})

	t.Run("raw text extension", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByExtension(".j2")
		if !ok {
			t.Fatalf("expected .j2 to resolve")
		}
		if definition.ParserKey != "raw_text" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "raw_text")
		}
		if definition.Language != "raw_text" {
			t.Fatalf("Language = %q, want %q", definition.Language, "raw_text")
		}
	})

	t.Run("dockerfile basename", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("infra", "Dockerfile"))
		if !ok {
			t.Fatalf("expected Dockerfile to resolve")
		}
		if definition.ParserKey != "__dockerfile__" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "__dockerfile__")
		}
		if definition.Language != "dockerfile" {
			t.Fatalf("Language = %q, want %q", definition.Language, "dockerfile")
		}
	})

	t.Run("dockerfile prefix", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("infra", "Dockerfile.prod"))
		if !ok {
			t.Fatalf("expected Dockerfile.prod to resolve")
		}
		if definition.ParserKey != "__dockerfile__" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "__dockerfile__")
		}
	})

	t.Run("jenkinsfile basename", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("ci", "Jenkinsfile"))
		if !ok {
			t.Fatalf("expected Jenkinsfile to resolve")
		}
		if definition.ParserKey != "__jenkinsfile__" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "__jenkinsfile__")
		}
		if definition.Language != "groovy" {
			t.Fatalf("Language = %q, want %q", definition.Language, "groovy")
		}
	})

	t.Run("jenkinsfile prefix", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("ci", "Jenkinsfile.release"))
		if !ok {
			t.Fatalf("expected Jenkinsfile.release to resolve")
		}
		if definition.ParserKey != "__jenkinsfile__" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "__jenkinsfile__")
		}
	})

	t.Run("terraform tfvars extension", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("infra", "terraform.tfvars"))
		if !ok {
			t.Fatalf("expected terraform.tfvars to resolve")
		}
		if definition.ParserKey != "hcl" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "hcl")
		}
		if definition.Language != "hcl" {
			t.Fatalf("Language = %q, want %q", definition.Language, "hcl")
		}
	})

	t.Run("terraform tfvars json extension", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("infra", "terraform.tfvars.json"))
		if !ok {
			t.Fatalf("expected terraform.tfvars.json to resolve")
		}
		if definition.ParserKey != "hcl" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "hcl")
		}
		if definition.Language != "hcl" {
			t.Fatalf("Language = %q, want %q", definition.Language, "hcl")
		}
	})

	t.Run("jsonc extension", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("apps", "studio", "turbo.jsonc"))
		if !ok {
			t.Fatalf("expected turbo.jsonc to resolve")
		}
		if definition.ParserKey != "json" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "json")
		}
		if definition.Language != "json" {
			t.Fatalf("Language = %q, want %q", definition.Language, "json")
		}
	})
}

func TestRegistryOrderingAndImmutability(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()

	parserKeys := registry.ParserKeys()
	if !slices.IsSorted(parserKeys) {
		t.Fatalf("parser keys are not sorted: %v", parserKeys)
	}

	extensions := registry.Extensions()
	if !slices.IsSorted(extensions) {
		t.Fatalf("extensions are not sorted: %v", extensions)
	}

	definitions := registry.Definitions()
	if len(definitions) == 0 {
		t.Fatal("expected default registry to contain definitions")
	}
	keys := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		keys = append(keys, definition.ParserKey)
	}
	if !slices.IsSorted(keys) {
		t.Fatalf("definitions are not sorted by parser key: %v", keys)
	}

	for i := range definitions {
		if len(definitions[i].Extensions) == 0 {
			continue
		}
		definitions[i].Extensions[0] = ".mutated"
		break
	}
	reloaded, ok := registry.LookupByExtension(".py")
	if !ok {
		t.Fatal("expected .py to resolve after slice mutation")
	}
	if reloaded.ParserKey != "python" {
		t.Fatalf("ParserKey = %q, want %q", reloaded.ParserKey, "python")
	}
}

func TestNewRegistryRejectsDuplicateDefinitions(t *testing.T) {
	t.Parallel()

	t.Run("duplicate parser key", func(t *testing.T) {
		t.Parallel()

		_, err := NewRegistry([]Definition{
			{
				ParserKey:  "go",
				Language:   "go",
				Extensions: []string{".go"},
			},
			{
				ParserKey:  "go",
				Language:   "go",
				Extensions: []string{".golang"},
			},
		})
		if err == nil {
			t.Fatal("expected duplicate parser key error")
		}
	})

	t.Run("duplicate extension", func(t *testing.T) {
		t.Parallel()

		_, err := NewRegistry([]Definition{
			{
				ParserKey:  "go",
				Language:   "go",
				Extensions: []string{".go"},
			},
			{
				ParserKey:  "rust",
				Language:   "rust",
				Extensions: []string{".go"},
			},
		})
		if err == nil {
			t.Fatal("expected duplicate extension error")
		}
	})
}
