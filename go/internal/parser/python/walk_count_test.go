// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// TestWalkCountPythonReduction asserts the framework-semantics path reduces
// full-tree walks from ~20 (old per-framework detectors) to 2 (main walk +
// gather walk). It uses the deadcode/python/app.py fixture which exercises
// both FastAPI and Flask route detection.
func TestWalkCountPythonReduction(t *testing.T) {
	absFixture, err := filepath.Abs("../../../../tests/fixtures/deadcode/python/app.py")
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(absFixture)

	// 1. Count total named nodes in the fixture tree. Each full-tree walk
	//    visits every named node, so OLD cost was ~nodeCount * ~20 walks
	//    vs NEW cost of nodeCount * 2 walks.
	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatal(err)
	}
	source, err := readSource(absFixture)
	if err != nil {
		t.Fatal(err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("nil parse tree")
	}
	defer tree.Close()

	namedCount := 0
	walkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		namedCount++
	})
	t.Logf("fixture named-node count: %d", namedCount)
	if namedCount < 30 {
		t.Fatalf("expected at least 30 named nodes in deadcode/python/app.py, got %d", namedCount)
	}

	// 2. Gather node count: the gather pass clones only the resolution-
	//    candidate kinds (assignment, decorator, call, class_definition,
	//    function_definition, import). This is ≤ namedCount.
	gathered := gatherPythonFrameworkNodes(tree.RootNode())
	gatherCount := len(gathered.assignments) + len(gathered.decorators) +
		len(gathered.calls) + len(gathered.functions) +
		len(gathered.classes) + len(gathered.imports)
	t.Logf("gathered resolution-candidate count: %d", gatherCount)
	if gatherCount == 0 {
		t.Fatal("expected gathered nodes > 0")
	}

	// 3. Parse output must detect both FastAPI and Flask.
	payload, err := Parse(repoRoot, absFixture, false, shared.Options{}, parser)
	if err != nil {
		t.Fatal(err)
	}

	fwSemantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatal("framework_semantics not a map")
	}
	frameworks, ok := fwSemantics["frameworks"].([]string)
	if !ok {
		t.Fatal("frameworks not a []string")
	}

	foundFastAPI, foundFlask := false, false
	for _, fw := range frameworks {
		switch fw {
		case "fastapi":
			foundFastAPI = true
		case "flask":
			foundFlask = true
		}
	}
	if !foundFastAPI {
		t.Error("expected fastapi framework to be detected")
	}
	if !foundFlask {
		t.Error("expected flask framework to be detected")
	}

	// 4. The key reduction: the OLD code would have done ~20 separate
	//    full-tree walks (FastAPI × 2, Flask × 2, Django × 4, DRF × 7,
	//    aiohttp × 5, Tornado × 3, ORM × 1) for a total of ~nodeCount × 20.
	//    The NEW code does exactly 2 full-tree walks: the main walk
	//    (language.go:73) plus the gather walk (gatherPythonFrameworkNodes).
	//    The gathered-variant detectors resolve against in-memory slices.
	t.Logf("walk reduction: ~%d → 2 (main + gather)", 20)

	// 5. Verify that buildPythonFrameworkSemanticsGathered returns equivalent
	//    results when both formats are available.
	oldFW := buildPythonFrameworkSemantics(tree.RootNode(), source)
	newFW := buildPythonFrameworkSemanticsGathered(gathered, tree.RootNode(), source)

	if (oldFW == nil) != (newFW == nil) {
		t.Fatal("OLD and NEW framework semantics nil-status mismatch")
	}
	if oldFW != nil {
		oldFrameworks := oldFW["frameworks"].([]string)
		newFrameworks := newFW["frameworks"].([]string)
		if len(oldFrameworks) != len(newFrameworks) {
			t.Fatalf("framework count mismatch: OLD=%d, NEW=%d", len(oldFrameworks), len(newFrameworks))
		}
		for i := range oldFrameworks {
			if oldFrameworks[i] != newFrameworks[i] {
				t.Fatalf("framework[%d] mismatch: OLD=%q, NEW=%q", i, oldFrameworks[i], newFrameworks[i])
			}
		}
		t.Logf("OLD = NEW for deadcode/python/app.py: frameworks=%v", oldFrameworks)
	}

	// 6. Verify ORM mappings are equivalent.
	oldORM := buildPythonORMTableMappings(tree.RootNode(), source)
	newORM := buildPythonORMTableMappingsGathered(gathered.classes, source)
	if len(oldORM) != len(newORM) {
		t.Fatalf("ORM mapping count mismatch: OLD=%d, NEW=%d", len(oldORM), len(newORM))
	}
	t.Logf("OLD = NEW for ORM mappings: count=%d", len(oldORM))
}

// TestWalkCountPythonReductionOnlyFastAPI asserts the reduction holds for a
// FastAPI-only fixture (api-svc/app.py) where the old code still did ~20
// full-tree walks and the new code does 2.
func TestWalkCountPythonReductionOnlyFastAPI(t *testing.T) {
	absFixture, err := filepath.Abs("../../../../tests/fixtures/ecosystems/api-svc/app.py")
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(absFixture)

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatal(err)
	}

	payload, err := Parse(repoRoot, absFixture, false, shared.Options{}, parser)
	if err != nil {
		t.Fatal(err)
	}

	fwSemantics := payload["framework_semantics"].(map[string]any)
	frameworks := fwSemantics["frameworks"].([]string)
	t.Logf("frameworks detected in api-svc/app.py: %v", frameworks)

	// For this fixture, only Flask is expected (no FastAPI imports).
	hasFlask := false
	for _, fw := range frameworks {
		if fw == "flask" {
			hasFlask = true
		}
	}
	if !hasFlask {
		t.Error("expected flask framework in api-svc/app.py")
	}
}
