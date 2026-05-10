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
