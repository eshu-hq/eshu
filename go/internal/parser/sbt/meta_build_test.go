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
	payload, err := Parse(root, meta, false, shared.Options{})
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
	sub, err := Parse(root, other, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	requireStateField(t, stateOf(t, sub), "meta_build", false)
}

// TestMetaBuildIsJudgedFromTheRepositoryRoot pins the meta-build rule on the
// path relative to the repository root (#8997): a repository cloned into a
// directory named `project` has a root build.sbt that is not the meta-build,
// while `<root>/project/build.sbt` and a nested `project/build.sbt` are.
func TestMetaBuildIsJudgedFromTheRepositoryRoot(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		root, file string
		want       bool
	}{
		{"/clones/project", "/clones/project/build.sbt", false},
		{"/clones/project/", "/clones/project/build.sbt", false},
		{"/clones/project", "/clones/project/project/build.sbt", true},
		{"/clones/project", "/clones/project/a/project/build.sbt", true},
		{"/clones/project", "/clones/project/myproject/build.sbt", false},
		{"/src/repo", "/src/repo/build.sbt", false},
		{"/src/repo", "/src/repo/project/build.sbt", true},
		{`C:\clones\project`, `C:\clones\project\build.sbt`, false},
		{`C:\clones\project`, `C:\clones\project\project\build.sbt`, true},
		{"", "build.sbt", false},
		{"", "project/build.sbt", true},
		// A file outside the root, or no root, falls back to the full path.
		{"/elsewhere", "/clones/project/build.sbt", true},
		{"", "/clones/project/build.sbt", true},
	} {
		if got := isMetaBuildPath(tc.root, tc.file); got != tc.want {
			t.Errorf("isMetaBuildPath(%q, %q) = %v, want %v", tc.root, tc.file, got, tc.want)
		}
	}

	root := filepath.Join(t.TempDir(), "project")
	file := filepath.Join(root, "build.sbt")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(file, []byte("libraryDependencies += \"a\" % \"b\" % \"1\"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	payload, err := Parse(root, file, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	requireStateField(t, stateOf(t, payload), "meta_build", false)
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "resolved")
}
