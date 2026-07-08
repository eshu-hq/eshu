// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// shapeAFixture produces a method whose dead_code_root_kinds contains three
// distinct root-kind strings sourced from different node kinds in the gather
// loops:
//   - go.direct_method_call         (gatheredCallExprs loop)
//   - go.generic_constraint_method   (goMarkGenericConstraintInterfaceRoots)
//   - go.interface_method_implementation (final interfaceConcreteTypes resolution)
//
// The same-key ordering these three arrive in is the characterization
// property this test locks in; a subsequent re-ordering of the gather
// loops must fail the test.
const shapeAFixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

func early() {
	var i = impl{}
	i.Process()
}

func useGeneric[T Processor](t T) {
	t.Process()
}

func makeProcessor() Processor {
	return impl{}
}
`

// shapeBFixture produces a method whose dead_code_root_kinds contains two
// distinct root-kind strings sourced from different node kinds:
//   - go.direct_method_call         (gatheredCallExprs loop)
//   - go.interface_method_implementation (final interfaceConcreteTypes resolution)
const shapeBFixture = `package test

type Runner interface {
	Run()
}

type handler struct{}

func (h *handler) Run() {}

func early() {
	var h handler
	h.Run()
}

func makeRunner() Runner {
	return handler{}
}
`

// TestDeadCodeCrossKindSameKeyOrdering is the COMMITTED characterization
// test for the cross-kind same-key ordering property discovered during the
// gather-then-resolve refactor (#4920). When a single functionRootKinds
// key receives root-kind strings from multiple different node-kind-based
// loops (e.g. a method that is both a direct_call target and an
// interface_method_implementation, or additionally a
// generic_constraint_method), the emitted dead_code_root_kinds slice
// order is deterministic. This test locks in the exact order
// origin/main produces so a future reorder of the gather loops in
// dead_code_semantic_roots.go cannot silently reorder the output.
func TestDeadCodeCrossKindSameKeyOrdering(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		methodKey     string
		wantRootKinds []string
	}{
		{
			name:      "ShapeA_ThreeKind",
			fixture:   shapeAFixture,
			methodKey: "impl.process",
			wantRootKinds: []string{
				"go.direct_method_call",
				"go.generic_constraint_method",
				"go.interface_method_implementation",
			},
		},
		{
			name:      "ShapeB_TwoKind",
			fixture:   shapeBFixture,
			methodKey: "handler.run",
			wantRootKinds: []string{
				"go.direct_method_call",
				"go.interface_method_implementation",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			fpath := filepath.Join(dir, "test.go")
			if err := os.WriteFile(fpath, []byte(tt.fixture), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v, want nil", err)
			}

			parser := tree_sitter.NewParser()
			if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
				t.Fatalf("SetLanguage(go) error = %v, want nil", err)
			}

			payload, err := Parse(parser, fpath, false, shared.Options{})
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}

			functions, _ := payload["functions"].([]map[string]any)
			var gotKinds []string
			for _, f := range functions {
				classCtx, _ := f["class_context"].(string)
				name, _ := f["name"].(string)
				methodKey := strings.ToLower(classCtx + "." + name)
				if methodKey != tt.methodKey {
					continue
				}
				// goDeadCodeRootKinds returns []string; try that first,
				// then fall back to []any for safety.
				switch v := f["dead_code_root_kinds"].(type) {
				case []string:
					gotKinds = v
				case []any:
					for _, k := range v {
						gotKinds = append(gotKinds, k.(string))
					}
				}
				break
			}

			if len(gotKinds) == 0 {
				t.Fatalf("method %q not found or has no dead_code_root_kinds", tt.methodKey)
			}

			if !slices.Equal(gotKinds, tt.wantRootKinds) {
				t.Errorf("%s ordering mismatch:\n  got:  %v\n  want: %v", tt.methodKey, gotKinds, tt.wantRootKinds)
			} else {
				t.Logf("%s root kinds (order-locked): %v", tt.methodKey, gotKinds)
			}
		})
	}
}
