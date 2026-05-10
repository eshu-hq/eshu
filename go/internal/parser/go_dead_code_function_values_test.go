package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoEmitsFunctionValueRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_values.go")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "assignedCallback"), "dead_code_root_kinds", "go.function_value_reference")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "compositeCallback"), "dead_code_root_kinds", "go.function_value_reference")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "mapCallback"), "dead_code_root_kinds", "go.function_value_reference")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "fieldCallback"), "dead_code_root_kinds", "go.function_value_reference")

	if _, ok := assertFunctionByName(t, got, "directlyCalled")["dead_code_root_kinds"]; ok {
		t.Fatalf("directlyCalled dead_code_root_kinds present, want absent for ordinary direct call")
	}
	if _, ok := assertFunctionByName(t, got, "unusedCallback")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedCallback dead_code_root_kinds present, want absent for unreferenced function")
	}
}

func TestDefaultEngineParsePathGoEmitsFunctionLiteralInitializerCallRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_literal_initializer.go")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "normalize"), "dead_code_root_kinds", "go.function_literal_reachable_call")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "rename"), "dead_code_root_kinds", "go.function_literal_reachable_call")
	if _, ok := assertFunctionByName(t, got, "shadowed")["dead_code_root_kinds"]; ok {
		t.Fatalf("shadowed dead_code_root_kinds present, want absent for locally shadowed literal call")
	}
	if _, ok := assertFunctionByName(t, got, "unused")["dead_code_root_kinds"]; ok {
		t.Fatalf("unused dead_code_root_kinds present, want absent for unreferenced function")
	}
}

func TestDefaultEngineParsePathGoEmitsCallArgumentFunctionValueRoots(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "call_arguments.go")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "cloudInitializer"), "dead_code_root_kinds", "go.function_value_reference")
	if _, ok := assertFunctionByName(t, got, "calledHelper")["dead_code_root_kinds"]; ok {
		t.Fatalf("calledHelper dead_code_root_kinds present, want absent for ordinary call argument result")
	}
	if _, ok := assertFunctionByName(t, got, "unused")["dead_code_root_kinds"]; ok {
		t.Fatalf("unused dead_code_root_kinds present, want absent for unreferenced function")
	}
}

func TestDefaultEngineParsePathGoEmitsFunctionValueReferenceCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "function_values.go")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	call := assertBucketItemByFieldValue(t, got, "function_calls", "name", "helpFunc")
	assertStringFieldValue(t, call, "call_kind", "go.function_value_reference")
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
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
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
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "StartNodeIpamControllerWrapper", "nodeIPAMController"), "dead_code_root_kinds", "go.method_value_reference")
	if _, ok := assertFunctionByNameAndClass(t, got, "unusedMethod", "nodeIPAMController")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedMethod dead_code_root_kinds present, want absent for unreferenced method")
	}
}

func TestDefaultEngineParsePathGoEmitsMethodValueRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "method_values.go")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByName(t, got, "assignedMethod"), "dead_code_root_kinds", "go.method_value_reference")
	assertParserStringSliceContains(t, assertFunctionByName(t, got, "compositeMethod"), "dead_code_root_kinds", "go.method_value_reference")
	if _, ok := assertFunctionByName(t, got, "unusedMethod")["dead_code_root_kinds"]; ok {
		t.Fatalf("unusedMethod dead_code_root_kinds present, want absent for unreferenced method")
	}
}

func TestDefaultEngineParsePathGoEmitsConvertedMethodValueRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "converted_method_values.go")
	writeTestFile(
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Run", "runFuncSlice"), "dead_code_root_kinds", "go.method_value_reference")
	if _, ok := assertFunctionByNameAndClass(t, got, "unusedMethod", "runFuncSlice")["dead_code_root_kinds"]; ok {
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
