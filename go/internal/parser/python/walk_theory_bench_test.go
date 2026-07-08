// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

// Throwaway prove-theory-first shim for epic #4831 child #4867. Delete after
// recording numbers in the PR.

import (
	"os"
	"path/filepath"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

func pyTheoryCorpus(tb testing.TB) []string {
	tb.Helper()
	root := os.Getenv("PY_THEORY_CORPUS")
	if root == "" {
		root = "../../../../tests/fixtures"
	}
	var paths []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".py" {
			paths = append(paths, p)
		}
		return nil
	})
	if len(paths) == 0 {
		tb.Skipf("no python corpus under %q", root)
	}
	return paths
}

// currentPyWalkCount models always-on full-tree walkNamed passes: dataclass
// names, script-main-guard roots, public-API root kinds, main payload, plus
// framework-semantics route walks (aiohttp/tornado/django/drf).
const currentPyWalkCount = 8

func benchPyWalkPasses(b *testing.B, passes int) {
	paths := pyTheoryCorpus(b)
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		b.Fatalf("SetLanguage(python): %v", err)
	}
	trees := make([]*tree_sitter.Tree, 0, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		tree := parser.Parse(src, nil)
		if tree == nil {
			continue
		}
		trees = append(trees, tree)
	}
	defer func() {
		for _, t := range trees {
			t.Close()
		}
	}()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tree := range trees {
			root := tree.RootNode()
			for pass := 0; pass < passes; pass++ {
				count := 0
				walkNamed(root, func(_ *tree_sitter.Node) { count++ })
				_ = count
			}
		}
	}
}

// BenchmarkPyWalkCurrent times current walk multiplicity (no-op).
func BenchmarkPyWalkCurrent(b *testing.B) { benchPyWalkPasses(b, currentPyWalkCount) }

// BenchmarkPyWalkSingle times a single consolidated walk (no-op).
func BenchmarkPyWalkSingle(b *testing.B) { benchPyWalkPasses(b, 1) }
