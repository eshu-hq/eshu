// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	t.Run("go.mod exact name", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("repo", "go.mod"))
		if !ok {
			t.Fatalf("expected go.mod to resolve")
		}
		if definition.ParserKey != "gomod" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "gomod")
		}
		if definition.Language != "gomod" {
			t.Fatalf("Language = %q, want %q so go-source per-file parsing stays distinct from per-module manifest parsing", definition.Language, "gomod")
		}
	})

	t.Run("go.sum exact name", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("repo", "go.sum"))
		if !ok {
			t.Fatalf("expected go.sum to resolve")
		}
		if definition.ParserKey != "gomod" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "gomod")
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

	t.Run("bundler gemfile basename", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("ruby", "Gemfile"))
		if !ok {
			t.Fatalf("expected Gemfile to resolve")
		}
		if definition.ParserKey != "ruby" {
			t.Fatalf("ParserKey = %q, want ruby", definition.ParserKey)
		}
		if definition.Language != "ruby" {
			t.Fatalf("Language = %q, want ruby", definition.Language)
		}
	})

	t.Run("bundler lockfile basename", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("ruby", "Gemfile.lock"))
		if !ok {
			t.Fatalf("expected Gemfile.lock to resolve")
		}
		if definition.ParserKey != "ruby" {
			t.Fatalf("ParserKey = %q, want ruby", definition.ParserKey)
		}
		if definition.Language != "ruby" {
			t.Fatalf("Language = %q, want ruby", definition.Language)
		}
	})

	t.Run("yarn lockfile basename", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("apps", "web", "yarn.lock"))
		if !ok {
			t.Fatalf("expected yarn.lock to resolve")
		}
		if definition.ParserKey != "node_lockfile" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "node_lockfile")
		}
		if definition.Language != "node_lockfile" {
			t.Fatalf("Language = %q, want %q", definition.Language, "node_lockfile")
		}
	})

	t.Run("pnpm lockfile basename takes priority over .yaml extension", func(t *testing.T) {
		t.Parallel()

		// .yaml extension would otherwise route to the YAML parser; the
		// exact-name registration for pnpm-lock.yaml must win.
		definition, ok := registry.LookupByPath(filepath.Join("apps", "web", "pnpm-lock.yaml"))
		if !ok {
			t.Fatalf("expected pnpm-lock.yaml to resolve")
		}
		if definition.ParserKey != "node_lockfile" {
			t.Fatalf("ParserKey = %q, want %q (yaml extension should not win for pnpm-lock.yaml)", definition.ParserKey, "node_lockfile")
		}
	})

	t.Run("pnpm lockfile yml alias", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("apps", "web", "pnpm-lock.yml"))
		if !ok {
			t.Fatalf("expected pnpm-lock.yml to resolve")
		}
		if definition.ParserKey != "node_lockfile" {
			t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, "node_lockfile")
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

func TestRegistryLanguagesReturnsSortedDedupedLanguages(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	langs := registry.Languages()

	if len(langs) == 0 {
		t.Fatal("Languages() returned empty slice, want at least one language")
	}
	// Must be sorted.
	for i := 1; i < len(langs); i++ {
		if langs[i] < langs[i-1] {
			t.Fatalf("Languages() not sorted: %q < %q at index %d", langs[i], langs[i-1], i)
		}
	}
	// Must be deduplicated.
	seen := make(map[string]int, len(langs))
	for _, lang := range langs {
		seen[lang]++
	}
	for lang, count := range seen {
		if count > 1 {
			t.Fatalf("Languages() contains duplicate %q (%d times)", lang, count)
		}
	}
	// Must contain the well-known canonical languages.
	for _, want := range []string{"go", "python", "typescript", "java", "hcl"} {
		if !slices.Contains(langs, want) {
			t.Errorf("Languages() missing well-known language %q", want)
		}
	}
}

func TestRegistryIsRegisteredLanguage(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()

	for _, lang := range []string{"go", "python", "typescript", "rust", "yaml", "dockerfile"} {
		if !registry.IsRegisteredLanguage(lang) {
			t.Errorf("IsRegisteredLanguage(%q) = false, want true", lang)
		}
	}

	for _, lang := range []string{"", "unknown_lang", "Go", "PYTHON", "javascript_extra"} {
		if registry.IsRegisteredLanguage(lang) {
			t.Errorf("IsRegisteredLanguage(%q) = true, want false", lang)
		}
	}
}
