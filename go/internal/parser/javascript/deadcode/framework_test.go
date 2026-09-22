// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"slices"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
)

// stubFrameworkEvidence answers each FrameworkEvidence method from a field, so a
// test can prove a seam is genuinely consulted before asserting what a nil does
// there. Without such a control, a nil assertion passes equally well when an
// earlier guard returned before the seam was ever reached.
type stubFrameworkEvidence struct {
	rootKinds  map[string][]string
	controller bool
	express    map[string]any
	expressOK  bool
}

func (s stubFrameworkEvidence) RegisteredRootKinds(
	*tree_sitter.Node,
	[]byte,
	map[string]struct{},
	map[string]struct{},
	map[string]struct{},
) map[string][]string {
	return s.rootKinds
}

func (s stubFrameworkEvidence) IsControllerMethod(*tree_sitter.Node, []byte, *syntax.ParentLookup) bool {
	return s.controller
}

func (s stubFrameworkEvidence) ExpressSemantics(*tree_sitter.Node, []byte) (map[string]any, bool) {
	return s.express, s.expressOK
}

// expressStub reaches the express seam for the fixture below by naming "app" as
// a server symbol, which is what lets RegisteredDeadCodeRootKinds walk past its
// early return.
func expressStub() stubFrameworkEvidence {
	return stubFrameworkEvidence{
		express:   map[string]any{"server_symbols": []string{"app"}},
		expressOK: true,
	}
}

const nilFrameworkFixture = `import express from "express";
const app = express();
app.get("/health", handler);
function handler(req, res) { res.send("ok"); }
`

// parseNilFrameworkFixture returns the fixture's tree, source and parent lookup.
// The caller closes the returned tree through t.Cleanup.
func parseNilFrameworkFixture(t *testing.T) (*tree_sitter.Node, []byte, *syntax.ParentLookup) {
	t.Helper()

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	t.Cleanup(parser.Close)

	source := []byte(nilFrameworkFixture)
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree")
	}
	t.Cleanup(tree.Close)

	root := tree.RootNode()
	return root, source, syntax.BuildParentLookup(root)
}

// handlerDeclaration returns the `function handler(...)` node from the fixture.
func handlerDeclaration(t *testing.T, root *tree_sitter.Node) *tree_sitter.Node {
	t.Helper()

	cursor := root.Walk()
	defer cursor.Close()
	for _, child := range root.NamedChildren(cursor) {
		if child.Kind() == "function_declaration" {
			found := child
			return &found
		}
	}
	t.Fatal("fixture has no function_declaration; the RootKinds probe would prove nothing")
	return nil
}

// TestNilFrameworkEvidenceAnswersAbsentRatherThanPanicking pins FrameworkEvidence
// to the same nil contract SiblingSource already carries, at all three seams.
//
// Issue #6771 replaced direct calls to the parent javascript package's framework
// helpers with this interface. SiblingSource got a nil-safe rootForFile wrapper;
// FrameworkEvidence did not, so a nil interface panicked where the pre-split
// direct call could not. RootEvidence, RootKinds and RegisteredDeadCodeRootKinds
// are all exported and reachable with a nil from any caller.
//
// One subtest per wrapper, because the wrappers fail independently: deleting the
// guard from any one of the three leaves the other two covering for it, and a
// mutation that removes all three at once only proves that at least one guard is
// load-bearing. Each subtest runs its control first, so a nil answering "absent"
// means the wrapper answered rather than an earlier guard short-circuiting.
func TestNilFrameworkEvidenceAnswersAbsentRatherThanPanicking(t *testing.T) {
	t.Run("expressSemantics, via RegisteredDeadCodeRootKinds", func(t *testing.T) {
		root, source, _ := parseNilFrameworkFixture(t)

		if got := RegisteredDeadCodeRootKinds(root, source, expressStub()); len(got["handler"]) == 0 {
			t.Fatalf("control: with a stub = %v, want an entry for \"handler\"; "+
				"without this the nil case proves nothing", got)
		}

		defer expectNoPanic(t, "RegisteredDeadCodeRootKinds")
		if got := RegisteredDeadCodeRootKinds(root, source, nil); len(got) != 0 {
			t.Errorf("RegisteredDeadCodeRootKinds with a nil FrameworkEvidence = %v, want empty", got)
		}
	})

	t.Run("frameworkRootKinds, via RootEvidence", func(t *testing.T) {
		root, source, parents := parseNilFrameworkFixture(t)
		const marker = "javascript.stub_route_registration"

		stub := stubFrameworkEvidence{rootKinds: map[string][]string{"handler": {marker}}}
		control := RootEvidence("", "server.js", root, source, nil, parents, nil, nil, nil, stub)
		if !slices.Contains(RootKinds("server.js", handlerDeclaration(t, root), "handler", source, control), marker) {
			t.Fatalf("control: RootEvidence did not carry the stub's root kind through to RootKinds; "+
				"the %s seam is not reached by this fixture and the nil case proves nothing", marker)
		}

		defer expectNoPanic(t, "RootEvidence")
		evidence := RootEvidence("", "server.js", root, source, nil, parents, nil, nil, nil, nil)
		if got := RootKinds("server.js", handlerDeclaration(t, root), "handler", source, evidence); len(got) != 0 {
			t.Errorf("RootKinds after a nil-framework RootEvidence = %v, want empty", got)
		}
	})

	t.Run("isControllerMethod, via RootKinds", func(t *testing.T) {
		root, source, parents := parseNilFrameworkFixture(t)
		const controllerKind = "javascript.nestjs_controller_method"

		stub := stubFrameworkEvidence{controller: true}
		control := RootEvidence("", "server.js", root, source, nil, parents, nil, nil, nil, stub)
		if !slices.Contains(RootKinds("server.js", handlerDeclaration(t, root), "handler", source, control), controllerKind) {
			t.Fatalf("control: RootKinds did not report %s for a stub that answers true; "+
				"the seam is not reached and the nil case proves nothing", controllerKind)
		}

		defer expectNoPanic(t, "RootKinds")
		evidence := RootEvidence("", "server.js", root, source, nil, parents, nil, nil, nil, nil)
		if got := RootKinds("server.js", handlerDeclaration(t, root), "handler", source, evidence); slices.Contains(got, controllerKind) {
			t.Errorf("RootKinds with a nil FrameworkEvidence = %v, want no %s", got, controllerKind)
		}
	})
}

// expectNoPanic fails the test if the deferred scope panicked, naming the call
// that did it. A nil at either seam must answer absence, not panic.
func expectNoPanic(t *testing.T, call string) {
	t.Helper()
	if recovered := recover(); recovered != nil {
		t.Fatalf("%s with a nil FrameworkEvidence panicked instead of answering absent: %v", call, recovered)
	}
}
