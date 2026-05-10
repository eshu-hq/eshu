package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoAnnotatesReceiverSelectorCalls(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handler.go")
	writeTestFile(
		t,
		filePath,
		`package query

import "fmt"

type CodeHandler struct{}

func (h *CodeHandler) transitiveRelationshipsGraphRow() {}

func (h *CodeHandler) handleRelationships() {
	h.transitiveRelationshipsGraphRow()
	fmt.Println("hello")
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

	methodCall := assertBucketItemByFieldValue(
		t,
		got,
		"function_calls",
		"full_name",
		"h.transitiveRelationshipsGraphRow",
	)
	assertStringFieldValue(t, methodCall, "name", "transitiveRelationshipsGraphRow")
	assertStringFieldValue(t, methodCall, "receiver_identifier", "h")
	assertStringFieldValue(t, methodCall, "class_context", "CodeHandler")
	if got, ok := methodCall["receiver_is_import_alias"].(bool); !ok || got {
		t.Fatalf("receiver_is_import_alias = %#v, want false", methodCall["receiver_is_import_alias"])
	}

	importCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "fmt.Println")
	assertStringFieldValue(t, importCall, "receiver_identifier", "fmt")
	if got, ok := importCall["receiver_is_import_alias"].(bool); !ok || !got {
		t.Fatalf("receiver_is_import_alias = %#v, want true", importCall["receiver_is_import_alias"])
	}
	if _, ok := importCall["class_context"]; ok {
		t.Fatalf("class_context = %#v, want omitted for import-qualified call", importCall["class_context"])
	}
}

func TestDefaultEngineParsePathGoInfersLocalReceiverFromConstructorReturn(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

type HTTPHarness struct{}

func NewHTTPHarness() *HTTPHarness {
	return &HTTPHarness{}
}

func (h *HTTPHarness) AddTestCases() {}

func addDemoTestCases() {
	harness := NewHTTPHarness()
	harness.AddTestCases()
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "harness.AddTestCases")
	assertStringFieldValue(t, call, "receiver_identifier", "harness")
	assertStringFieldValue(t, call, "inferred_obj_type", "HTTPHarness")
}

func TestDefaultEngineParsePathGoInfersReceiverFromTypedParameter(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

type HTTPHarness struct{}

func (h *HTTPHarness) AddTestCases() {}

func addDemoTestCases(harness *HTTPHarness) {
	harness.AddTestCases()
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "harness.AddTestCases")
	assertStringFieldValue(t, call, "receiver_identifier", "harness")
	assertStringFieldValue(t, call, "inferred_obj_type", "HTTPHarness")
}

func TestDefaultEngineParsePathGoRecordsFunctionReturnType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

type EvalContext struct{}
type Actions struct{}

func (ctx *EvalContext) Actions() *Actions {
	return &Actions{}
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

	function := assertFunctionByNameAndClass(t, got, "Actions", "EvalContext")
	assertStringFieldValue(t, function, "return_type", "Actions")
}

func TestDefaultEngineParsePathGoRecordsMethodReturnChainCallName(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

type EvalContext struct{}
type Actions struct{}

func (ctx *EvalContext) Actions() *Actions {
	return &Actions{}
}

func (a *Actions) GetActionInstance() {}

func execute(ctx *EvalContext) {
	ctx.Actions().GetActionInstance()
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "name", "GetActionInstance")
	assertStringFieldValue(t, call, "full_name", "ctx.Actions().GetActionInstance")
}

func TestDefaultEngineParsePathGoNormalizesQualifiedReturnTypes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

import "example.com/project/internal/actions"

type BuiltinEvalContext struct{}

func (ctx *BuiltinEvalContext) Actions() *actions.Actions {
	return actions.NewActions()
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

	function := assertFunctionByNameAndClass(t, got, "Actions", "BuiltinEvalContext")
	assertStringFieldValue(t, function, "return_type", "Actions")
}
