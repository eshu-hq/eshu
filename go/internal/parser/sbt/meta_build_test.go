// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestMetaBuildFileIsFlaggedAndPartial pins the meta-build rule. The registry
// admits build.sbt by base name, so project/build.sbt is parsed; it configures
// the build definition, so its dependencies are the build's classpath, not
// the project's. Its rows are partial and sbt_state.meta_build is true.
func TestMetaBuildFileIsFlaggedAndPartial(t *testing.T) {
	t.Parallel()

	body := []byte("libraryDependencies += \"a\" % \"b\" % \"1\"\n")
	root := t.TempDir()
	meta := filepath.Join(root, "project", "build.sbt")
	if err := os.MkdirAll(filepath.Dir(meta), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(meta, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	payload, err := Parse(meta, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
	state := stateOf(t, payload)
	requireStateField(t, state, "meta_build", true)
	requireStateField(t, state, "unresolved_declaration_count", 1)

	plain := parseSource(t, string(body))
	requireField(t, requireRow(t, plain, "a:b"), "dependency_resolution_state", "resolved")
	requireStateField(t, stateOf(t, plain), "meta_build", false)

	// A directory that only ends in "project" is not the meta-build.
	other := filepath.Join(root, "myproject", "build.sbt")
	if err := os.MkdirAll(filepath.Dir(other), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(other, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sub, err := Parse(other, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	requireStateField(t, stateOf(t, sub), "meta_build", false)
}
