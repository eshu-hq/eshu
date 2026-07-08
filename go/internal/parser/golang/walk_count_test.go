// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// testGoFixture is a single-file Go fixture that exercises every resolution
// node kind visited by walk-2 and walk-3 in goCollectSemanticDeadCodeRoots:
// var_spec, short_var_declaration, assignment_statement, composite_literal,
// parameter_declaration, field_declaration, function_declaration,
// return_statement, call_expression, and type_parameter_declaration
// (walk-3). Forward references (a call naming a function declared later) are
// exercised so the gather-then-resolve ordering stays honest.
const testGoFixture = `package test

import "fmt"

type Printer interface {
	Print(s string)
}

type Logger struct {
	Name string
}

func (l *Logger) Print(s string) {
	fmt.Println(l.Name + ":" + s)
}

func (l *Logger) Error(s string) {
	l.Print("ERROR: " + s)
}

func ForwardCall() string {
	return helper()
}

func helper() string {
	return "ok"
}

func UseComposite() {
	cfg := Config{Port: 8080}
	_ = cfg
}

type Config struct {
	Port int
}

func Process[T any](item T) {
	fmt.Println(item)
}

func constraintFunc[T Printer](item T) {
	item.Print("constrained")
}
`

// TestParseFullTreeWalkCount pins the number of shared.WalkNamed full-tree
// traversals Parse performs on a single representative Go file. Before the
// gather-then-resolve refactor (issue #4920, epic #4917),
// goCollectSemanticDeadCodeRoots ran two independent full-tree resolution
// re-walks (walk-2 at dead_code_semantic_roots.go:94 and walk-3 via
// goMarkGenericConstraintInterfaceRoots at dead_code_semantic_roots.go:216)
// in addition to the declaration-collection walk (walk-1). After the
// refactor, resolution candidate nodes are gathered during walk-1 and
// resolved via in-memory loops, removing exactly two WalkNamed calls.
//
// This test counts shared.WalkNamed itself (via
// shared.SetWalkNamedHookForTest), not a manually annotated call site, so it
// also fails if a future change reintroduces a removed walk or adds new ones
// in the dead-code semantic root path.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestParseFullTreeWalkCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte(testGoFixture), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	_, err := Parse(parser, path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	// TDD step 1: characterize the pre-refactor baseline. When the
	// gather-then-resolve refactor removes walk-2 and walk-3, the
	// expected count drops from 60 to 58.
	const expectedWalkCount = 58
	if walkNamedCalls != expectedWalkCount {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (after refactor: 60 pre-refactor baseline minus 2 removed walks)", walkNamedCalls, expectedWalkCount)
	}
}
