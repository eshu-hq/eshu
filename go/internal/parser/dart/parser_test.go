package dart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesDartBuckets(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "widget.dart", `import 'package:flutter/material.dart';
class HomePage {}
final counter = makeCounter();
Widget build() => Text('hi');
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "imports", "package:flutter/material.dart")
	assertBucketName(t, payload, "classes", "HomePage")
	function := assertBucketName(t, payload, "functions", "build")
	if got := function["source"]; got != "Widget build() => Text('hi');" {
		t.Fatalf("functions[build][source] = %#v, want source line", got)
	}
	assertBucketName(t, payload, "variables", "counter")
	assertBucketName(t, payload, "function_calls", "makeCounter")
}

func TestPreScanReturnsDartDeclarations(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "model.dart", `class Model {}
void run() {}
`)

	got, err := PreScan(path)
	if err != nil {
		t.Fatalf("PreScan() error = %v, want nil", err)
	}
	want := []string{"Model", "run"}
	if len(got) != len(want) {
		t.Fatalf("PreScan() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("PreScan() = %#v, want %#v", got, want)
		}
	}
}

func TestParseMarksDartDeadCodeRoots(t *testing.T) {
	t.Parallel()

	path := writeSource(t, filepath.Join("pkg", "lib", "home.dart"), `import 'package:flutter/widgets.dart';

void main(List<String> args) {
  runApp(const DemoApp());
}

class DemoApp extends StatefulWidget {
  const DemoApp({super.key});

  @override
  State<DemoApp> createState() => _DemoAppState();

  void main() {}
}

class _DemoAppState extends State<DemoApp> {
  _DemoAppState.named();

  @override
  Widget build(BuildContext context) => const Text('hi');

  void unusedCleanupCandidate() {}
}

class PublicHelper {
  void exposed() {}
}

@immutable
class StatelessDemo extends StatelessWidget {
  Widget build(BuildContext context) => const Text('hi');
}

void _privateHelper() {}
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "main"), "dead_code_root_kinds", "dart.main_function")
	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "DemoApp"), "dead_code_root_kinds", "dart.constructor")
	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "_DemoAppState.named"), "dead_code_root_kinds", "dart.constructor")
	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "createState"), "dead_code_root_kinds", "dart.flutter_create_state")
	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "createState"), "dead_code_root_kinds", "dart.override_method")
	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "build"), "dead_code_root_kinds", "dart.flutter_widget_build")
	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "build"), "dead_code_root_kinds", "dart.override_method")
	assertStringSliceContains(t, assertBucketName(t, payload, "classes", "PublicHelper"), "dead_code_root_kinds", "dart.public_library_api")
	assertStringSliceContains(t, assertBucketName(t, payload, "functions", "exposed"), "dead_code_root_kinds", "dart.public_library_api")

	classMain := assertFunctionByNameAndClass(t, payload, "main", "DemoApp")
	assertStringSliceNotContains(t, classMain, "dead_code_root_kinds", "dart.main_function")
	statelessBuild := assertFunctionByNameAndClass(t, payload, "build", "StatelessDemo")
	assertStringSliceContains(t, statelessBuild, "dead_code_root_kinds", "dart.flutter_widget_build")
	assertStringSliceNotContains(t, statelessBuild, "decorators", "@immutable")
	if private := assertBucketName(t, payload, "functions", "_privateHelper"); private["dead_code_root_kinds"] != nil {
		t.Fatalf("_privateHelper dead_code_root_kinds = %#v, want nil", private["dead_code_root_kinds"])
	}
	if unused := assertBucketName(t, payload, "functions", "unusedCleanupCandidate"); unused["dead_code_root_kinds"] != nil {
		t.Fatalf("unusedCleanupCandidate dead_code_root_kinds = %#v, want nil", unused["dead_code_root_kinds"])
	}
}

func TestParseDoesNotLeakDartAnnotationsFromFields(t *testing.T) {
	t.Parallel()

	path := writeSource(t, filepath.Join("pkg", "lib", "state.dart"), `class DemoState extends State<Demo> {
  @override
  final callback = createState;

  void helper() {}
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	helper := assertBucketName(t, payload, "functions", "helper")
	assertStringSliceNotContains(t, helper, "decorators", "@override")
	assertStringSliceNotContains(t, helper, "dead_code_root_kinds", "dart.override_method")
}

func TestParseDoesNotTreatConstructorCallsAsConstructors(t *testing.T) {
	t.Parallel()

	path := writeSource(t, filepath.Join("pkg", "lib", "widget.dart"), `class Demo {
  Demo();

  void make() {
    Demo();
  }
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	functions := payload["functions"].([]map[string]any)
	constructorCount := 0
	for _, item := range functions {
		if item["name"] == "Demo" {
			constructorCount++
		}
	}
	if got, want := constructorCount, 1; got != want {
		t.Fatalf("constructor count = %d, want %d functions=%#v", got, want, functions)
	}
}

func writeSource(t *testing.T, name string, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return path
}

func assertBucketName(t *testing.T, payload map[string]any, bucket string, name string) map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []map[string]any", bucket, payload[bucket])
	}
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	t.Fatalf("payload[%q] missing name %q in %#v", bucket, name, items)
	return nil
}

func assertFunctionByNameAndClass(t *testing.T, payload map[string]any, name string, classContext string) map[string]any {
	t.Helper()

	items, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("payload[functions] = %T, want []map[string]any", payload["functions"])
	}
	for _, item := range items {
		if item["name"] == name && item["class_context"] == classContext {
			return item
		}
	}
	t.Fatalf("functions missing name %q class_context %q in %#v", name, classContext, items)
	return nil
}

func assertStringSliceContains(t *testing.T, item map[string]any, key string, want string) {
	t.Helper()

	values, ok := item[key].([]string)
	if !ok {
		t.Fatalf("item[%q] = %T, want []string in %#v", key, item[key], item)
	}
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("item[%q] missing %q in %#v", key, want, values)
}

func assertStringSliceNotContains(t *testing.T, item map[string]any, key string, unwanted string) {
	t.Helper()

	values, _ := item[key].([]string)
	for _, value := range values {
		if value == unwanted {
			t.Fatalf("item[%q] contains %q in %#v", key, unwanted, values)
		}
	}
}
