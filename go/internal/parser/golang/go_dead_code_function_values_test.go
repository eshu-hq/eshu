// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoEmitsFunctionValueRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_values.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

type hookSet struct {
	OnStart func()
}

func assignedCallback() {}
func compositeCallback() {}
func mapCallback() {}
func fieldCallback() {}
func directlyCalled() {}
func unusedCallback() {}

func wire() {
	callback := assignedCallback
	callback()
	callbacks := []func(){compositeCallback}
	callbackMap := map[string]func(){"ready": mapCallback}
	hooks := hookSet{OnStart: fieldCallback}
	directlyCalled()
	_ = callbacks
	_ = callbackMap
	_ = hooks
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "assignedCallback"), "dead_code_root_kinds", "go.function_value_reference")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "compositeCallback"), "dead_code_root_kinds", "go.function_value_reference")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "mapCallback"), "dead_code_root_kinds", "go.function_value_reference")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "fieldCallback"), "dead_code_root_kinds", "go.function_value_reference")

	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "directlyCalled")["dead_code_root_kinds"]; ok {
		t.Fatalf("directlyCalled dead_code_root_kinds present, want absent for ordinary direct call")
	}
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "unusedCallback")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedCallback dead_code_root_kinds present, want absent for unreferenced function")
	}
}

func TestDefaultEngineParsePathGoEmitsFunctionLiteralInitializerCallRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_literal_initializer.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

type rewriter func(string) string

var registry = []rewriter{
	func(value string) string {
		value = normalize(value)
		return rename(value)
	},
	func(shadowed func()) string {
		shadowed()
		return "shadowed"
	},
}

func normalize(value string) string { return value }
func rename(value string) string { return value }
func shadowed() {}
func unused() {}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "normalize"), "dead_code_root_kinds", "go.function_literal_reachable_call")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "rename"), "dead_code_root_kinds", "go.function_literal_reachable_call")
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "shadowed")["dead_code_root_kinds"]; ok {
		t.Fatalf("shadowed dead_code_root_kinds present, want absent for locally shadowed literal call")
	}
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "unused")["dead_code_root_kinds"]; ok {
		t.Fatalf("unused dead_code_root_kinds present, want absent for unreferenced function")
	}
}

func TestDefaultEngineParsePathGoEmitsCallArgumentFunctionValueRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "call_arguments.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

import "example.com/project/builder"

func cloudInitializer() {}
func calledHelper() {}
func unused() {}

func main() {
	builder.NewCommand(cloudInitializer)
	builder.NewCommand(calledHelper())
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "cloudInitializer"), "dead_code_root_kinds", "go.function_value_reference")
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "calledHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("calledHelper dead_code_root_kinds present, want absent for ordinary call argument result")
	}
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "unused")["dead_code_root_kinds"]; ok {
		t.Fatalf("unused dead_code_root_kinds present, want absent for unreferenced function")
	}
}

func TestDefaultEngineParsePathGoEmitsFunctionValueReferenceCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_values.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

type CLI struct {
	HelpFunc func()
}

func helpFunc() {}
func directCall() {}

func wire() {
	cli := CLI{HelpFunc: helpFunc}
	directCall()
	_ = cli
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "name", "helpFunc")
	parsertest.AssertStringFieldValue(t, call, "call_kind", "go.function_value_reference")
	if bucketHasFieldValues(got, "function_calls", map[string]string{
		"name":      "directCall",
		"call_kind": "go.function_value_reference",
	}) {
		t.Fatalf("directCall emitted as a function value reference, want ordinary direct calls only")
	}
}

func TestDefaultEngineParsePathGoSkipsShadowedFunctionValueReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_values.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

func helper() {}
func callback() {}

func wire(helper func()) {
	local := 1
	callback := 2
	_ = []any{helper, local, callback}
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	for _, name := range []string{"helper", "local", "callback"} {
		if bucketHasFieldValues(got, "function_calls", map[string]string{
			"name":      name,
			"call_kind": "go.function_value_reference",
		}) {
			t.Fatalf("%s emitted as a function value reference, want shadowed or non-function identifiers skipped", name)
		}
	}
}

func TestDefaultEngineParsePathGoEmitsMethodValueCallArgumentRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_argument.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

type ControllerInitFuncConstructor struct {
	Constructor func()
}

type nodeIPAMController struct{}

func (nodeIPAMController) StartNodeIpamControllerWrapper() {}
func (nodeIPAMController) unusedMethod() {}

func main() {
	nodeIpamController := nodeIPAMController{}
	_ = ControllerInitFuncConstructor{
		Constructor: nodeIpamController.StartNodeIpamControllerWrapper,
	}
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "StartNodeIpamControllerWrapper", "nodeIPAMController"), "dead_code_root_kinds", "go.method_value_reference")
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "unusedMethod", "nodeIPAMController")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedMethod dead_code_root_kinds present, want absent for unreferenced method")
	}
}

func TestDefaultEngineParsePathGoEmitsMethodValueRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_values.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

type worker struct{}

func (worker) assignedMethod() {}
func (worker) compositeMethod() {}
func (worker) unusedMethod() {}

func wire() {
	w := worker{}
	assigned := w.assignedMethod
	callbacks := []func(){w.compositeMethod}
	assigned()
	_ = callbacks
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "assignedMethod"), "dead_code_root_kinds", "go.method_value_reference")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "functions", "compositeMethod"), "dead_code_root_kinds", "go.method_value_reference")
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "unusedMethod")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedMethod dead_code_root_kinds present, want absent for unreferenced method")
	}
}

func TestDefaultEngineParsePathGoEmitsConvertedMethodValueRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "converted_method_values.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package roots

type runFunc func()
type runFuncSlice []runFunc

func (rx runFuncSlice) Run() {}
func (rx runFuncSlice) unusedMethod() {}

func join(rx []runFunc) runFunc {
	return runFuncSlice(rx).Run
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Run", "runFuncSlice"), "dead_code_root_kinds", "go.method_value_reference")
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "unusedMethod", "runFuncSlice")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedMethod dead_code_root_kinds present, want absent for unreferenced converted method value")
	}
}

func bucketHasFieldValues(payload map[string]any, bucket string, fields map[string]string) bool {
	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		return false
	}
	for _, item := range items {
		matches := true
		for field, want := range fields {
			value, _ := item[field].(string)
			if value != want {
				matches = false
				break
			}
		}
		if matches {
			return matches
		}
	}
	return false
}
