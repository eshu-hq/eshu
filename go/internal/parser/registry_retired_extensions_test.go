// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultRegistryClaimsRetiredMapExtensions pins the three extensions the
// retired query-side languageFileExtensions map listed but no parser claimed
// (#6578). Discovery drops an unclaimed path before a File is written, so an
// unregistered extension never reaches a language query at all.
func TestDefaultRegistryClaimsRetiredMapExtensions(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()

	cases := []struct {
		extension string
		language  string
	}{
		{extension: ".hxx", language: "cpp"},
		{extension: ".kts", language: "kotlin"},
		{extension: ".pyi", language: "python"},
	}
	for _, tc := range cases {
		t.Run(tc.extension, func(t *testing.T) {
			t.Parallel()

			definition, ok := registry.LookupByExtension(tc.extension)
			if !ok {
				t.Fatalf("expected %s to resolve", tc.extension)
			}
			if definition.Language != tc.language {
				t.Fatalf("Language = %q, want %q", definition.Language, tc.language)
			}
			if definition.ParserKey != tc.language {
				t.Fatalf("ParserKey = %q, want %q", definition.ParserKey, tc.language)
			}
		})
	}

	t.Run("build.gradle.kts keeps the gradle engine by exact name", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("app", "build.gradle.kts"))
		if !ok {
			t.Fatalf("expected build.gradle.kts to resolve")
		}
		if definition.Language != "gradle" {
			t.Fatalf("Language = %q, want %q", definition.Language, "gradle")
		}
	})

	t.Run("settings.gradle.kts is kotlin by extension", func(t *testing.T) {
		t.Parallel()

		definition, ok := registry.LookupByPath(filepath.Join("app", "settings.gradle.kts"))
		if !ok {
			t.Fatalf("expected settings.gradle.kts to resolve")
		}
		if definition.Language != "kotlin" {
			t.Fatalf("Language = %q, want %q", definition.Language, "kotlin")
		}
	})

	t.Run("literate haskell stays unregistered", func(t *testing.T) {
		t.Parallel()

		if _, ok := registry.LookupByExtension(".lhs"); ok {
			t.Fatalf("expected .lhs to stay unregistered")
		}
	})
}
