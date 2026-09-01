// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathJavaEmitsLiteralReflectionReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/ReflectionBootstrap.java")
	writeJavaTestFile(t, filePath, `package example;

public class ReflectionBootstrap {
    public void bootstrap() throws Exception {
        Class.forName("example.Plugin");
        ClassLoader loader = Thread.currentThread().getContextClassLoader();
        loader.loadClass("example.Plugin");
        Plugin.class.getDeclaredMethod("run", String.class);
    }
}

final class Plugin {
    void run(String value) {
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classRef := assertJavaFunctionCallByNameAndKind(t, got, "Plugin", "java.reflection_class_reference")
	parsertest.AssertStringFieldValue(t, classRef, "reflected_class", "example.Plugin")

	methodRef := assertJavaFunctionCallByNameAndKind(t, got, "run", "java.reflection_method_reference")
	parsertest.AssertStringFieldValue(t, methodRef, "inferred_obj_type", "Plugin")
	parsertest.AssertIntFieldValue(t, methodRef, "argument_count", 1)
	parsertest.AssertStringSliceEquals(t, methodRef, "argument_types", []string{"String"})
}

func TestDefaultEngineParsePathJavaIgnoresDynamicReflectionStrings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/ReflectionBootstrap.java")
	writeJavaTestFile(t, filePath, `package example;

public class ReflectionBootstrap {
    public void bootstrap(String className, String methodName) throws Exception {
        Class.forName(className);
        Plugin.class.getMethod(methodName);
    }
}

final class Plugin {
    void run() {
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertJavaNoFunctionCallByNameAndKind(t, got, "Plugin", "java.reflection_class_reference")
	assertJavaNoFunctionCallByNameAndKind(t, got, "run", "java.reflection_method_reference")
}

func assertJavaNoFunctionCallByNameAndKind(t *testing.T, payload map[string]any, name string, kind string) {
	t.Helper()

	items, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", payload["function_calls"])
	}
	for _, item := range items {
		itemName := negativeAssertionStringField(t, item, "name")
		callKind := negativeAssertionStringField(t, item, "call_kind")
		if itemName == name && callKind == kind {
			t.Fatalf("unexpected function_call name %q with call_kind %q in %#v", name, kind, items)
		}
	}
}

// negativeAssertionStringField returns item[field] as a string, or "" when the
// field is absent. A present field of another type fails the test: a
// wrongly-typed name or call_kind would otherwise read as "" and let the
// negative assertion above pass over a drifted payload.
func negativeAssertionStringField(t *testing.T, item map[string]any, field string) string {
	t.Helper()

	raw, present := item[field]
	if !present {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		t.Fatalf("function_calls %s = %#v (%T), want string; a malformed row must not pass a negative assertion", field, raw, raw)
	}
	return value
}
