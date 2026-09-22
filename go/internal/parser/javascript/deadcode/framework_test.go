// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
)

// stubFrameworkEvidence answers as the parent package's real implementation does
// for the fixture below: it names "app" as an Express server symbol, which is
// what lets RegisteredDeadCodeRootKinds walk past its early return and reach the
// route registration. It exists to give the nil case a positive control.
type stubFrameworkEvidence struct{}

func (stubFrameworkEvidence) RegisteredRootKinds(
	*tree_sitter.Node,
	[]byte,
	map[string]struct{},
	map[string]struct{},
	map[string]struct{},
) map[string][]string {
	return nil
}

func (stubFrameworkEvidence) IsControllerMethod(*tree_sitter.Node, []byte, *syntax.ParentLookup) bool {
	return false
}

func (stubFrameworkEvidence) ExpressSemantics(*tree_sitter.Node, []byte) (map[string]any, bool) {
	return map[string]any{"server_symbols": []string{"app"}}, true
}

const nilFrameworkFixture = `import express from "express";
const app = express();
app.get("/health", handler);
function handler(req, res) { res.send("ok"); }
`

// TestNilFrameworkEvidenceAnswersAbsentRatherThanPanicking pins FrameworkEvidence
// to the same nil contract SiblingSource already carries.
//
// Issue #6771 replaced direct calls to the parent package's framework helpers
// with this interface. SiblingSource got a nil-safe rootForFile wrapper;
// FrameworkEvidence did not, so a nil interface panicked where the pre-split
// direct call could not. RegisteredDeadCodeRootKinds is exported and already
// called from outside this package, so any caller can reach that nil.
//
// The subtests run in the order they must be read: the stub case proves the
// seam is genuinely consulted for this fixture, so the nil case returning empty
// means the wrapper answered, not that an earlier guard short-circuited. An
// earlier version of this test "controlled" for that with a strings.Contains
// assertion over a const, which could not fail and proved nothing.
func TestNilFrameworkEvidenceAnswersAbsentRatherThanPanicking(t *testing.T) {
	language := tree_sitter.NewLanguage(tree_sitter_javascript.Language())
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	defer parser.Close()

	source := []byte(nilFrameworkFixture)
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree")
	}
	defer tree.Close()
	root := tree.RootNode()

	t.Run("control: a real implementation reaches the seam", func(t *testing.T) {
		got := RegisteredDeadCodeRootKinds(root, source, stubFrameworkEvidence{})
		if len(got["handler"]) == 0 {
			t.Fatalf("RegisteredDeadCodeRootKinds with a stub = %v, want an entry for \"handler\"; "+
				"without this the nil case below proves nothing", got)
		}
	})

	t.Run("nil answers absent", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("nil FrameworkEvidence panicked instead of answering absent: %v", recovered)
			}
		}()

		if got := RegisteredDeadCodeRootKinds(root, source, nil); len(got) != 0 {
			t.Errorf("RegisteredDeadCodeRootKinds with a nil FrameworkEvidence = %v, want empty", got)
		}
	})
}
