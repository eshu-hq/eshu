// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestPreScanRepositoryPathsJSBoundsOversizedFile proves the fix for #4808: a
// JavaScript file over the 1 MiB parse-byte cap (#4766) must not be handed
// whole to tree-sitter during repository pre-scan either. Pre-scan runs
// across the FULL repository on every delta sync (unlike the normal parse
// stage, which only visits changed targets), so an over-cap file previously
// still paid the same superlinear tree-sitter cost there even after #4766
// bounded the normal parse stage. A bounded file must contribute no pre-scan
// names: the import-map result must have no name key pointing at its path.
func TestPreScanRepositoryPathsJSBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_bundle.js")
	writeTestFile(t, sourcePath, oversizedJSFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	assertPrescanPathAbsent(t, got, sourcePath)
}

// TestPreScanRepositoryPathsTSBoundsOversizedFile proves the pre-scan byte cap
// also covers TypeScript, which shares the javascript-family PreScan entry
// point.
func TestPreScanRepositoryPathsTSBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_bundle.ts")
	writeTestFile(t, sourcePath, oversizedJSFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	assertPrescanPathAbsent(t, got, sourcePath)
}

// TestPreScanRepositoryPathsTSXBoundsOversizedFile proves the pre-scan byte
// cap also covers TSX, which shares the javascript-family PreScan entry
// point.
func TestPreScanRepositoryPathsTSXBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_bundle.tsx")
	writeTestFile(t, sourcePath, oversizedJSFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	assertPrescanPathAbsent(t, got, sourcePath)
}

// TestPreScanRepositoryPathsJSSmallFileUnaffected proves 0/0 identity for the
// common case: a normal, under-cap JavaScript file must pre-scan exactly as
// before the byte cap was introduced.
func TestPreScanRepositoryPathsJSSmallFileUnaffected(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "small.js")
	writeTestFile(t, sourcePath, `
function greet(name) {
  return "hello " + name;
}

function farewell(name) {
  return "bye " + name;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "greet", sourcePath)
	assertPrescanContains(t, got, "farewell", sourcePath)
}

// TestPreScanRepositoryPathsPHPBoundsOversizedFile proves the fix for #4808 on
// PHP: a file over the 1 MiB cap must not be handed whole to tree-sitter
// during repository pre-scan.
func TestPreScanRepositoryPathsPHPBoundsOversizedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "big_map.php")
	writeTestFile(t, sourcePath, oversizedPHPFunctionArraySource(t))

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	assertPrescanPathAbsent(t, got, sourcePath)
}

// TestPreScanRepositoryPathsPHPSmallFileUnaffected proves 0/0 identity for
// the common case: a normal, under-cap PHP file pre-scans exactly as before
// the byte cap was introduced.
func TestPreScanRepositoryPathsPHPSmallFileUnaffected(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourcePath := filepath.Join(repoRoot, "small.php")
	writeTestFile(t, sourcePath, `<?php

function greet($name) {
    return "hello " . $name;
}

function farewell($name) {
    return "bye " . $name;
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.PreScanRepositoryPaths(repoRoot, []string{sourcePath})
	if err != nil {
		t.Fatalf("PreScanRepositoryPaths() error = %v, want nil", err)
	}

	assertPrescanContains(t, got, "greet", sourcePath)
	assertPrescanContains(t, got, "farewell", sourcePath)
}

// assertPrescanPathAbsent fails the test if any name in importsMap resolves
// to wantPath. Used to prove a bounded file contributed zero pre-scan names.
func assertPrescanPathAbsent(t *testing.T, importsMap map[string][]string, wantPath string) {
	t.Helper()

	for name, paths := range importsMap {
		for _, path := range paths {
			if path == wantPath {
				t.Fatalf("imports map[%q] unexpectedly contains bounded path %q, want no pre-scan names for an over-cap file", name, wantPath)
			}
		}
	}
}
