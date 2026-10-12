// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// envKeyLiteral matches an environment-variable-shaped string literal. The git
// collector, discovery, the parsers, and SCIP read their settings through
// keys with these prefixes.
var envKeyLiteral = regexp.MustCompile(`"((?:ESHU|SCIP|GITHUB)_[A-Z0-9_]+)"`)

// envKeyRoots are the source trees whose settings can change what a snapshot
// emits: repository selection and snapshotting, file discovery, and every
// parser including SCIP. Paths are relative to this package directory.
var envKeyRoots = []string{"../..", "../../../../parser"}

// scanEnvKeys returns every env-shaped key literal in the non-test Go files
// under roots, plus any in the extra sources (for the seeded case).
func scanEnvKeys(t *testing.T, roots []string, extra ...string) []string {
	t.Helper()
	keys := map[string]bool{}
	add := func(source string) {
		for _, match := range envKeyLiteral.FindAllStringSubmatch(source, -1) {
			keys[match[1]] = true
		}
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			add(string(raw))
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
	for _, source := range extra {
		add(source)
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	return sorted
}

// unclassified returns the keys neither folded into the identity nor
// classified as not changing emitted facts.
func unclassified(keys []string) []string {
	var out []string
	for _, key := range keys {
		_, folded := FoldedEnvKeys[key]
		_, other := NonOutputEnvKeys[key]
		if !folded && !other {
			out = append(out, key)
		}
	}
	return out
}

// TestEveryEnvKeyIsClassified is the enumeration guard ruling a9-143 item 1
// requires: every env key read by discovery, parse, or SCIP code is either
// folded into the pre-parse identity or classified as not changing emitted
// facts, with a reason. A new key fails here until someone decides which.
func TestEveryEnvKeyIsClassified(t *testing.T) {
	t.Parallel()
	keys := scanEnvKeys(t, envKeyRoots)
	if len(keys) < 30 {
		t.Fatalf("scanned %d env keys; the roots are wrong (want the git collector, discovery, and parser trees)", len(keys))
	}
	if missing := unclassified(keys); len(missing) > 0 {
		t.Fatalf("env keys neither folded nor classified (add each to FoldedEnvKeys or NonOutputEnvKeys): %v", missing)
	}
	for key := range FoldedEnvKeys {
		if _, both := NonOutputEnvKeys[key]; both {
			t.Fatalf("%s is both folded and classified non-output", key)
		}
	}
}

// TestEnvKeyGuardCatchesASeededKey is the guard's seeded RED: a source that
// reads a new, unclassified key makes the guard report it.
func TestEnvKeyGuardCatchesASeededKey(t *testing.T) {
	t.Parallel()
	seeded := `package x; func f(getenv func(string) string) string { return getenv("ESHU_SEEDED_PARSER_KNOB") }`
	missing := unclassified(scanEnvKeys(t, envKeyRoots, seeded))
	if len(missing) != 1 || missing[0] != "ESHU_SEEDED_PARSER_KNOB" {
		t.Fatalf("unclassified = %v, want exactly the seeded key", missing)
	}
}
