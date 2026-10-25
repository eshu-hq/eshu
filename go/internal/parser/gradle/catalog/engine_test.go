// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestDefaultRegistryBindsGradleVersionCatalogByBaseName pins the #8822 name:
// the catalog binds by exact base name to the existing gradle definition, in
// any directory and any letter case, so no path rule and no forced content
// epoch advance is needed. A catalog under another file name
// is not claimed.
func TestDefaultRegistryBindsGradleVersionCatalogByBaseName(t *testing.T) {
	t.Parallel()
	registry := parser.DefaultRegistry()
	for _, path := range []string{
		"libs.versions.toml",
		"gradle/libs.versions.toml",
		"app/gradle/LIBS.VERSIONS.TOML",
	} {
		definition, ok := registry.LookupByPath(path)
		if !ok || definition.ParserKey != "gradle" || definition.Admission != parser.AdmissionStructuredFact {
			t.Errorf("LookupByPath(%q) = %q admission %q ok=%v, want gradle at L2", path, definition.ParserKey, definition.Admission, ok)
		}
	}
	for _, path := range []string{"gradle/deps.versions.toml", "versions.toml", "gradle/libs.versions.toml.bak"} {
		if definition, ok := registry.LookupByPath(path); ok && definition.ParserKey == "gradle" {
			t.Errorf("LookupByPath(%q) bound the gradle parser; only libs.versions.toml is claimed", path)
		}
	}
}

// TestParsePathGradleVersionCatalogDispatchesToCatalogParser proves the engine
// hands libs.versions.toml to the catalog parser and keeps build scripts on the
// build-script parser, with the state envelope on the catalog payload only.
func TestParsePathGradleVersionCatalogDispatchesToCatalogParser(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	catalogPath := filepath.Join(repoRoot, "gradle", "libs.versions.toml")
	buildPath := filepath.Join(repoRoot, "build.gradle")
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := "[versions]\nspring = \"6.1.4\"\n[libraries]\nspring-core = { module = \"org.springframework:spring-core\", version.ref = \"spring\" }\nfloating = { module = \"g:a\", version.ref = \"missing\" }\n"
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildPath, []byte("dependencies {\n    implementation 'junit:junit:4.13.2'\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v", err)
	}

	payload, err := engine.ParsePath(repoRoot, catalogPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(catalog) error = %v", err)
	}
	if payload["lang"] != "gradle" {
		t.Fatalf("lang = %v, want gradle", payload["lang"])
	}
	rows, _ := payload["variables"].([]map[string]any)
	if len(rows) != 2 || rows[0]["value"] != "6.1.4" || rows[0]["section"] != "libraries" {
		t.Fatalf("catalog rows = %#v", rows)
	}
	if rows[1]["dependency_resolution_state"] != "unresolved" || rows[1]["value"] != "" {
		t.Fatalf("unresolved reference row = %#v, want unresolved with no value", rows[1])
	}
	state, _ := payload["gradle_catalog_state"].(map[string]any)
	if state["state"] != "parsed" || state["unresolved_count"] != 1 {
		t.Fatalf("gradle_catalog_state = %#v", state)
	}

	script, err := engine.ParsePath(repoRoot, buildPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(build.gradle) error = %v", err)
	}
	if _, has := script["gradle_catalog_state"]; has {
		t.Fatal("build.gradle payload carries gradle_catalog_state")
	}
	scriptRows, _ := script["variables"].([]map[string]any)
	if len(scriptRows) != 1 || scriptRows[0]["section"] != "implementation" {
		t.Fatalf("build.gradle rows = %#v", scriptRows)
	}
}
