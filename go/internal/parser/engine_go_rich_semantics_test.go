// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathGoRichSemanticMetadata asserts Go rich-semantic
// metadata: docstring, class_context, and cyclomatic_complexity on a Go
// method. It lives in the parent package, not in internal/parser/python: it
// parses Go source and asserts Go behaviour, and it sat inside
// engine_python_semantics_test.go only because that file happened to hold it.
// The #6062 relocation would otherwise have carried a Go-language test into
// the Python package and made docs/public/languages/go.md cite it there.

func TestDefaultEngineParsePathGoRichSemanticMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "worker.go")
	writeTestFile(
		t,
		filePath,
		`package worker

type Worker struct{}

// Work handles queued jobs.
func (w *Worker) Work(name string) int {
	if name == "" {
		return 0
	}
	for range name {
	}
	return len(name)
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := assertFunctionByName(t, got, "Work")
	assertStringFieldValue(t, functionItem, "docstring", "Work handles queued jobs.")
	assertStringFieldValue(t, functionItem, "class_context", "Worker")
	assertIntFieldValue(t, functionItem, "cyclomatic_complexity", 3)
}
