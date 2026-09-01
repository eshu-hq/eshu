// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEnginePreScanRepositoryPathsKotlinStaysWithinRepoRoot(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	repoRoot := filepath.Join(tempRoot, "repo")
	restrictedDir := filepath.Join(tempRoot, "restricted")
	targetPath := filepath.Join(repoRoot, "src", "Basic.kt")
	siblingPath := filepath.Join(repoRoot, "src", "Sibling.kt")

	writeKotlinTestFile(t, targetPath, `package sample

class Greeter
`)
	writeKotlinTestFile(t, siblingPath, `package sample

fun helper(): String = "ok"
`)
	if err := os.MkdirAll(restrictedDir, 0o000); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", restrictedDir, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(restrictedDir, 0o755)
	})

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanRepositoryPaths(repoRoot, []string{targetPath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	parsertest.AssertPrescanContains(t, got, "Greeter", targetPath)
}

func TestDefaultEngineParsePathKotlinStaysWithinRepoRoot(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	repoRoot := filepath.Join(tempRoot, "repo")
	restrictedDir := filepath.Join(tempRoot, "restricted")
	targetPath := filepath.Join(repoRoot, "src", "Basic.kt")
	siblingPath := filepath.Join(repoRoot, "src", "Sibling.kt")

	writeKotlinTestFile(t, targetPath, `package sample

class Greeter {
    fun greet(): String = helper()
}
`)
	writeKotlinTestFile(t, siblingPath, `package sample

fun helper(): String = "ok"
`)
	if err := os.MkdirAll(restrictedDir, 0o000); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", restrictedDir, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(restrictedDir, 0o755)
	})

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, targetPath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertNamedBucketContains(t, got, "classes", "Greeter")
}
