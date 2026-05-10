package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoSkipsSelectorAssignmentReceiverBindings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

type State struct {
	harness *HTTPHarness
}
type HTTPHarness struct{}

func NewHTTPHarness() *HTTPHarness {
	return &HTTPHarness{}
}

func (h *HTTPHarness) AddTestCases() {}
func (s *State) AddTestCases() {}

func configure(s *State) {
	s.harness = NewHTTPHarness()
	s.AddTestCases()
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

	call := assertBucketItemByFieldValue(t, got, "function_calls", "full_name", "s.AddTestCases")
	assertStringFieldValue(t, call, "receiver_identifier", "s")
	assertStringFieldValue(t, call, "inferred_obj_type", "State")
}

func TestDefaultEngineParsePathGoAnnotatesAliasedImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "context.go")
	writeTestFile(
		t,
		filePath,
		`package terraform

import acts "github.com/hashicorp/terraform/internal/actions"

func configureContext() {
	_ = acts.NewActions()
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

	importItem := assertBucketItemByFieldValue(t, got, "imports", "name", "github.com/hashicorp/terraform/internal/actions")
	assertStringFieldValue(t, importItem, "alias", "acts")
}

func TestDefaultEngineParsePathGoAnnotatesMethodReturnChainReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	writeTestFile(
		t,
		filePath,
		`package main

type BuiltinEvalContext struct{}
type Actions struct{}

func (ctx *BuiltinEvalContext) Actions() *Actions {
	return &Actions{}
}

func (a *Actions) GetActionInstance() {}

func execute(ctx *BuiltinEvalContext) {
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
	assertStringFieldValue(t, call, "chain_receiver_identifier", "ctx")
	assertStringFieldValue(t, call, "chain_receiver_method", "Actions")
	assertStringFieldValue(t, call, "chain_receiver_obj_type", "BuiltinEvalContext")
}
