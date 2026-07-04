// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"path/filepath"
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
