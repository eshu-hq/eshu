// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseCapturesDartBuckets(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "widget.dart", `import 'package:flutter/material.dart';
export 'src/helper.dart';
class HomePage {}
final counter = makeCounter();
Widget build() => Text('hi');
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	importRow := assertBucketName(t, payload, "imports", "package:flutter/material.dart")
	if got, want := importRow["import_type"], "import"; got != want {
		t.Fatalf("imports[package:flutter/material.dart][import_type] = %#v, want %#v", got, want)
	}
	exportRow := assertBucketName(t, payload, "imports", "src/helper.dart")
	if got, want := exportRow["import_type"], "export"; got != want {
		t.Fatalf("imports[src/helper.dart][import_type] = %#v, want %#v", got, want)
	}
	assertBucketNameCount(t, payload, "imports", "package:flutter/material.dart", 1)
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

func TestParseCapturesMultilineDartMethodSignature(t *testing.T) {
	t.Parallel()

	path := writeSource(t, filepath.Join("pkg", "lib", "worker.dart"), `class Worker {
  Future<String> fetchData(
    Uri endpoint,
  ) async {
    return endpoint.toString();
  }
}
`)

	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	method := assertFunctionByNameAndClass(t, payload, "fetchData", "Worker")
	if got, want := method["line_number"], 2; got != want {
		t.Fatalf("fetchData line_number = %#v, want %#v", got, want)
	}
	if got, want := method["end_line"], 5; got != want {
		t.Fatalf("fetchData end_line = %#v, want %#v", got, want)
	}
	if got := method["source"]; got == "" {
		t.Fatalf("fetchData source is empty in %#v", method)
	}
	assertBucketName(t, payload, "function_calls", "toString")
}

func TestParseSkipsDartCommentOnlyCalls(t *testing.T) {
	t.Parallel()

	path := writeSource(t, filepath.Join("pkg", "lib", "comments.dart"), `class Worker {
  void run() {
    // helperFromLineComment();
    /*
      helperFromBlockComment();
    */
    final text = 'helperFromString()';
    activeHelper();
  }
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "function_calls", "activeHelper")
	assertBucketNameMissing(t, payload, "function_calls", "helperFromLineComment")
	assertBucketNameMissing(t, payload, "function_calls", "helperFromBlockComment")
	assertBucketNameMissing(t, payload, "function_calls", "helperFromString")
}

func TestParseCapturesDartEnums(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "colors.dart", `enum Color { red, green, blue }
enum Status { pending, active, done }
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	enum := assertBucketName(t, payload, "classes", "Color")
	if got, want := enum["lang"], "dart"; got != want {
		t.Fatalf("enum Color lang = %#v, want %#v", got, want)
	}
	assertBucketName(t, payload, "classes", "Status")
	assertBucketNameCount(t, payload, "classes", "Color", 1)
}

func TestParseCapturesDartMixins(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "logging.dart", `mixin Logger {
  void log(String msg) {}
}
mixin Cacheable {
  String cacheKey() => '';
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	mixin := assertBucketName(t, payload, "classes", "Logger")
	if got, want := mixin["lang"], "dart"; got != want {
		t.Fatalf("mixin Logger lang = %#v, want %#v", got, want)
	}
	assertBucketName(t, payload, "classes", "Cacheable")
	assertFunctionByNameAndClass(t, payload, "log", "Logger")
	assertFunctionByNameAndClass(t, payload, "cacheKey", "Cacheable")
}

func TestParseCapturesDartExtensions(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "ext.dart", `extension StringX on String {
  bool get isBlank => trim().isEmpty;
  String capitalize() => this[0].toUpperCase() + substring(1);
}
extension type Meters(int value) {
  Meters operator +(Meters other) => Meters(value + other.value);
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	ext := assertBucketName(t, payload, "classes", "StringX")
	if got, want := ext["lang"], "dart"; got != want {
		t.Fatalf("extension StringX lang = %#v, want %#v", got, want)
	}
	assertBucketName(t, payload, "classes", "Meters")
	// extension members carry class context
	assertFunctionByNameAndClass(t, payload, "isBlank", "StringX")
	assertFunctionByNameAndClass(t, payload, "capitalize", "StringX")
}

func TestParseCapturesDartFactoryConstructors(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "factory.dart", `class Widget {
  factory Widget.named() => Widget._();
  factory Widget.fromJson(Map<String, dynamic> json) {
    return Widget._();
  }
  Widget._();
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	named := assertFunctionByNameAndClass(t, payload, "Widget.named", "Widget")
	if isFactory, _ := named["factory"].(bool); !isFactory {
		t.Fatalf("factory constructor Widget.named missing factory=true in %#v", named)
	}

	fromJson := assertFunctionByNameAndClass(t, payload, "Widget.fromJson", "Widget")
	if isFactory, _ := fromJson["factory"].(bool); !isFactory {
		t.Fatalf("factory constructor Widget.fromJson missing factory=true in %#v", fromJson)
	}

	// regular redirecting constructor (not factory) should not have factory=true
	regular := assertFunctionByNameAndClass(t, payload, "Widget._", "Widget")
	if isFactory, _ := regular["factory"].(bool); isFactory {
		t.Fatalf("non-factory constructor Widget._ has factory=true in %#v", regular)
	}
}

func TestParseCapturesDartVariables(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "vars.dart", `int count = 0;
final name = 'Eshu';
const maxItems = 100;
var items = <String>[];
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	v := assertBucketName(t, payload, "variables", "count")
	if got, want := v["lang"], "dart"; got != want {
		t.Fatalf("variable count lang = %#v, want %#v", got, want)
	}
	assertBucketName(t, payload, "variables", "name")
	assertBucketName(t, payload, "variables", "maxItems")
	assertBucketName(t, payload, "variables", "items")
	assertBucketNameCount(t, payload, "variables", "count", 1)
}

func TestParseCapturesDartCallExtraction(t *testing.T) {
	t.Parallel()

	path := writeSource(t, "calls.dart", `void run() {
  print('hello');
  doSomething(1, 2);
  var result = compute(a, b);
  fetch().then(process);
  validate();
}
`)

	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assertBucketName(t, payload, "function_calls", "print")
	assertBucketName(t, payload, "function_calls", "doSomething")
	assertBucketName(t, payload, "function_calls", "compute")
	assertBucketName(t, payload, "function_calls", "fetch")
	assertBucketName(t, payload, "function_calls", "then")
	assertBucketName(t, payload, "function_calls", "validate")

	// ensure line_number is present on calls
	call := assertBucketName(t, payload, "function_calls", "print")
	if _, ok := call["line_number"]; !ok {
		t.Fatalf("function_calls[print] missing line_number in %#v", call)
	}
}
