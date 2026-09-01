// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultRegistryLookupByPathJavaMetadata(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	paths := []string{
		filepath.Join("META-INF", "services", "com.example.Plugin"),
		filepath.Join("src", "main", "resources", "META-INF", "services", "com.example.Plugin"),
		filepath.Join("src", "main", "resources", "META-INF", "spring", "org.springframework.boot.autoconfigure.AutoConfiguration.imports"),
		filepath.Join("META-INF", "spring.factories"),
		filepath.Join("src", "main", "resources", "META-INF", "spring.factories"),
	}
	for _, path := range paths {
		definition, ok := registry.LookupByPath(path)
		if !ok {
			t.Fatalf("LookupByPath(%q) ok = false, want true", path)
		}
		if got, want := definition.Language, "java_metadata"; got != want {
			t.Fatalf("LookupByPath(%q).Language = %q, want %q", path, got, want)
		}
	}
}

func TestDefaultEngineParsePathJavaMetadataEmitsStaticClassReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	servicesPath := filepath.Join(repoRoot, "src", "main", "resources", "META-INF", "services", "com.example.Plugin")
	writeJavaTestFile(t, servicesPath, `# service implementations
com.example.PluginImpl
com.example.PluginImpl # duplicate
`)
	importsPath := filepath.Join(repoRoot, "src", "main", "resources", "META-INF", "spring", "org.springframework.boot.autoconfigure.AutoConfiguration.imports")
	writeJavaTestFile(t, importsPath, `com.example.AutoConfig
`)
	factoriesPath := filepath.Join(repoRoot, "src", "main", "resources", "META-INF", "spring.factories")
	writeJavaTestFile(t, factoriesPath, `org.springframework.boot.autoconfigure.EnableAutoConfiguration=\
com.example.LegacyAutoConfig,\
com.example.MoreAutoConfig
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	for _, tc := range []struct {
		path string
		name string
		kind string
		full string
	}{
		{
			path: servicesPath,
			name: "PluginImpl",
			kind: "java.service_loader_provider",
			full: "com.example.PluginImpl",
		},
		{
			path: importsPath,
			name: "AutoConfig",
			kind: "java.spring_autoconfiguration_class",
			full: "com.example.AutoConfig",
		},
		{
			path: factoriesPath,
			name: "LegacyAutoConfig",
			kind: "java.spring_autoconfiguration_class",
			full: "com.example.LegacyAutoConfig",
		},
		{
			path: factoriesPath,
			name: "MoreAutoConfig",
			kind: "java.spring_autoconfiguration_class",
			full: "com.example.MoreAutoConfig",
		},
	} {
		got, err := engine.ParsePath(repoRoot, tc.path, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", tc.path, err)
		}
		if gotLang, wantLang := got["lang"], "java_metadata"; gotLang != wantLang {
			t.Fatalf("ParsePath(%q) lang = %#v, want %#v", tc.path, gotLang, wantLang)
		}
		ref := assertJavaFunctionCallByNameAndKind(t, got, tc.name, tc.kind)
		parsertest.AssertStringFieldValue(t, ref, "full_name", tc.full)
		parsertest.AssertStringFieldValue(t, ref, "referenced_class", tc.full)
	}
}
