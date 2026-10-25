// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared/toml/nesting"
)

// hostileKeyCount is the table size from #8991. go-toml v2.3.1 takes about
// 18 seconds to decode 100,000 keys in one table.
const hostileKeyCount = 100_000

// dependencyTable returns a [dependencies] table of n registry dependencies.
func dependencyTable(n int) string {
	var b strings.Builder
	b.WriteString("[dependencies]\n")
	for i := range n {
		fmt.Fprintf(&b, "dep%d = \"1\"\n", i)
	}
	return b.String()
}

// manifestWithKeys returns a Cargo.toml whose [dependencies] table holds n
// registry dependencies.
func manifestWithKeys(n int) string {
	return "[package]\nname = \"x\"\nversion = \"0.1.0\"\n\n" + dependencyTable(n)
}

// TestCargoManyKeysAreRejectedBeforeDecode is the #8991 regression. Each file
// piles 100,000 entries into one table by a different route; every one must
// fail with the typed error and allocate no more than reading the file does,
// which proves the quadratic decode never ran. The test asserts bytes, not
// time: an unguarded decode of these files allocates many times the file.
func TestCargoManyKeysAreRejectedBeforeDecode(t *testing.T) {
	var headers, lock strings.Builder
	for i := range hostileKeyCount {
		fmt.Fprintf(&headers, "[dependencies.dep%d]\nversion = \"1\"\n", i)
		fmt.Fprintf(&lock, "k%d = 1\n", i)
	}
	for name, tc := range map[string]struct{ file, body string }{
		"manifest table keys":    {"Cargo.toml", manifestWithKeys(hostileKeyCount)},
		"manifest child headers": {"Cargo.toml", headers.String()},
		"lockfile root keys":     {"Cargo.lock", "version = 3\n" + lock.String()},
	} {
		t.Run(name, func(t *testing.T) {
			repoRoot := t.TempDir()
			path := filepath.Join(repoRoot, tc.file)
			writeCargoFile(t, path, tc.body)

			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := ParseCargoDependencyFile(repoRoot, path, false)
			runtime.ReadMemStats(&after)

			if !errors.Is(err, nesting.ErrTooManyKeys) {
				t.Fatalf("ParseCargoDependencyFile() error = %v, want nesting.ErrTooManyKeys", err)
			}
			// Reading costs the file once; the header set adds a few hundred
			// KiB at most.
			ceiling := uint64(2*len(tc.body)) + 1<<20
			if got := after.TotalAlloc - before.TotalAlloc; got > ceiling {
				t.Fatalf("parse allocated %d bytes, want <= %d (file %d bytes)", got, ceiling, len(tc.body))
			}
		})
	}
}

// TestCargoWideLegalFilesStillParse keeps the limit clear of real files: a
// manifest with 2,000 dependencies (the corpus maximum is 45) and a lockfile
// with 20,000 packages.
func TestCargoWideLegalFilesStillParse(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	manifest := filepath.Join(repoRoot, "Cargo.toml")
	writeCargoFile(t, manifest, manifestWithKeys(2000))
	payload, err := ParseCargoDependencyFile(repoRoot, manifest, false)
	if err != nil {
		t.Fatalf("manifest error = %v, want nil", err)
	}
	if rows, _ := payload["variables"].([]map[string]any); len(rows) != 2000 {
		t.Fatalf("manifest rows = %d, want 2000", len(rows))
	}

	var lock strings.Builder
	lock.WriteString("version = 3\n")
	for i := range 20_000 {
		fmt.Fprintf(&lock, "\n[[package]]\nname = \"crate-%d\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\nchecksum = \"%064x\"\n", i, i)
	}
	lockPath := filepath.Join(repoRoot, "Cargo.lock")
	writeCargoFile(t, lockPath, lock.String())
	payload, err = ParseCargoDependencyFile(repoRoot, lockPath, false)
	if err != nil {
		t.Fatalf("lockfile error = %v, want nil", err)
	}
	if rows, _ := payload["variables"].([]map[string]any); len(rows) != 20_000 {
		t.Fatalf("lockfile rows = %d, want 20000", len(rows))
	}
}

// TestCargoKeyWorkLimitBoundary pins cargoMaxKeyWork. A [dependencies] table
// of n keys costs n squared plus one for its header, so the limit admits 5,000
// minus one keys and rejects 5,000.
func TestCargoKeyWorkLimitBoundary(t *testing.T) {
	t.Parallel()
	const widest = 4999
	if got := widest*widest + 1; got > cargoMaxKeyWork {
		t.Fatalf("%d keys cost %d, over cargoMaxKeyWork %d", widest, got, cargoMaxKeyWork)
	}
	if got := (widest+1)*(widest+1) + 1; got <= cargoMaxKeyWork {
		t.Fatalf("%d keys cost %d, under cargoMaxKeyWork %d", widest+1, got, cargoMaxKeyWork)
	}
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "Cargo.toml")
	writeCargoFile(t, path, dependencyTable(widest))
	if _, err := ParseCargoDependencyFile(repoRoot, path, false); err != nil {
		t.Fatalf("%d keys: error = %v, want nil", widest, err)
	}
	writeCargoFile(t, path, dependencyTable(widest+1))
	if _, err := ParseCargoDependencyFile(repoRoot, path, false); !errors.Is(err, nesting.ErrTooManyKeys) {
		t.Fatalf("%d keys: error = %v, want nesting.ErrTooManyKeys", widest+1, err)
	}
}
