// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestDefaultEngineParsePathGoRichSemanticMetadata asserts Go rich-semantic
// metadata: docstring, class_context, and cyclomatic_complexity on a Go
// method. It parses Go source and asserts Go behaviour, so it lives here with
// the other Go engine tests: it sat inside engine_python_semantics_test.go only
// because that file happened to hold it, moved to the parent when the Python
// tests relocated, and came here with the rest of the go_*_test.go family in
// #6062.
func TestDefaultEngineParsePathGoRichSemanticMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "worker.go")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	functionItem := parsertest.AssertBucketItemByName(t, got, "functions", "Work")
	parsertest.AssertStringFieldValue(t, functionItem, "docstring", "Work handles queued jobs.")
	parsertest.AssertStringFieldValue(t, functionItem, "class_context", "Worker")
	parsertest.AssertIntFieldValue(t, functionItem, "cyclomatic_complexity", 3)
}
