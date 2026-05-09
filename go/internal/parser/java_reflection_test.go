package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaEmitsLiteralReflectionReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/ReflectionBootstrap.java")
	writeTestFile(t, filePath, `package example;

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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	classRef := assertJavaFunctionCallByNameAndKind(t, got, "Plugin", "java.reflection_class_reference")
	assertStringFieldValue(t, classRef, "reflected_class", "example.Plugin")

	methodRef := assertJavaFunctionCallByNameAndKind(t, got, "run", "java.reflection_method_reference")
	assertStringFieldValue(t, methodRef, "inferred_obj_type", "Plugin")
	assertIntFieldValue(t, methodRef, "argument_count", 1)
	assertParserStringSliceFieldValue(t, methodRef, "argument_types", []string{"String"})
}

func TestDefaultEngineParsePathJavaIgnoresDynamicReflectionStrings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/ReflectionBootstrap.java")
	writeTestFile(t, filePath, `package example;

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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
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
		itemName, _ := item["name"].(string)
		callKind, _ := item["call_kind"].(string)
		if itemName == name && callKind == kind {
			t.Fatalf("unexpected function_call name %q with call_kind %q in %#v", name, kind, items)
		}
	}
}
