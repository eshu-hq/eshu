// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// versionPinNames is the explicit allow-list of runtime-version pin files the
// raw_text family admits (#8809). It is deliberately a name list, not a
// "*-version" rule.
var versionPinNames = []string{
	".nvmrc",
	".node-version",
	".tool-versions",
	".python-version",
	".ruby-version",
	".java-version",
	".go-version",
	".sdkmanrc",
	".terraform-version",
	".perl-version",
	"rust-toolchain",
	"rust-toolchain.toml",
	".swift-version",
}

func TestDefaultRegistryAdmitsVersionPinFilesAsRawText(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, name := range versionPinNames {
		for _, path := range []string{name, filepath.Join("services", "api", name)} {
			definition, ok := registry.LookupByPath(path)
			if !ok {
				t.Fatalf("LookupByPath(%q) = false, want raw_text", path)
			}
			if definition.ParserKey != "raw_text" || definition.Language != "raw_text" {
				t.Fatalf("LookupByPath(%q) = %q/%q, want raw_text/raw_text",
					path, definition.ParserKey, definition.Language)
			}
		}
	}
}

// TestDefaultRegistryKeepsCredentialBearingDotfilesOut pins the exclusion list:
// the content store has no write-time redaction, so files that can carry
// credentials or literal environment values must not resolve. .yarnrc.yml is
// absent on purpose: the generic .yml extension already admits it as YAML, a
// pre-existing path this change does not touch.
func TestDefaultRegistryKeepsCredentialBearingDotfilesOut(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, name := range []string{
		".pypirc", ".netrc",
		".env", ".env.local", ".env.production",
		"mise.toml", ".mise.toml",
		".something-version", "node-version", ".nvmrc.bak",
		"swift-version", ".swift-version.bak", ".swift-versions", ".swiftversion",
	} {
		if definition, ok := registry.LookupByPath(name); ok {
			t.Fatalf("LookupByPath(%q) resolved to %q, want no match", name, definition.ParserKey)
		}
	}
}
