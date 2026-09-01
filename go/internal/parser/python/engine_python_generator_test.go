// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPythonGeneratorFunctionsEmitSemanticKind(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "generators.py")
	writeTestFile(
		t,
		filePath,
		`def create_ids():
    yield 1
    yield 2
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

	fn := parsertest.AssertBucketItemByName(t, got, "functions", "create_ids")
	parsertest.AssertStringFieldValue(t, fn, "semantic_kind", "generator")
}

func TestDefaultEngineParsePathPythonGeneratorYieldInNestedFunctionStaysInnerOnly(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "nested_generators.py")
	writeTestFile(
		t,
		filePath,
		`def outer():
    def inner():
        yield 1
    return 1

def create_ids():
    yield 1
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

	outer := parsertest.AssertBucketItemByName(t, got, "functions", "outer")
	if _, ok := outer["semantic_kind"]; ok {
		t.Fatalf("outer semantic_kind = %#v, want absent", outer["semantic_kind"])
	}

	inner := parsertest.AssertBucketItemByName(t, got, "functions", "inner")
	parsertest.AssertStringFieldValue(t, inner, "semantic_kind", "generator")

	createIDs := parsertest.AssertBucketItemByName(t, got, "functions", "create_ids")
	parsertest.AssertStringFieldValue(t, createIDs, "semantic_kind", "generator")
}
