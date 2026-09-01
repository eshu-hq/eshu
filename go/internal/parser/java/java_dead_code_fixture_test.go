// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathJavaComprehensiveDeadCodeFixture(t *testing.T) {
	t.Parallel()

	repoRoot := javaFixturePath(t, "ecosystems", "java_comprehensive")
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	sourcePath := filepath.Join(repoRoot, "deadcode", "RuntimeEntrypoints.java")
	payload, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
	}

	classRef := assertJavaFunctionCallByNameAndKind(t, payload, "PluginImpl", "java.reflection_class_reference")
	parsertest.AssertStringFieldValue(t, classRef, "reflected_class", "comprehensive.deadcode.PluginImpl")
	methodRef := assertJavaFunctionCallByNameAndKind(t, payload, "run", "java.reflection_method_reference")
	parsertest.AssertStringFieldValue(t, methodRef, "inferred_obj_type", "PluginImpl")
	parsertest.AssertIntFieldValue(t, methodRef, "argument_count", 1)
	parsertest.AssertStringSliceEquals(t, methodRef, "argument_types", []string{"String"})
	parsertest.AssertStringSliceContains(
		t,
		parsertest.AssertFunctionByNameAndClass(t, payload, "readObject", "SerializationHooks"),
		"dead_code_root_kinds",
		"java.serialization_hook_method",
	)
	parsertest.AssertStringSliceContains(
		t,
		parsertest.AssertFunctionByNameAndClass(t, payload, "writeExternal", "ExternalizedState"),
		"dead_code_root_kinds",
		"java.externalizable_hook_method",
	)
}

func TestDefaultEngineParsePathJavaComprehensiveMetadataFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := javaFixturePath(t, "ecosystems", "java_comprehensive")
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
			path: filepath.Join(repoRoot, "META-INF", "services", "comprehensive.deadcode.Plugin"),
			name: "PluginImpl",
			kind: "java.service_loader_provider",
			full: "comprehensive.deadcode.PluginImpl",
		},
		{
			path: filepath.Join(repoRoot, "META-INF", "spring", "org.springframework.boot.autoconfigure.AutoConfiguration.imports"),
			name: "PluginAutoConfiguration",
			kind: "java.spring_autoconfiguration_class",
			full: "comprehensive.deadcode.PluginAutoConfiguration",
		},
		{
			path: filepath.Join(repoRoot, "META-INF", "spring.factories"),
			name: "LegacyAutoConfiguration",
			kind: "java.spring_autoconfiguration_class",
			full: "comprehensive.deadcode.LegacyAutoConfiguration",
		},
	} {
		payload, err := engine.ParsePath(repoRoot, tc.path, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", tc.path, err)
		}
		ref := assertJavaFunctionCallByNameAndKind(t, payload, tc.name, tc.kind)
		parsertest.AssertStringFieldValue(t, ref, "full_name", tc.full)
		parsertest.AssertStringFieldValue(t, ref, "referenced_class", tc.full)
	}
}
