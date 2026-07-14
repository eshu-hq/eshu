// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// benchmarkNotebookFixture returns a synthetic but representative .ipynb
// document: 40 code cells, each with a small function and a call, matching
// the shape of a real analysis notebook rather than a single trivial cell.
func benchmarkNotebookFixture(b *testing.B) string {
	b.Helper()

	type cell struct {
		CellType string   `json:"cell_type"`
		Source   []string `json:"source"`
	}
	cells := make([]cell, 0, 40)
	for i := 0; i < 40; i++ {
		cells = append(cells, cell{
			CellType: "code",
			Source: []string{
				"import os\n",
				"\n",
				"def step_" + itoa(i) + "(value):\n",
				"    total = value + " + itoa(i) + "\n",
				"    return os.path.join(str(total), \"out\")\n",
				"\n",
				"step_" + itoa(i) + "(" + itoa(i) + ")\n",
			},
		})
	}

	notebook := map[string]any{
		"cells":          cells,
		"metadata":       map[string]any{},
		"nbformat":       4,
		"nbformat_minor": 5,
	}
	body, err := json.Marshal(notebook)
	if err != nil {
		b.Fatalf("marshal notebook fixture: %v", err)
	}

	path := filepath.Join(b.TempDir(), "analysis.ipynb")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		b.Fatalf("write notebook fixture: %v", err)
	}
	return path
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func newBenchmarkPythonParser(b *testing.B) *tree_sitter.Parser {
	b.Helper()

	parser := tree_sitter.NewParser()
	b.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		b.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	return parser
}

// BenchmarkParseNotebookTempFileRoundTrip is the Prove-The-Theory-First
// evidence for issue #4874's .ipynb disk round-trip removal: it measures
// Parse() on a representative 40-cell notebook with -benchmem. Run before the
// in-memory refactor it reports the temp-file write+read+remove cost baked
// into Parse's .ipynb branch; run after, it reports the in-memory conversion
// cost only, on the identical fixture and parser setup.
func BenchmarkParseNotebookTempFileRoundTrip(b *testing.B) {
	path := benchmarkNotebookFixture(b)
	repoRoot := filepath.Dir(path)
	parser := newBenchmarkPythonParser(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(repoRoot, path, false, shared.Options{}, parser); err != nil {
			b.Fatalf("Parse() error = %v, want nil", err)
		}
	}
}
