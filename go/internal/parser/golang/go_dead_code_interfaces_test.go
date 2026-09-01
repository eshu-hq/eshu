// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoEmitsLocalInterfaceRootKinds(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "interfaces.go")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "interfaces", "Runner"), "dead_code_root_kinds", "go.interface_type_reference")
	parsertest.AssertStringSliceContains(t, parsertest.AssertBucketItemByName(t, got, "interfaces", "Handler"), "dead_code_root_kinds", "go.interface_type_reference")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Run", "worker"), "dead_code_root_kinds", "go.interface_method_implementation")
	parsertest.AssertStringSliceContains(t, parsertest.AssertFunctionByNameAndClass(t, got, "Handle", "worker"), "dead_code_root_kinds", "go.interface_method_implementation")
	if _, ok := parsertest.AssertFunctionByNameAndClass(t, got, "Run", "idle")["dead_code_root_kinds"]; ok {
		t.Fatalf("idle.Run dead_code_root_kinds present, want absent for type without interface assignment evidence")
	}
	if _, ok := parsertest.AssertBucketItemByName(t, got, "functions", "unused")["dead_code_root_kinds"]; ok {
		t.Fatalf("unused dead_code_root_kinds present, want absent for method outside referenced interface")
	}
}
