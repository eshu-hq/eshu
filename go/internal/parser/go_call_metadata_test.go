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
