// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParsePathReadsSourceExactlyOnce is the regression seed for the #4515
// front-half-throughput dedup: ParsePath used to read one file's bytes twice
// per call (once inside the language parser via shared.ReadSource, once again
// in ParsePath for inferContentMetadata). It must read exactly once so
// per-file I/O is not doubled across the front-half ingest path.
//
// Not t.Parallel(): shared.SetReadSourceHookForTest mutates package-level test
// instrumentation in the shared package that every test in this file installs
// and restores, so these tests must not race each other or any other test that
// installs the same hook.
func TestParsePathReadsSourceExactlyOnce(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.go")
	writeTestFile(t, filePath, `package service

func Hello() string {
	return "hello"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var reads int
	restore := shared.SetReadSourceHookForTest(func(path string) {
		if path == filePath {
			reads++
		}
	})
	defer restore()

	if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if reads != 1 {
		t.Fatalf("physical disk reads for %q = %d, want 1", filePath, reads)
	}
}

// TestParsePathConcurrentDistinctPathsDoNotShareCache mirrors the collector's
// production usage in git_snapshot_parse_partitions.go, where a worker pool
// calls ParsePath concurrently for distinct file paths from one snapshot. The
// single-read cache is keyed by absolute path and cleared via defer before
// ParsePath returns, so concurrent calls on distinct paths must never observe
// each other's primed bytes or leave a stale entry behind. Run with -race.
func TestParsePathConcurrentDistinctPathsDoNotShareCache(t *testing.T) {
	t.Parallel()

	const fileCount = 32
	repoRoot := t.TempDir()
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	paths := make([]string, fileCount)
	for i := range fileCount {
		path := filepath.Join(repoRoot, fmt.Sprintf("service_%d.go", i))
		writeTestFile(t, path, fmt.Sprintf(`package service

func Hello%d() string {
	return "hello-%d"
}
`, i, i))
		paths[i] = path
	}

	var wg sync.WaitGroup
	errs := make([]error, fileCount)
	for i, path := range paths {
		wg.Add(1)
		go func(index int, path string) {
			defer wg.Done()
			payload, err := engine.ParsePath(repoRoot, path, false, Options{})
			if err != nil {
				errs[index] = err
				return
			}
			if payload["path"] != path {
				errs[index] = fmt.Errorf("path = %#v, want %#v", payload["path"], path)
			}
		}(i, path)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", paths[i], err)
		}
	}
}

// TestParsePathConcurrentSamePathDoesNotTearReads is an integration-level
// companion to the #4657 codex P2 finding covered deterministically by
// shared.TestPrimeSourceFirstWriterWinsUnderRefcount and
// shared.TestClearSourceSurvivesUntilLastRefcountDecrement: the single-read
// cache in shared.go is a global keyed by absolute path, so two concurrent
// Engine.ParsePath calls on the SAME path interleave PrimeSource/ClearSource.
// Under the pre-fix sync.Map, one goroutine's ClearSource could delete
// another in-flight goroutine's primed entry mid-parse, and one goroutine's
// PrimeSource could overwrite another's bytes outright, letting a single
// ParsePath call mix the language parser's bytes (one version) with
// inferContentMetadata's bytes (a different version) -- a torn read within
// one call.
//
// This test drives many concurrent ParsePath calls against one real file
// path while a background writer atomically replaces the file's content
// (rename over the path, never a truncating in-place write, so a concurrent
// reader always sees one complete version), then asserts every returned
// payload is internally consistent: exactly one function bucket entry whose
// name carries the well-formed "HelloVersionN" shape. A torn read would
// surface here as a malformed or duplicated function bucket. In practice the
// exact PrimeSource/ClearSource interleaving window is narrow enough that
// this test does not reliably fail against the pre-fix sync.Map at
// reasonable iteration counts -- the shared-package tests above are the
// deterministic regression proof for this defect. This test's job is to
// prove the fixed cache holds up under heavy concurrent same-path load in the
// real Engine.ParsePath path (no panic, no -race violation, no functional
// corruption), which the shared-package unit tests alone cannot exercise.
// Run with -race.
func TestParsePathConcurrentSamePathDoesNotTearReads(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "service.go")

	// Each version's function name embeds the version number, so the parsed
	// function name is only derivable from the exact bytes the language
	// parser observed for that call.
	source := func(version int) string {
		return fmt.Sprintf(`package service

func HelloVersion%d() string {
	return "hello-%d"
}
`, version, version)
	}

	// Seed the file so the very first read always succeeds.
	writeTestFile(t, filePath, source(0))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	stop := make(chan struct{})
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		version := 1
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Rename over the target instead of truncating it in place
			// (writeTestFile's os.WriteFile) so a concurrent ParsePath call
			// always observes either the fully-old or fully-new file, never a
			// truncated/half-written one. A torn-truncated read is a test
			// harness artifact, not the sourceCache hazard under test, and
			// would otherwise produce indistinguishable false failures.
			atomicWriteTestFile(t, filePath, source(version))
			version++
		}
	}()

	const goroutines = 24
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			payload, parseErr := engine.ParsePath(repoRoot, filePath, false, Options{})
			if parseErr != nil {
				// A file rename between LookupByPath and the physical read
				// can transiently fail to parse (e.g. ENOENT); that is not
				// the hazard under test (torn reads within one successful
				// call), so only real torn-read mismatches fail the test.
				return
			}
			errs[index] = assertSinglePayloadVersionConsistent(payload)
		}(i)
	}
	wg.Wait()
	close(stop)
	writerWG.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
}

// atomicWriteTestFile replaces path's content by writing to a sibling temp
// file and renaming it over path. Unlike writeTestFile (os.WriteFile, which
// truncates path in place), this guarantees a concurrent reader observes
// either the complete old bytes or the complete new bytes, never a
// truncated partial write -- required for a background writer racing
// Engine.ParsePath in TestParsePathConcurrentSamePathDoesNotTearReads.
func atomicWriteTestFile(t *testing.T, path string, body string) {
	t.Helper()

	tmp := path + ".tmp"
	if err := osWriteFile(tmp, []byte(body)); err != nil {
		t.Fatalf("osWriteFile(%q) error = %v, want nil", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("os.Rename(%q, %q) error = %v, want nil", tmp, path, err)
	}
}

// assertSinglePayloadVersionConsistent checks one ParsePath payload for
// internal consistency: it must name exactly one function, and that
// function's name must carry the well-formed "HelloVersionN" shape emitted
// by source(). A torn read that mixed bytes from two different concurrent
// primers would surface here as a malformed or duplicated function bucket.
func assertSinglePayloadVersionConsistent(payload map[string]any) error {
	functions, _ := payload["functions"].([]map[string]any)
	if len(functions) != 1 {
		return fmt.Errorf("functions bucket = %#v, want exactly 1 entry (torn/duplicated read)", functions)
	}
	name, _ := functions[0]["name"].(string)
	if !strings.HasPrefix(name, "HelloVersion") {
		return fmt.Errorf("function name = %q, want HelloVersionN shape", name)
	}
	return nil
}

// TestParsePathReadsRawTextSourceExactlyOnce covers the raw_text language,
// which independently calls readSource inside parseRawText for its own
// artifact-type metadata before ParsePath's engine-level metadata inference
// ran a second read over the same bytes.
//
// Not t.Parallel(): see TestParsePathReadsSourceExactlyOnce.
func TestParsePathReadsRawTextSourceExactlyOnce(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "app.conf")
	writeTestFile(t, filePath, "listen 8080;\nserver_name example.com;\n")

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var reads int
	restore := shared.SetReadSourceHookForTest(func(path string) {
		if path == filePath {
			reads++
		}
	})
	defer restore()

	if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if reads != 1 {
		t.Fatalf("physical disk reads for %q = %d, want 1", filePath, reads)
	}
}

// TestParsePathReadsNuGetProjectSourceExactlyOnce covers nuget_project, the
// third engine-local readSource consumer alongside raw_text: parseNuGetProject
// calls readSource for the XML decode, then ParsePath's content-metadata
// inference used to read the same .csproj bytes a second time.
//
// Not t.Parallel(): see TestParsePathReadsSourceExactlyOnce.
func TestParsePathReadsNuGetProjectSourceExactlyOnce(t *testing.T) {
	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Worker.csproj")
	writeTestFile(t, filePath, `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
  </ItemGroup>
</Project>`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	var reads int
	restore := shared.SetReadSourceHookForTest(func(path string) {
		if path == filePath {
			reads++
		}
	})
	defer restore()

	if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if reads != 1 {
		t.Fatalf("physical disk reads for %q = %d, want 1", filePath, reads)
	}
}
