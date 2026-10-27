// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
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
	var headers, bomb, unknownKeys strings.Builder
	for i := range hostileKeyCount {
		fmt.Fprintf(&headers, "[dependencies.dep%d]\nversion = \"1\"\n", i)
	}
	// The decoder tracker-checks every key inside [[package]], known field
	// or not, so unknown plain keys pile up quadratically there (the P0 from
	// the PR #9270 review). The inline case is the same pile inside one
	// entry's inline table, which the decoder always walks.
	// No trailing comma: go-toml rejects `,}` without any key lookup, which
	// would make the document a vacuous threat.
	bomb.WriteString("version = 3\n[[package]]\nname = \"a\"\ndeps = {")
	for i := range hostileKeyCount {
		if i > 0 {
			bomb.WriteString(", ")
		}
		fmt.Fprintf(&bomb, "k%d = 1", i)
	}
	bomb.WriteString("}\n")
	unknownKeys.WriteString("version = 3\n[[package]]\nname = \"a\"\n")
	for i := range hostileKeyCount {
		fmt.Fprintf(&unknownKeys, "x%d = 1\n", i)
	}
	for name, tc := range map[string]struct{ file, body string }{
		"manifest table keys":    {"Cargo.toml", manifestWithKeys(hostileKeyCount)},
		"manifest child headers": {"Cargo.toml", headers.String()},
		"lockfile inline bomb":   {"Cargo.lock", bomb.String()},
		"lockfile unknown keys":  {"Cargo.lock", unknownKeys.String()},
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

// TestCargoLockKnownTablesCoversTarget keeps cargoLockKnownTables over
// every top-level table of the lock target. A table field missing from the
// set would be skipped by the guard but tracker-checked by the decoder,
// which reopens the quadratic hole, so the set must move with the struct.
func TestCargoLockKnownTablesCoversTarget(t *testing.T) {
	t.Parallel()
	var checkFields func(rt reflect.Type)
	checkFields = func(rt reflect.Type) {
		for i := range rt.NumField() {
			field := rt.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
			if name == "-" {
				continue
			}
			// An untagged embedded struct promotes its fields to the root,
			// so its tables are root tables too.
			ft := field.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if field.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				checkFields(ft)
				continue
			}
			// Only table-valued fields can be named by a table header; a
			// scalar root field is a key, and root keys are always charged.
			switch field.Type.Kind() {
			case reflect.Slice, reflect.Array, reflect.Struct, reflect.Map, reflect.Pointer, reflect.Interface:
			default:
				continue
			}
			if name == "" {
				name = field.Name
			}
			// The guard matches case-insensitively, as go-toml does.
			found := false
			for known := range cargoLockKnownTables {
				if strings.EqualFold(name, known) {
					found = true
				}
			}
			if !found {
				t.Errorf("cargoLockKnownTables is missing %q (field %s): the guard would skip a table the decoder tracks", name, field.Name)
			}
		}
	}
	checkFields(reflect.TypeOf(cargoLockDocument{}))
}

// TestCargoV1LockfileWithManyPackagesStillParses is the P2 from the #8991
// review. A v1 Cargo.lock holds one checksum key per package under
// [metadata], and the lock target has no metadata field, so the struct
// decode never enters that table and its keys are never tracker-checked.
// Map-shaped bounds count all 50,000 keys (over a billion work) and reject
// the legal file; the lock bounds must skip them the way the decoder does.
func TestCargoV1LockfileWithManyPackagesStillParses(t *testing.T) {
	t.Parallel()
	const packages = 50_000
	var lock strings.Builder
	for i := range packages {
		fmt.Fprintf(&lock, "[[package]]\nname = \"crate-%d\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\n\n", i)
	}
	lock.WriteString("[metadata]\n")
	for i := range packages {
		fmt.Fprintf(&lock, "\"checksum crate-%d 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)\" = \"%064x\"\n", i, i)
	}
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "Cargo.lock")
	writeCargoFile(t, path, lock.String())
	payload, err := ParseCargoDependencyFile(repoRoot, path, false)
	if err != nil {
		t.Fatalf("lockfile error = %v, want nil", err)
	}
	if rows, _ := payload["variables"].([]map[string]any); len(rows) != packages {
		t.Fatalf("lockfile rows = %d, want %d", len(rows), packages)
	}
}

// TestCargoKeyWorkLimitBoundary pins cargoMaxKeyWork. A [dependencies] table
// of n keys costs about half n squared: the header costs 2 and key i scans
// the i entries so far plus the one header, for n(n+1)/2 + n + 2. The limit
// admits 7,069 keys and rejects 7,070.
func TestCargoKeyWorkLimitBoundary(t *testing.T) {
	t.Parallel()
	const widest = 7069
	cost := func(n int) int { return n*(n+1)/2 + n + 2 }
	if got := cost(widest); got > cargoMaxKeyWork {
		t.Fatalf("%d keys cost %d, over cargoMaxKeyWork %d", widest, got, cargoMaxKeyWork)
	}
	if got := cost(widest + 1); got <= cargoMaxKeyWork {
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
