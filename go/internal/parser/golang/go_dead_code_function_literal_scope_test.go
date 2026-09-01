// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathGoDoesNotMarkUnusedLocalClosureCalleeAsRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "closures.go")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	helper := parsertest.AssertBucketItemByFieldValue(t, got, "functions", "name", "hiddenHelper")
	if rootKinds := helper["dead_code_root_kinds"]; rootKinds != nil {
		t.Fatalf("dead_code_root_kinds = %#v, want omitted for unused local closure callee", rootKinds)
	}
}

func TestDefaultEngineParsePathGoMarksCallbackClosureCalleeAsRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "closures.go")
	parsertest.WriteFile(
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	helper := parsertest.AssertBucketItemByFieldValue(t, got, "functions", "name", "callbackHelper")
	parsertest.AssertStringSliceEquals(t, helper, "dead_code_root_kinds", []string{"go.function_literal_reachable_call"})
}
