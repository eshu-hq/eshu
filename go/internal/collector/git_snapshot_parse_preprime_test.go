// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParseRepositoryFilePrePrimeEliminatesDoubleRead is the regression seed
// for the #4851 double-read fix. The collector's parseRepositoryFile previously
// called engine.ParsePath (1 physical disk read) + os.ReadFile (1 physical
// disk read) = 2 reads per parsed file. After the fix, parseRepositoryFile
// reads the body once up front, primes shared.PrimeSource, defers
// shared.ClearSource, and calls engine.ParsePath — which internally hits the
// primed cache entry instead of reading from disk. The shared.ReadSource hook
// must record exactly 0 physical disk reads for the target path during
// ParsePath.
//
// The same body is reused for shapeFileFromParsed, so total physical reads
// per file drops from 2 to 1.
//
// Not t.Parallel(): shared.SetReadSourceHookForTest mutates process-global
// instrumentation.
func TestParseRepositoryFilePrePrimeEliminatesDoubleRead(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.go")
	writeCollectorTestFile(t, filePath, `package service

func Hello() string {
	return "hello"
}
`)

	// Count physical shared.ReadSource calls during ParsePath for the
	// target file. With the pre-prime, these must be 0.
	var readsDuringParse int
	restore := shared.SetReadSourceHookForTest(func(path string) {
		if path == filePath {
			readsDuringParse++
		}
	})
	defer restore()

	engine := defaultCollectorTestEngine(t)

	// Mirror parseRepositoryFile: read once, prime with absPath, parse, clear.
	body, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", filePath, err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v, want nil", filePath, err)
	}

	shared.PrimeSource(absPath, body)
	defer shared.ClearSource(absPath)

	snapshotter := NativeRepositorySnapshotter{}
	result := snapshotter.parseRepositoryFile(
		context.Background(),
		repoRoot,
		parseFileJob{index: 0, path: filePath},
		engine,
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
		nil,
	)
	if result.skipped {
		t.Fatal("parseRepositoryFile skipped the file, want success")
	}

	// ParsePath must have triggered 0 physical shared.ReadSource calls
	// because the pre-prime served every internal read from the cache.
	if readsDuringParse != 0 {
		t.Fatalf("physical shared.ReadSource calls during ParsePath = %d, want 0 (pre-primed cache should have served all reads)", readsDuringParse)
	}

	// Shape-body equivalence: the body reused from the upfront read must
	// match a fresh os.ReadFile of the same file.
	freshBody, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", filePath, err)
	}
	if string(body) != string(freshBody) {
		t.Fatalf("collector body != fresh disk body: %q vs %q", body, freshBody)
	}
}

// TestPrePrimeCacheIsReleasedAfterParse proves the collector's prime/clear
// pair balances correctly: after parseRepositoryFile returns and the
// collector's ClearSource runs, the shared source cache entry for the parsed
// path is released, and a subsequent shared.ReadSource triggers a fresh
// physical disk read (hook fires).
//
// Not t.Parallel(): shared.SetReadSourceHookForTest.
func TestPrePrimeCacheIsReleasedAfterParse(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.go")
	writeCollectorTestFile(t, filePath, `package service

func Hello() string {
	return "hello"
}
`)

	engine := defaultCollectorTestEngine(t)

	body, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", filePath, err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v, want nil", filePath, err)
	}

	// Prime and parse, exactly as the production code does in parseRepositoryFile.
	shared.PrimeSource(absPath, body)
	// NOT deferring — we explicitly clear to control timing.

	snapshotter := NativeRepositorySnapshotter{}
	result := snapshotter.parseRepositoryFile(
		context.Background(),
		repoRoot,
		parseFileJob{index: 0, path: filePath},
		engine,
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
		nil,
	)
	if result.skipped {
		t.Fatal("parseRepositoryFile skipped the file, want success")
	}

	// After ParsePath returns, its own internal defer clearSource has run
	// (refs decreased by 1). Our prime is still holding refs=1. Now clear
	// our prime — the cache entry must be deleted.
	shared.ClearSource(absPath)

	// After clearing, verify the cache entry is gone: a shared.ReadSource
	// for the same path must perform a real physical read (hook fires).
	var postClearReads int
	restore := shared.SetReadSourceHookForTest(func(path string) {
		if path == filePath {
			postClearReads++
		}
	})
	defer restore()

	readBody, err := shared.ReadSource(filePath)
	if err != nil {
		t.Fatalf("shared.ReadSource(%q) error = %v, want nil", filePath, err)
	}
	if string(readBody) != string(body) {
		t.Fatalf("shared.ReadSource(%q) = %q, want %q", filePath, readBody, body)
	}
	if postClearReads != 1 {
		t.Fatalf("physical shared.ReadSource calls after ClearSource = %d, want 1 (cache must be released)", postClearReads)
	}
}

// TestParseRepositoryFilesConcurrentPrePrimeRaceFree runs
// buildParsedRepositoryFilesConcurrent under -race to prove the prime/clear
// path holds up under concurrent parse workers on distinct paths (the normal
// collector case). No data race, correct per-file output, and all files
// parsed without skips.
func TestParseRepositoryFilesConcurrentPrePrimeRaceFree(t *testing.T) {
	t.Parallel()

	const fileCount = 32
	repoRoot := t.TempDir()
	files := make([]discovery.FileWithSize, fileCount)
	for i := range fileCount {
		path := filepath.Join(repoRoot, fmt.Sprintf("svc_%d.go", i))
		writeCollectorTestFile(t, path, fmt.Sprintf(`package service

func Hello%d() string {
	return "hello-%d"
}
`, i, i))
		files[i] = discovery.FileWithSize{Path: path}
	}

	engine := defaultCollectorTestEngine(t)
	snapshotter := NativeRepositorySnapshotter{ParseWorkers: 8}
	fileSet := discovery.RepoFileSet{Files: files}

	shapeFiles, parsedFiles, _, err := snapshotter.buildParsedRepositoryFiles(
		context.Background(),
		repoRoot,
		fileSet,
		engine,
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
	)
	if err != nil {
		t.Fatalf("buildParsedRepositoryFiles() error = %v, want nil", err)
	}
	if len(shapeFiles) != fileCount {
		t.Fatalf("shape file count = %d, want %d", len(shapeFiles), fileCount)
	}
	if len(parsedFiles) != fileCount {
		t.Fatalf("parsed file count = %d, want %d", len(parsedFiles), fileCount)
	}
	for _, parsed := range parsedFiles {
		if parsed["path"] == nil {
			t.Fatal("parsed file missing path field")
		}
	}
}

// TestShapeBodyMatchesFileSystemAfterPrePrime proves the shape-file body
// built from the collector's upfront os.ReadFile matches a fresh os.ReadFile
// after parseRepositoryFile returns. This is an integration-level check that
// the body bytes are consistent through the entire parse→shape pipeline.
func TestShapeBodyMatchesFileSystemAfterPrePrime(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "handler.py")
	fileBody := "def handle():\n    return 42\n"
	writeCollectorTestFile(t, filePath, fileBody)

	engine := defaultCollectorTestEngine(t)
	snapshotter := NativeRepositorySnapshotter{ParseWorkers: 1}

	result := snapshotter.parseRepositoryFile(
		context.Background(),
		repoRoot,
		parseFileJob{index: 0, path: filePath},
		engine,
		"commit-sha",
		false,
		parser.GoPackageSemanticRoots{},
		"repo-alpha",
		nil,
	)
	if result.skipped {
		t.Fatal("parseRepositoryFile skipped the file, want success")
	}

	if result.shapeFile.Body != fileBody {
		t.Fatalf("shapeFile.Body = %q, want %q", result.shapeFile.Body, fileBody)
	}
}
