package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathSwiftEmitsBasesAndFunctionArgs(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "swift_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	classesPath := filepath.Join(repoRoot, "Classes.swift")
	payload, err := engine.ParsePath(repoRoot, classesPath, false, Options{IndexSource: true})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", classesPath, err)
	}

	assertSwiftTypeBases(t, payload, "Dog", []string{"Animal"})
	assertSwiftTypeBases(t, payload, "GuideDog", []string{"Dog"})
	assertSwiftFunctionArgs(t, payload, "fetch", []string{"item"})
	assertSwiftFunctionArgs(t, payload, "guide", []string{"destination"})
	assertSwiftFunctionSourcePresent(t, payload, "guide")
}

func TestDefaultEngineParsePathSwiftEmitsVariableContextAndTypeMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "swift_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	structsPath := filepath.Join(repoRoot, "Structs.swift")
	payload, err := engine.ParsePath(repoRoot, structsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", structsPath, err)
	}

	assertSwiftVariableMetadata(t, payload, "host", "String", "Config", "Config")
	assertSwiftVariableMetadata(t, payload, "port", "Int", "Config", "Config")
	assertSwiftVariableMetadata(t, payload, "width", "Double", "Size", "Size")
}

func TestDefaultEngineParsePathSwiftEmitsImportAndCallMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "swift_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	actorsPath := filepath.Join(repoRoot, "Actors.swift")
	actorsPayload, err := engine.ParsePath(repoRoot, actorsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", actorsPath, err)
	}

	assertSwiftImportMetadata(t, actorsPayload, "Foundation")
	assertSwiftCallMetadata(t, actorsPayload, "print", "print")

	enumsPath := filepath.Join(repoRoot, "Enums.swift")
	enumsPayload, err := engine.ParsePath(repoRoot, enumsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", enumsPath, err)
	}

	assertSwiftCallMetadata(t, enumsPayload, "transform", "transform")
}

func TestDefaultEngineParsePathSwiftInfersReceiverCallTypesAndEmitsProtocols(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "worker.swift")
	writeTestFile(
		t,
		filePath,
		`import Foundation

protocol Runnable {
    func run()
}

class Logger {
    func info(_ message: String) {}
}

class Worker: Runnable {
    let logger: Logger

    init(logger: Logger) {
        self.logger = logger
    }

    func run() {
        logger.info("running")
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	assertSwiftTypeBases(t, payload, "Worker", []string{"Runnable"})
	assertSwiftVariableMetadata(t, payload, "logger", "Logger", "Worker", "Worker")
	assertSwiftCallInferredType(t, payload, "info", "logger.info", "Logger")
	assertSwiftNamedBucketContains(t, payload, "protocols", "Runnable")
}

func assertSwiftTypeBases(
	t *testing.T,
	payload map[string]any,
	typeName string,
	wantBases []string,
) {
	t.Helper()

	items, ok := payload["classes"].([]map[string]any)
	if !ok {
		t.Fatalf("classes = %T, want []map[string]any", payload["classes"])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != typeName {
			continue
		}
		bases, _ := item["bases"].([]string)
		if len(bases) != len(wantBases) {
			t.Fatalf("bases for %q = %#v, want %#v", typeName, item["bases"], wantBases)
		}
		for index, wantBase := range wantBases {
			if bases[index] != wantBase {
				t.Fatalf("bases for %q = %#v, want %#v", typeName, bases, wantBases)
			}
		}
		return
	}
	t.Fatalf("classes missing name=%q in %#v", typeName, items)
}

func assertSwiftFunctionArgs(
	t *testing.T,
	payload map[string]any,
	functionName string,
	wantArgs []string,
) {
	t.Helper()

	items, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != functionName {
			continue
		}
		args, _ := item["args"].([]string)
		if len(args) != len(wantArgs) {
			t.Fatalf("args for %q = %#v, want %#v", functionName, item["args"], wantArgs)
		}
		for index, wantArg := range wantArgs {
			if args[index] != wantArg {
				t.Fatalf("args for %q = %#v, want %#v", functionName, args, wantArgs)
			}
		}
		return
	}
	t.Fatalf("functions missing name=%q in %#v", functionName, items)
}

func assertSwiftFunctionSourcePresent(t *testing.T, payload map[string]any, functionName string) {
	t.Helper()

	items, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != functionName {
			continue
		}
		source, _ := item["source"].(string)
		if source == "" {
			t.Fatalf("source for %q = %#v, want non-empty string", functionName, item["source"])
		}
		return
	}
	t.Fatalf("functions missing name=%q in %#v", functionName, items)
}

func assertSwiftVariableMetadata(
	t *testing.T,
	payload map[string]any,
	variableName string,
	wantType string,
	wantContext string,
	wantClassContext string,
) {
	t.Helper()

	items, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != variableName {
			continue
		}
		gotType, _ := item["type"].(string)
		gotContext, _ := item["context"].(string)
		gotClassContext, _ := item["class_context"].(string)
		if gotType != wantType || gotContext != wantContext || gotClassContext != wantClassContext {
			t.Fatalf(
				"metadata for %q = type=%q context=%q class_context=%q, want type=%q context=%q class_context=%q",
				variableName,
				gotType,
				gotContext,
				gotClassContext,
				wantType,
				wantContext,
				wantClassContext,
			)
		}
		return
	}
	t.Fatalf("variables missing name=%q in %#v", variableName, items)
}

func assertSwiftImportMetadata(t *testing.T, payload map[string]any, importName string) {
	t.Helper()

	items, ok := payload["imports"].([]map[string]any)
	if !ok {
		t.Fatalf("imports = %T, want []map[string]any", payload["imports"])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != importName {
			continue
		}
		fullImportName, _ := item["full_import_name"].(string)
		if fullImportName != importName {
			t.Fatalf("full_import_name for %q = %#v, want %#v", importName, item["full_import_name"], importName)
		}
		isDependency, ok := item["is_dependency"].(bool)
		if !ok || isDependency {
			t.Fatalf("is_dependency for %q = %#v, want false", importName, item["is_dependency"])
		}
		return
	}
	t.Fatalf("imports missing name=%q in %#v", importName, items)
}

func assertSwiftCallMetadata(
	t *testing.T,
	payload map[string]any,
	callName string,
	wantFullName string,
) {
	t.Helper()

	items, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", payload["function_calls"])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != callName {
			continue
		}
		fullName, _ := item["full_name"].(string)
		if fullName != wantFullName {
			t.Fatalf("full_name for %q = %#v, want %#v", callName, fullName, wantFullName)
		}
		return
	}
	t.Fatalf("function_calls missing name=%q in %#v", callName, items)
}

func assertSwiftCallInferredType(
	t *testing.T,
	payload map[string]any,
	callName string,
	wantFullName string,
	wantType string,
) {
	t.Helper()

	items, ok := payload["function_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("function_calls = %T, want []map[string]any", payload["function_calls"])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name != callName {
			continue
		}
		fullName, _ := item["full_name"].(string)
		inferredType, _ := item["inferred_obj_type"].(string)
		if fullName != wantFullName || inferredType != wantType {
			t.Fatalf(
				"call %q = full_name=%q inferred_obj_type=%q, want full_name=%q inferred_obj_type=%q",
				callName,
				fullName,
				inferredType,
				wantFullName,
				wantType,
			)
		}
		return
	}
	t.Fatalf("function_calls missing name=%q in %#v", callName, items)
}

func assertSwiftNamedBucketContains(t *testing.T, payload map[string]any, key string, wantName string) {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	for _, item := range items {
		name, _ := item["name"].(string)
		if name == wantName {
			return
		}
	}
	t.Fatalf("%s missing name=%q in %#v", key, wantName, items)
}
