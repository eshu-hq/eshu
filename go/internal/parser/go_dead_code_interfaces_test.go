package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoEmitsLocalInterfaceRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "interfaces.go")
	writeTestFile(
		t,
		filePath,
		`package roots

type Runner interface {
	Run()
}

type Handler interface {
	Handle()
}

type worker struct{}
type idle struct{}

func (worker) Run() {}
func (worker) Handle() {}
func (idle) Run() {}
func (idle) unused() {}

func wire() {
	var runner Runner = worker{}
	handlers := []Handler{worker{}}
	runner.Run()
	_ = handlers
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

	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "interfaces", "Runner"), "dead_code_root_kinds", "go.interface_type_reference")
	assertParserStringSliceContains(t, assertBucketItemByName(t, got, "interfaces", "Handler"), "dead_code_root_kinds", "go.interface_type_reference")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Run", "worker"), "dead_code_root_kinds", "go.interface_method_implementation")
	assertParserStringSliceContains(t, assertFunctionByNameAndClass(t, got, "Handle", "worker"), "dead_code_root_kinds", "go.interface_method_implementation")
	if _, ok := assertFunctionByNameAndClass(t, got, "Run", "idle")["dead_code_root_kinds"]; ok {
		t.Fatalf("idle.Run dead_code_root_kinds present, want absent for type without interface assignment evidence")
	}
	if _, ok := assertFunctionByName(t, got, "unused")["dead_code_root_kinds"]; ok {
		t.Fatalf("unused dead_code_root_kinds present, want absent for method outside referenced interface")
	}
}

func assertFunctionByNameAndClass(t *testing.T, payload map[string]any, name string, classContext string) map[string]any {
	t.Helper()

	functions, ok := payload["functions"].([]map[string]any)
	if !ok {
		t.Fatalf("functions = %T, want []map[string]any", payload["functions"])
	}
	for _, function := range functions {
		functionName, _ := function["name"].(string)
		functionClassContext, _ := function["class_context"].(string)
		if functionName == name && functionClassContext == classContext {
			return function
		}
	}
	t.Fatalf("functions missing name %q with class_context %q in %#v", name, classContext, functions)
	return nil
}
