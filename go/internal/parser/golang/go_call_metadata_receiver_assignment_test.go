// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoSkipsSelectorAssignmentReceiverBindings(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "s.AddTestCases")
	parsertest.AssertStringFieldValue(t, call, "receiver_identifier", "s")
	parsertest.AssertStringFieldValue(t, call, "inferred_obj_type", "State")
}

func TestDefaultEngineParsePathGoAnnotatesAliasedImports(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "context.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package terraform

import acts "github.com/hashicorp/terraform/internal/actions"

func configureContext() {
	_ = acts.NewActions()
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

	importItem := parsertest.AssertBucketItemByFieldValue(t, got, "imports", "name", "github.com/hashicorp/terraform/internal/actions")
	parsertest.AssertStringFieldValue(t, importItem, "alias", "acts")
}

func TestDefaultEngineParsePathGoAnnotatesMethodReturnChainReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "name", "GetActionInstance")
	parsertest.AssertStringFieldValue(t, call, "chain_receiver_identifier", "ctx")
	parsertest.AssertStringFieldValue(t, call, "chain_receiver_method", "Actions")
	parsertest.AssertStringFieldValue(t, call, "chain_receiver_obj_type", "BuiltinEvalContext")
}

func TestDefaultEngineParsePathGoAnnotatesConcreteInterfaceAssignmentChainReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package main

type EvalContext interface {
	Actions() *Actions
}
type BuiltinEvalContext struct{}
type Actions struct{}

func (ctx *BuiltinEvalContext) Actions() *Actions {
	return &Actions{}
}

func (a *Actions) GetActionInstance() {}

func execute() {
	var ctx EvalContext = &BuiltinEvalContext{}
	ctx.Actions().GetActionInstance()
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

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "name", "GetActionInstance")
	parsertest.AssertStringFieldValue(t, call, "chain_receiver_identifier", "ctx")
	parsertest.AssertStringFieldValue(t, call, "chain_receiver_method", "Actions")
	parsertest.AssertStringFieldValue(t, call, "chain_receiver_obj_type", "BuiltinEvalContext")
}

func TestDefaultEngineParsePathGoSkipsUnprovenInterfaceParameterChainReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package main

type EvalContext interface {
	Actions() *Actions
}
type Actions struct{}

func (a *Actions) GetActionInstance() {}

func execute(ctx EvalContext) {
	ctx.Actions().GetActionInstance()
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

	directCall := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "full_name", "ctx.Actions")
	parsertest.AssertStringFieldValue(t, directCall, "receiver_identifier", "ctx")
	parsertest.AssertStringFieldValue(t, directCall, "inferred_obj_type", "EvalContext")

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "name", "GetActionInstance")
	if gotType, ok := call["chain_receiver_obj_type"]; ok {
		t.Fatalf("chain_receiver_obj_type = %#v, want no unproven concrete receiver type", gotType)
	}
}

func TestDefaultEngineParsePathGoSkipsAmbiguousInterfaceAssignmentChainReceiverType(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "eval.go")
	parsertest.WriteFile(
		t,
		filePath,
		`package main

type EvalContext interface {
	Actions() *Actions
}
type BuiltinEvalContext struct{}
type OtherEvalContext struct{}
type Actions struct{}

func (ctx *BuiltinEvalContext) Actions() *Actions {
	return &Actions{}
}

func (ctx *OtherEvalContext) Actions() *Actions {
	return &Actions{}
}

func (a *Actions) GetActionInstance() {}

func execute() {
	var ctx EvalContext = &BuiltinEvalContext{}
	ctx = &OtherEvalContext{}
	ctx.Actions().GetActionInstance()
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

	call := parsertest.AssertBucketItemByFieldValue(t, got, "function_calls", "name", "GetActionInstance")
	if gotType, ok := call["chain_receiver_obj_type"]; ok {
		t.Fatalf("chain_receiver_obj_type = %#v, want no ambiguous concrete receiver type", gotType)
	}
}
