// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinMarksSuspendFunctions(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Coroutine.kt")
	writeKotlinTestFile(
		t,
		filePath,
		`package comprehensive

class Worker {
    suspend fun load(): String = "ok"
}

suspend fun fetchRemote(): String = "remote"

fun regular(): String = "done"
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

	load := parsertest.AssertBucketItemByName(t, got, "functions", "load")
	assertBoolFieldValue(t, load, "suspend", true)

	fetchRemote := parsertest.AssertBucketItemByName(t, got, "functions", "fetchRemote")
	assertBoolFieldValue(t, fetchRemote, "suspend", true)
}
