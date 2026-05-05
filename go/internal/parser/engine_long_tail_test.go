package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathRubyFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "ruby_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	basicPath := filepath.Join(repoRoot, "basic.rb")
	basicPayload, err := engine.ParsePath(repoRoot, basicPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", basicPath, err)
	}
	assertNamedBucketContains(t, basicPayload, "functions", "greet")
	assertRubyStringSliceFieldValue(t, assertFunctionByName(t, basicPayload, "greet"), "args", []string{"name"})
	assertNamedBucketContains(t, basicPayload, "classes", "Application")
	assertNamedBucketContains(t, basicPayload, "modules", "Comprehensive")
	assertNamedBucketContains(t, basicPayload, "variables", "@config")
	assertBucketContainsFieldValue(t, basicPayload, "function_calls", "full_name", "Comprehensive.greet")

	mixinsPath := filepath.Join(repoRoot, "modules_mixins.rb")
	mixinsPayload, err := engine.ParsePath(repoRoot, mixinsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", mixinsPath, err)
	}
	assertNamedBucketContains(t, mixinsPayload, "classes", "Service")
	assertNamedBucketContains(t, mixinsPayload, "functions", "expensive_operation")
	assertRubyStringSliceFieldValue(t, assertFunctionByName(t, mixinsPayload, "expensive_operation"), "args", []string{"input"})
	assertStringFieldValue(t, assertFunctionByName(t, mixinsPayload, "expensive_operation"), "class_context", "Service")
	assertNamedBucketContains(t, mixinsPayload, "imports", "basic")
	assertBucketContainsFieldValue(t, mixinsPayload, "function_calls", "name", "require_relative")
	assertModuleInclusion(t, mixinsPayload, "Service", "Printable")
}

func TestDefaultEngineParsePathPHPFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "php_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	importsPath := filepath.Join(repoRoot, "imports.php")
	importsPayload, err := engine.ParsePath(repoRoot, importsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", importsPath, err)
	}
	assertNamedBucketContains(t, importsPayload, "functions", "run")
	assertNamedBucketContains(t, importsPayload, "classes", "Application")
	assertBucketContainsFieldValue(t, importsPayload, "imports", "name", "Comprehensive\\Config")
	assertBucketContainsFieldValue(t, importsPayload, "imports", "full_import_name", "use Comprehensive\\Config;")
	assertNamedBucketContains(t, importsPayload, "variables", "$config")
	assertBucketContainsFieldValue(t, importsPayload, "function_calls", "full_name", "$this->service.info")

	classesPath := filepath.Join(repoRoot, "classes.php")
	classesPayload, err := engine.ParsePath(repoRoot, classesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", classesPath, err)
	}
	assertBucketItemStringSliceContains(t, classesPayload, "classes", "Circle", "Shape")
	assertBucketItemStringSliceContains(t, classesPayload, "classes", "Rectangle", "Shape")

	staticCallsPath := filepath.Join(repoRoot, "static_calls.php")
	staticCallsPayload, err := engine.ParsePath(repoRoot, staticCallsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", staticCallsPath, err)
	}
	assertNamedBucketContains(t, staticCallsPayload, "classes", "Logger")
	assertNamedBucketContains(t, staticCallsPayload, "classes", "Child")
	assertNamedBucketContains(t, staticCallsPayload, "functions", "warn")
	assertNamedBucketContains(t, staticCallsPayload, "functions", "emit")
	assertBucketContainsFieldValue(t, staticCallsPayload, "function_calls", "full_name", "Logger.warn")
	assertBucketContainsFieldValue(t, staticCallsPayload, "function_calls", "inferred_obj_type", "Logger")
	assertBucketContainsFieldValue(t, staticCallsPayload, "function_calls", "full_name", "parent::instance()->createService().info")
	assertBucketContainsFieldValue(t, staticCallsPayload, "function_calls", "inferred_obj_type", "Service")
	staticEmitCalls := 0
	for _, item := range staticCallsPayload["function_calls"].([]map[string]any) {
		fullName, _ := item["full_name"].(string)
		if fullName != "Config.emit" {
			continue
		}
		assertStringFieldValue(t, item, "inferred_obj_type", "Config")
		staticEmitCalls++
	}
	if staticEmitCalls != 2 {
		t.Fatalf("Config.emit calls = %d, want 2 in %#v", staticEmitCalls, staticCallsPayload["function_calls"])
	}

	staticAliasCallsPath := filepath.Join(repoRoot, "static_alias_calls.php")
	staticAliasCallsPayload, err := engine.ParsePath(repoRoot, staticAliasCallsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", staticAliasCallsPath, err)
	}
	assertNamedBucketContains(t, staticAliasCallsPayload, "classes", "Factory")
	assertBucketContainsFieldValue(t, staticAliasCallsPayload, "function_calls", "full_name", "AppFactory::instance()->createService().info")
	assertBucketContainsFieldValue(t, staticAliasCallsPayload, "function_calls", "inferred_obj_type", "Service")

	traitsPath := filepath.Join(repoRoot, "traits.php")
	traitsPayload, err := engine.ParsePath(repoRoot, traitsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", traitsPath, err)
	}
	assertNamedBucketContains(t, traitsPayload, "traits", "Loggable")
	assertNamedBucketContains(t, traitsPayload, "functions", "info")
}

func TestDefaultEngineParsePathPHPResolvesSelfStaticReceiverMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "self_static.php")
	writeTestFile(
		t,
		filePath,
		`<?php
class Config {
    public static function emit(string $message): void {}

    public function run(): void {
        self::emit("hello");
    }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	call := assertBucketItemByName(t, got, "function_calls", "emit")
	assertStringFieldValue(t, call, "full_name", "Config.emit")
	assertStringFieldValue(t, call, "inferred_obj_type", "Config")
}

func TestDefaultEngineParsePathKotlinFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "kotlin_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	basicPath := filepath.Join(repoRoot, "Basic.kt")
	basicPayload, err := engine.ParsePath(repoRoot, basicPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", basicPath, err)
	}
	assertNamedBucketContains(t, basicPayload, "functions", "greet")
	assertNamedBucketContains(t, basicPayload, "classes", "AppConfig")
	assertNamedBucketContains(t, basicPayload, "variables", "VERSION")

	classesPath := filepath.Join(repoRoot, "Classes.kt")
	classesPayload, err := engine.ParsePath(repoRoot, classesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", classesPath, err)
	}
	assertNamedBucketContains(t, classesPayload, "classes", "Point")
	assertNamedBucketContains(t, classesPayload, "classes", "Companion")
	assertFunctionWithClassContext(t, classesPayload, "create", "Person")

	genericsPath := filepath.Join(repoRoot, "Generics.kt")
	genericsPayload, err := engine.ParsePath(repoRoot, genericsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", genericsPath, err)
	}
	assertNamedBucketContains(t, genericsPayload, "classes", "Box")
	assertBucketContainsFieldValue(t, genericsPayload, "function_calls", "full_name", "typedBox.unwrap().info")
	assertBucketContainsFieldValue(t, genericsPayload, "function_calls", "full_name", "returnedBox.unwrap().info")
}

func TestDefaultEngineParsePathSwiftFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "swift_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	structsPath := filepath.Join(repoRoot, "Structs.swift")
	structsPayload, err := engine.ParsePath(repoRoot, structsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", structsPath, err)
	}
	assertNamedBucketContains(t, structsPayload, "structs", "Point")
	assertNamedBucketContains(t, structsPayload, "variables", "host")
	assertNamedBucketContains(t, structsPayload, "imports", "Foundation")

	enumsPath := filepath.Join(repoRoot, "Enums.swift")
	enumsPayload, err := engine.ParsePath(repoRoot, enumsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", enumsPath, err)
	}
	assertNamedBucketContains(t, enumsPayload, "enums", "Direction")
	assertNamedBucketContains(t, enumsPayload, "functions", "map")
	assertNamedBucketContains(t, enumsPayload, "function_calls", "transform")
}

func TestDefaultEngineParsePathElixirFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "elixir_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	basicPath := filepath.Join(repoRoot, "basic.ex")
	basicPayload, err := engine.ParsePath(repoRoot, basicPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", basicPath, err)
	}
	assertNamedBucketContains(t, basicPayload, "functions", "greet")
	assertNamedBucketContains(t, basicPayload, "modules", "Comprehensive.Basic")

	importsPath := filepath.Join(repoRoot, "imports.ex")
	importsPayload, err := engine.ParsePath(repoRoot, importsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", importsPath, err)
	}
	assertBucketContainsFieldValue(t, importsPayload, "imports", "name", "Logger")
	assertNamedBucketContains(t, importsPayload, "functions", "start")
	assertEmptyNamedBucket(t, importsPayload, "variables")
}

func TestDefaultEngineParsePathDartFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "dart_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	asyncPath := filepath.Join(repoRoot, "async.dart")
	asyncPayload, err := engine.ParsePath(repoRoot, asyncPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", asyncPath, err)
	}
	assertNamedBucketContains(t, asyncPayload, "functions", "fetchData")
	assertNamedBucketContains(t, asyncPayload, "classes", "AsyncWorker")
	assertNamedBucketContains(t, asyncPayload, "imports", "dart:async")
	assertNamedBucketContains(t, asyncPayload, "function_calls", "fetchData")

	extensionsPath := filepath.Join(repoRoot, "extensions.dart")
	extensionsPayload, err := engine.ParsePath(repoRoot, extensionsPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", extensionsPath, err)
	}
	assertNamedBucketContains(t, extensionsPayload, "classes", "StringTools")
}

func TestDefaultEngineParsePathPerlFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "perl_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	modulesPath := filepath.Join(repoRoot, "modules.pl")
	payload, err := engine.ParsePath(repoRoot, modulesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", modulesPath, err)
	}
	assertNamedBucketContains(t, payload, "classes", "Utilities")
	assertNamedBucketContains(t, payload, "functions", "format_path")
	assertBucketContainsFieldValue(t, payload, "imports", "name", "File::Basename")
}

func TestDefaultEngineParsePathPerlCallsAndVariables(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "perl_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	modulesPath := filepath.Join(repoRoot, "modules.pl")
	modulesPayload, err := engine.ParsePath(repoRoot, modulesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", modulesPath, err)
	}
	assertBucketContainsFieldValue(t, modulesPayload, "function_calls", "name", "File::Basename::basename")
	assertBucketContainsFieldValue(t, modulesPayload, "function_calls", "name", "List::Util::sum")
	assertBucketContainsFieldValue(t, modulesPayload, "function_calls", "name", "Carp::croak")

	callbacksPath := filepath.Join(repoRoot, "callbacks.pl")
	callbacksPayload, err := engine.ParsePath(repoRoot, callbacksPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", callbacksPath, err)
	}
	assertNamedBucketContains(t, callbacksPayload, "classes", "EventSystem")
	assertNamedBucketContains(t, callbacksPayload, "variables", "handlers")
}

func TestDefaultEngineParsePathHaskellFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "haskell_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	basicPath := filepath.Join(repoRoot, "Basic.hs")
	basicPayload, err := engine.ParsePath(repoRoot, basicPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", basicPath, err)
	}
	assertNamedBucketContains(t, basicPayload, "functions", "greet")
	assertNamedBucketContains(t, basicPayload, "functions", "processNames")
	assertBucketContainsFieldValue(t, basicPayload, "imports", "name", "Data.List")
	assertNamedBucketContains(t, basicPayload, "variables", "category")
}

func assertFunctionWithClassContext(
	t *testing.T,
	payload map[string]any,
	name string,
	classContext string,
) {
	t.Helper()

	items, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		itemContext, _ := item["class_context"].(string)
		if itemName == name && itemContext == classContext {
			return
		}
	}
	t.Fatalf("functions missing name=%q class_context=%q in %#v", name, classContext, items)
}

func assertModuleInclusion(t *testing.T, payload map[string]any, className string, moduleName string) {
	t.Helper()

	items, ok := payload["module_inclusions"].([]map[string]any)
	if !ok {
		t.Fatalf("module_inclusions = %T, want []map[string]any", payload["module_inclusions"])
	}
	for _, item := range items {
		itemClass, _ := item["class"].(string)
		itemModule, _ := item["module"].(string)
		if itemClass == className && itemModule == moduleName {
			return
		}
	}
	t.Fatalf(
		"module_inclusions missing class=%q module=%q in %#v",
		className,
		moduleName,
		items,
	)
}

func assertRubyStringSliceFieldValue(
	t *testing.T,
	item map[string]any,
	field string,
	want []string,
) {
	t.Helper()

	got, ok := item[field].([]string)
	if !ok {
		t.Fatalf("%s = %T, want []string", field, item[field])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func assertBucketItemStringSliceContains(
	t *testing.T,
	payload map[string]any,
	bucket string,
	name string,
	want string,
) {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		itemName, _ := item["name"].(string)
		if itemName != name {
			continue
		}
		values, ok := item["bases"].([]string)
		if !ok {
			t.Fatalf("%s[%q].bases = %T, want []string", bucket, name, item["bases"])
		}
		for _, value := range values {
			if value == want {
				return
			}
		}
		t.Fatalf("%s[%q].bases = %#v, want to contain %q", bucket, name, values, want)
	}
	t.Fatalf("%s missing name %q in %#v", bucket, name, items)
}
