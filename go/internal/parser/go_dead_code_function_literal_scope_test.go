package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathGoDoesNotMarkUnusedLocalClosureCalleeAsRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "closures.go")
	writeTestFile(
		t,
		filePath,
		`package main

func hiddenHelper() {}

func configure() {
	unused := func() {
		hiddenHelper()
	}
	_ = unused
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

	helper := assertBucketItemByFieldValue(t, got, "functions", "name", "hiddenHelper")
	if rootKinds := helper["dead_code_root_kinds"]; rootKinds != nil {
		t.Fatalf("dead_code_root_kinds = %#v, want omitted for unused local closure callee", rootKinds)
	}
}

func TestDefaultEngineParsePathGoMarksCallbackClosureCalleeAsRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "closures.go")
	writeTestFile(
		t,
		filePath,
		`package main

func runCallback(callback func()) {
	callback()
}

func callbackHelper() {}

func configure() {
	runCallback(func() {
		callbackHelper()
	})
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

	helper := assertBucketItemByFieldValue(t, got, "functions", "name", "callbackHelper")
	assertParserStringSliceFieldValue(t, helper, "dead_code_root_kinds", []string{"go.function_literal_reachable_call"})
}
