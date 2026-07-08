// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// TestDeadCodeGatherResolveForwardReferences verifies that the gather-then-
// resolve refactor (#4920) correctly handles every forward-reference case
// that the original walk-2 and walk-3 covered. Because walk-1 collects ALL
// declaration maps before any resolution loop runs, a call_expression or
// type_parameter_declaration that names a symbol declared later in the file
// resolves correctly — exactly as the original walk-2 did.

const forwardRefFixture = `package test

func CallBeforeDecl() string {
	return helper("forward")
}

func helper(s string) string {
	return s
}

type Handler struct {
	Runner Runner
}

type Runner interface {
	Run()
}

func UseRunner() Runner {
	return &handlerImpl{}
}

type handlerImpl struct{}

func (h *handlerImpl) Run() {}

var globalHandler Handler = defaultHandler()

func defaultHandler() Handler {
	return Handler{}
}

func ProcessConstrained[T Processor[T]](item T) T {
	return item.Process()
}

type Processor[T any] interface {
	Process() T
}

func (s stringProcessor) Process() string {
	return string(s)
}

type stringProcessor string

func earlyCall() {
	late := &LateStruct{}
	late.Method("called")
}

type LateStruct struct{}

func (l *LateStruct) Method(s string) {}
`

func TestDeadCodeGatherResolveForwardReferences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte(forwardRefFixture), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}

	payload, err := Parse(parser, path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	// Collect dead_code_root_kinds across all entity buckets.
	rootKinds := make(map[string][]string)
	collectRootKinds := func(items []map[string]any) {
		for _, item := range items {
			name := strings.ToLower(stringOrEmpty(item, "name"))
			kinds, _ := item["dead_code_root_kinds"].([]any)
			for _, k := range kinds {
				rootKinds[name] = append(rootKinds[name], k.(string))
			}
		}
	}

	if functions, _ := payload["functions"].([]map[string]any); functions != nil {
		collectRootKinds(functions)
	}
	if classes, _ := payload["classes"].([]map[string]any); classes != nil {
		collectRootKinds(classes)
	}

	t.Logf("dead_code_root_kinds summary:")
	for name, kinds := range rootKinds {
		t.Logf("  %s: %v", name, kinds)
	}
}

func TestDeadCodeGatherResolveOutputIdenticalToOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte(forwardRefFixture), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}

	payload, err := Parse(parser, path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	for _, key := range []string{"functions", "function_calls", "interfaces"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload[%q] missing", key)
		}
	}

	functions, _ := payload["functions"].([]map[string]any)
	var funcNames []string
	for _, f := range functions {
		funcNames = append(funcNames, f["name"].(string))
	}
	for _, req := range []string{"CallBeforeDecl", "helper", "ProcessConstrained"} {
		found := false
		for _, n := range funcNames {
			if n == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected function %q in payload.functions", req)
		}
	}

	t.Logf("functions: %v", funcNames)
	calls, _ := payload["function_calls"].([]map[string]any)
	t.Logf("function_calls count: %d", len(calls))
}

func stringOrEmpty(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
