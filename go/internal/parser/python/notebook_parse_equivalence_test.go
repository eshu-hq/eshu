// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// notebookEquivalenceFixture is a small but structurally varied notebook:
// markdown/raw/blank cells that must be dropped, and code cells covering an
// import, a class, a function, and a call, so Parse's ipynb branch exercises
// the same shape the payload buckets assert on.
const notebookEquivalenceFixture = `{
  "cells": [
    {"cell_type": "markdown", "source": ["# Analysis\n"]},
    {"cell_type": "raw", "source": ["ignored raw cell\n"]},
    {"cell_type": "code", "source": []},
    {
      "cell_type": "code",
      "source": [
        "import os\n",
        "\n",
        "class NotebookGreeter:\n",
        "    def greet(self, name):\n",
        "        return os.path.join(name, \"child\")\n"
      ]
    },
    {
      "cell_type": "code",
      "source": [
        "def hello(name):\n",
        "    return NotebookGreeter().greet(name)\n",
        "\n",
        "hello(\"world\")\n"
      ]
    }
  ],
  "metadata": {},
  "nbformat": 4,
  "nbformat_minor": 5
}
`

// TestParseNotebookMatchesEquivalentPythonSource pins Parse's .ipynb output
// against Parse's output for the equivalent, already-converted .py source
// (issue #4874's payload-equivalence gate for the in-memory ipynb refactor).
// The two payloads must be identical except for the "path" field, which
// legitimately differs by extension. This invariant holds both before and
// after the temp-file round trip is replaced by in-memory conversion: it is
// the 0/0 proof that removing the disk round trip does not change parser
// truth.
func TestParseNotebookMatchesEquivalentPythonSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	notebookPath := filepath.Join(repoRoot, "analysis.ipynb")
	if err := os.WriteFile(notebookPath, []byte(notebookEquivalenceFixture), 0o600); err != nil {
		t.Fatalf("write notebook fixture: %v", err)
	}

	convertedSource, err := NotebookSource([]byte(notebookEquivalenceFixture))
	if err != nil {
		t.Fatalf("NotebookSource() error = %v, want nil", err)
	}
	pythonPath := filepath.Join(repoRoot, "analysis.py")
	if err := os.WriteFile(pythonPath, []byte(convertedSource), 0o600); err != nil {
		t.Fatalf("write converted python fixture: %v", err)
	}

	notebookParser := newEquivalenceTestPythonParser(t)
	notebookPayload, err := Parse(repoRoot, notebookPath, false, shared.Options{}, notebookParser)
	if err != nil {
		t.Fatalf("Parse(notebook) error = %v, want nil", err)
	}

	pythonParser := newEquivalenceTestPythonParser(t)
	pythonPayload, err := Parse(repoRoot, pythonPath, false, shared.Options{}, pythonParser)
	if err != nil {
		t.Fatalf("Parse(converted python) error = %v, want nil", err)
	}

	// "path" legitimately differs by extension; normalize it before comparing
	// the rest of the payload byte-for-byte.
	notebookPayload["path"] = "NORMALIZED"
	pythonPayload["path"] = "NORMALIZED"

	if !reflect.DeepEqual(notebookPayload, pythonPayload) {
		t.Fatalf(
			"Parse(notebook) != Parse(converted python)\nnotebook: %#v\npython:   %#v",
			notebookPayload, pythonPayload,
		)
	}
}

// TestPreScanNotebookMatchesEquivalentPythonSource pins PreScan's .ipynb
// output against PreScan's output for the equivalent, already-converted .py
// source, mirroring TestParseNotebookMatchesEquivalentPythonSource for the
// collector import-map pre-scan path (prescan.go), which has the same
// temp-file round trip as language.go's Parse.
func TestPreScanNotebookMatchesEquivalentPythonSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	notebookPath := filepath.Join(repoRoot, "analysis.ipynb")
	if err := os.WriteFile(notebookPath, []byte(notebookEquivalenceFixture), 0o600); err != nil {
		t.Fatalf("write notebook fixture: %v", err)
	}

	convertedSource, err := NotebookSource([]byte(notebookEquivalenceFixture))
	if err != nil {
		t.Fatalf("NotebookSource() error = %v, want nil", err)
	}
	pythonPath := filepath.Join(repoRoot, "analysis.py")
	if err := os.WriteFile(pythonPath, []byte(convertedSource), 0o600); err != nil {
		t.Fatalf("write converted python fixture: %v", err)
	}

	notebookNames, err := PreScan(notebookPath, newEquivalenceTestPythonParser(t))
	if err != nil {
		t.Fatalf("PreScan(notebook) error = %v, want nil", err)
	}
	pythonNames, err := PreScan(pythonPath, newEquivalenceTestPythonParser(t))
	if err != nil {
		t.Fatalf("PreScan(converted python) error = %v, want nil", err)
	}

	if !reflect.DeepEqual(notebookNames, pythonNames) {
		t.Fatalf("PreScan(notebook) = %#v, want %#v (PreScan(converted python))", notebookNames, pythonNames)
	}
}

func newEquivalenceTestPythonParser(t *testing.T) *tree_sitter.Parser {
	t.Helper()

	parser := tree_sitter.NewParser()
	t.Cleanup(parser.Close)
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	return parser
}
