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

func TestDefaultEngineParsePathGoInfersReceiverFromFuncLiteralParameter(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "controllers.go")
	writeTestFile(
		t,
		filePath,
		`package main

type ControllerDescriptor struct{}

func (d *ControllerDescriptor) BuildController() {}

func runControllers() {
	buildController := func(controllerDesc *ControllerDescriptor) error {
		controllerDesc.BuildController()
		return nil
	}
	_ = buildController
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "controllerDesc.BuildController")
	assertStringFieldValue(t, call, "receiver_identifier", "controllerDesc")
	assertStringFieldValue(t, call, "inferred_obj_type", "ControllerDescriptor")
}

func TestDefaultEngineParsePathGoKeepsConstructorReceiverBindingsBlockScoped(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

type HTTPHarness struct{}
type OtherHarness struct{}

func NewHTTPHarness() *HTTPHarness {
	return &HTTPHarness{}
}

func (h *HTTPHarness) AddTestCases() {}
func (h *OtherHarness) AddTestCases() {}

func addDemoTestCases(harness *OtherHarness, enabled bool) {
	if enabled {
		harness := NewHTTPHarness()
		harness.AddTestCases()
	}
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

	var innerCall, outerCall map[string]any
	for _, call := range bucketItems(t, got, "function_calls") {
		if fullName, _ := call["full_name"].(string); fullName != "harness.AddTestCases" {
			continue
		}
		line := intFieldValue(t, call, "line_number")
		switch line {
		case 16:
			innerCall = call
		case 18:
			outerCall = call
		}
	}
	if innerCall == nil || outerCall == nil {
		t.Fatalf("missing expected harness.AddTestCases calls; calls=%#v", got["function_calls"])
	}
	assertStringFieldValue(t, innerCall, "inferred_obj_type", "HTTPHarness")
	assertStringFieldValue(t, outerCall, "inferred_obj_type", "OtherHarness")
}

func TestDefaultEngineParsePathGoInfersRangeReceiverFromMapValueType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "descriptors.go")
	writeTestFile(
		t,
		filePath,
		`package main

type ControllerDescriptor struct{}

func (d *ControllerDescriptor) BuildController() {}
func (d *ControllerDescriptor) RequiresSpecialHandling() bool { return false }

func runControllers(controllerDescriptors map[string]*ControllerDescriptor) {
	for _, controllerDesc := range controllerDescriptors {
		controllerDesc.BuildController()
		if controllerDesc.RequiresSpecialHandling() {
			continue
		}
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

	buildCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "controllerDesc.BuildController")
	assertStringFieldValue(t, buildCall, "receiver_identifier", "controllerDesc")
	assertStringFieldValue(t, buildCall, "inferred_obj_type", "ControllerDescriptor")

	specialCall := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "controllerDesc.RequiresSpecialHandling")
	assertStringFieldValue(t, specialCall, "receiver_identifier", "controllerDesc")
	assertStringFieldValue(t, specialCall, "inferred_obj_type", "ControllerDescriptor")
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

func bucketItems(t *testing.T, payload map[string]any, bucket string) []map[string]any {
	t.Helper()

	items, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	return items
}

func intFieldValue(t *testing.T, item map[string]any, field string) int {
	t.Helper()

	switch value := item[field].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		t.Fatalf("%s = %T, want numeric value", field, item[field])
		return 0
	}
}
