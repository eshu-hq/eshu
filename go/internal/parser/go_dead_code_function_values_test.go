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
