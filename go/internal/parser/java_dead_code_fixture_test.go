package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaComprehensiveDeadCodeFixture(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "java_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	sourcePath := filepath.Join(repoRoot, "deadcode", "RuntimeEntrypoints.java")
	payload, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
	}

	classRef := assertJavaFunctionCallByNameAndKind(t, payload, "PluginImpl", "java.reflection_class_reference")
	assertStringFieldValue(t, classRef, "reflected_class", "comprehensive.deadcode.PluginImpl")
	methodRef := assertJavaFunctionCallByNameAndKind(t, payload, "run", "java.reflection_method_reference")
	assertStringFieldValue(t, methodRef, "inferred_obj_type", "PluginImpl")
	assertIntFieldValue(t, methodRef, "argument_count", 1)
	assertParserStringSliceFieldValue(t, methodRef, "argument_types", []string{"String"})
	assertParserStringSliceContains(
		t,
		assertFunctionByNameAndClass(t, payload, "readObject", "SerializationHooks"),
		"dead_code_root_kinds",
		"java.serialization_hook_method",
	)
	assertParserStringSliceContains(
		t,
		assertFunctionByNameAndClass(t, payload, "writeExternal", "ExternalizedState"),
		"dead_code_root_kinds",
		"java.externalizable_hook_method",
	)
}

func TestDefaultEngineParsePathJavaComprehensiveMetadataFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "java_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
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
		payload, err := engine.ParsePath(repoRoot, tc.path, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", tc.path, err)
		}
		ref := assertJavaFunctionCallByNameAndKind(t, payload, tc.name, tc.kind)
		assertStringFieldValue(t, ref, "full_name", tc.full)
		assertStringFieldValue(t, ref, "referenced_class", tc.full)
	}
}
