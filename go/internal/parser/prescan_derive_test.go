// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestDerivePreScanNamesMatchesLegacyPreScan pins issue #4764: for php,
// javascript, typescript, and tsx, DerivePreScanNames(payload) — extracted
// from the same parse payload the collector already computes — must return
// exactly the same name set the legacy tree-sitter PreScan pass returns for
// every fixture under tests/fixtures/ecosystems covering these languages,
// including PHP method_declaration/anonymous_class and JS pair/
// assignment_expression function-valued exports. json and groovy are
// out of scope (they keep their own prescan/parse relationship) and are
// asserted NOT to be derive-eligible.
func TestDerivePreScanNamesMatchesLegacyPreScan(t *testing.T) {
	t.Parallel()

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	registry := DefaultRegistry()

	fixtureDirs := []string{
		"php_comprehensive",
		"javascript_comprehensive",
		"typescript_comprehensive",
		"tsx_comprehensive",
	}

	root := repoRootForFixtures(t)
	var checked int
	for _, dir := range fixtureDirs {
		dirPath := filepath.Join(root, "tests", "fixtures", "ecosystems", dir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			t.Fatalf("ReadDir(%s) error = %v, want nil", dirPath, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dirPath, entry.Name())
			definition, ok := registry.LookupByPath(path)
			if !ok || !IsDerivedPreScanLanguage(definition.Language) {
				continue
			}
			checked++

			payload, err := engine.ParsePath(dirPath, path, false, Options{IndexSource: true, VariableScope: "all"})
			if err != nil {
				t.Fatalf("ParsePath(%s) error = %v, want nil", path, err)
			}
			derived := DerivePreScanNames(payload)

			legacy, err := engine.PreScanRepositoryPaths(dirPath, []string{path})
			if err != nil {
				t.Fatalf("PreScanRepositoryPaths(%s) error = %v, want nil", path, err)
			}
			var legacyNames []string
			for name := range legacy {
				legacyNames = append(legacyNames, name)
			}
			slices.Sort(legacyNames)
			slices.Sort(derived)

			if !slices.Equal(derived, legacyNames) {
				t.Fatalf(
					"%s: DerivePreScanNames() = %v, want (legacy PreScan) %v",
					path, derived, legacyNames,
				)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no derive-eligible fixtures were checked; fixture discovery is broken")
	}
}

// TestIsDerivedPreScanLanguageScope pins the exact #4764 language scope: only
// php, javascript, typescript, and tsx are derive-eligible. json and groovy
// (and every other prescan-covered language) MUST keep going through the
// legacy tree-sitter PreScan pass.
func TestIsDerivedPreScanLanguageScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		language string
		want     bool
	}{
		{"php", true},
		{"javascript", true},
		{"typescript", true},
		{"tsx", true},
		{"json", false},
		{"groovy", false},
		{"python", false},
		{"go", false},
		{"java", false},
	}
	for _, tt := range tests {
		if got := IsDerivedPreScanLanguage(tt.language); got != tt.want {
			t.Errorf("IsDerivedPreScanLanguage(%q) = %v, want %v", tt.language, got, tt.want)
		}
	}
}

// repoRootForFixtures walks upward from the working directory to find the
// repository root containing tests/fixtures/ecosystems, mirroring how other
// engine tests locate shared fixtures without a hardcoded absolute path.
func repoRootForFixtures(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v, want nil", err)
	}
	for {
		candidate := filepath.Join(dir, "tests", "fixtures", "ecosystems")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root containing tests/fixtures/ecosystems from %s", dir)
		}
		dir = parent
	}
}
