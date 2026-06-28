// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/parserfixture"
)

// TestEmitterRelativeTreePreservesNestedPaths is the regression for the codex P2
// on #4109: when the caller passes a RELATIVE tree path, the emitter must still
// produce per-file provenance that keeps each file's directory. The parser
// stores an absolute file path in its payload; if the repo root stays relative,
// the Git collector seam cannot relativize the absolute path and falls back to
// filepath.Base, collapsing nested same-named files (pkga/dup.go and pkgb/dup.go)
// into one stable_fact_key. NewEmitter normalizes the tree to absolute, so the
// two files keep distinct keys. Without that normalization this test fails (both
// keys reduce to .../dup.go).
func TestEmitterRelativeTreePreservesNestedPaths(t *testing.T) {
	tmp := t.TempDir()
	writeGoFile(t, filepath.Join(tmp, "pkga", "dup.go"), "package pkga\n")
	writeGoFile(t, filepath.Join(tmp, "pkgb", "dup.go"), "package pkgb\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	relTree, err := filepath.Rel(cwd, tmp)
	if err != nil {
		t.Fatalf("rel(%q,%q): %v", cwd, tmp, err)
	}
	if filepath.IsAbs(relTree) {
		t.Fatalf("expected a relative tree path, got absolute %q", relTree)
	}

	emitter, err := parserfixture.NewEmitter(parserfixture.EmitterOptions{
		ScopeID:  "parser_fixture:nested",
		RepoID:   "nested",
		TreePath: relTree,
	})
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}

	envs := drainEnvelopes(t, emitter)
	if len(envs) != 2 {
		t.Fatalf("emitted %d envelopes, want 2 (one per nested dup.go)", len(envs))
	}
	keys := map[string]struct{}{}
	for _, env := range envs {
		keys[env.StableFactKey] = struct{}{}
	}
	if len(keys) != 2 {
		t.Fatalf("nested same-named files collapsed to %d distinct stable_fact_key(s), want 2: %v", len(keys), keys)
	}
	// The keys must retain the directory, not just the basename.
	var sawA, sawB bool
	for k := range keys {
		if strings.Contains(k, filepath.ToSlash(filepath.Join("pkga", "dup.go"))) {
			sawA = true
		}
		if strings.Contains(k, filepath.ToSlash(filepath.Join("pkgb", "dup.go"))) {
			sawB = true
		}
	}
	if !sawA || !sawB {
		t.Fatalf("stable_fact_keys lost their directories: %v", keys)
	}
}

func writeGoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
