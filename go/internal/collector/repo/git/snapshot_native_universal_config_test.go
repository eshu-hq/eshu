// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestResolveNativeSnapshotFileSetAdmitsUniversalConfigNames proves discovery
// keeps the universal build and tool configuration names (#8814) at the repo
// root and nested, in any letter case, keeps a directory that shares a name out
// of the file set while still walking into it, and still drops the
// credential-bearing names.
func TestResolveNativeSnapshotFileSetAdmitsUniversalConfigNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		indexed bool
	}{
		{name: "makefile", path: "Makefile", indexed: true},
		{name: "nested makefile", path: "tools/lint/Makefile", indexed: true},
		{name: "lower-case makefile", path: "sub/makefile", indexed: true},
		{name: "gnumakefile", path: "GNUmakefile", indexed: true},
		{name: "justfile", path: "justfile", indexed: true},
		{name: "editorconfig", path: ".editorconfig", indexed: true},
		{name: "gitattributes", path: ".gitattributes", indexed: true},
		{name: "dockerignore", path: ".dockerignore", indexed: true},
		{name: "gitignore", path: ".gitignore", indexed: true},
		{name: "nested gitignore", path: "web/.gitignore", indexed: true},
		{name: "eslintrc", path: ".eslintrc", indexed: true},
		{name: "prettierrc", path: ".prettierrc", indexed: true},
		{name: "babelrc", path: ".babelrc", indexed: true},
		{name: "npmignore", path: ".npmignore", indexed: true},
		{name: "tox", path: "tox.ini", indexed: true},
		{name: "mypy", path: "mypy.ini", indexed: true},
		{name: "pytest", path: "pytest.ini", indexed: true},
		{name: "flake8", path: ".flake8", indexed: true},
		{name: "rustfmt", path: "rustfmt.toml", indexed: true},
		{name: "clippy", path: "clippy.toml", indexed: true},
		{name: "renovate json5", path: "renovate.json5", indexed: true},
		{name: "maven extensions", path: ".mvn/extensions.xml", indexed: true},
		{name: "nested maven extensions", path: "module/.mvn/extensions.xml", indexed: true},
		{name: "taskfile stays yaml", path: "Taskfile.yml", indexed: true},
		{name: "directory named like a config file, file inside", path: "Makefile.d/rules.cfg", indexed: true},
		{name: "env", path: ".env", indexed: false},
		{name: "npmrc", path: ".npmrc", indexed: false},
		{name: "settings xml", path: "settings.xml", indexed: false},
		{name: "maven settings", path: ".mvn/settings.xml", indexed: false},
		{name: "maven jvm config", path: ".mvn/jvm.config", indexed: false},
		{name: "maven config", path: ".mvn/maven.config", indexed: false},
		{name: "phpunit", path: "phpunit.xml", indexed: false},
		{name: "phpunit dist", path: "phpunit.xml.dist", indexed: false},
		{name: "generic ini", path: "other.ini", indexed: false},
		{name: "generic toml", path: "other.toml", indexed: false},
		{name: "generic xml", path: "other.xml", indexed: false},
		{name: "generic extensions xml", path: "src/extensions.xml", indexed: false},
	}

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	for _, tt := range tests {
		writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(tt.path)), "x = 1\n")
	}
	// A directory that carries a registered file name is never a file.
	writeCollectorTestFile(t, filepath.Join(repoRoot, "justfile.d", "note.cfg"), "x = 1\n")
	dirNamedLikeFile := filepath.Join(repoRoot, "dirs", "Makefile")
	if err := os.MkdirAll(dirNamedLikeFile, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", dirNamedLikeFile, err)
	}
	writeCollectorTestFile(t, filepath.Join(dirNamedLikeFile, "inner.cfg"), "x = 1\n")
	// A symlink named like an admitted file that points outside the repository
	// must stay out: discovery never reads through an external link.
	outside := filepath.Join(t.TempDir(), "outside-secret")
	writeCollectorTestFile(t, outside, "token = hunter2\n")
	if err := os.MkdirAll(filepath.Join(repoRoot, "linked"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, link := range []string{"linked/Makefile", "linked/.editorconfig"} {
		if err := os.Symlink(outside, filepath.Join(repoRoot, filepath.FromSlash(link))); err != nil {
			t.Fatalf("Symlink(%q) error = %v", link, err)
		}
	}

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	fileSet, _, err := resolveNativeSnapshotFileSet(
		resolvedRepoRoot, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions(),
	)
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}
	admitted := make(map[string]bool, len(fileSet.Files))
	for _, file := range fileSet.Files {
		rel, relErr := filepath.Rel(resolvedRepoRoot, file.Path)
		if relErr != nil {
			t.Fatalf("Rel(%q) error = %v", file.Path, relErr)
		}
		admitted[filepath.ToSlash(rel)] = true
	}

	for _, tt := range tests {
		if got := admitted[tt.path]; got != tt.indexed {
			t.Errorf("%s (%s): admitted = %t, want %t", tt.name, tt.path, got, tt.indexed)
		}
	}
	for _, directory := range []string{"dirs/Makefile", "justfile.d"} {
		if admitted[directory] {
			t.Errorf("directory %q was admitted as a file", directory)
		}
	}
	for _, inside := range []string{"dirs/Makefile/inner.cfg", "justfile.d/note.cfg"} {
		if !admitted[inside] {
			t.Errorf("file %q inside a directory named like a config file was not admitted", inside)
		}
	}
	for _, link := range []string{"linked/Makefile", "linked/.editorconfig"} {
		if admitted[link] {
			t.Errorf("external symlink %q was admitted", link)
		}
	}
}

// TestFilesystemSelectorPreservesUniversalConfigDotfiles proves the filesystem
// selector copies the admitted dot-prefixed names into the managed workspace.
// The copy drops dot-prefixed entries unless preserveFilesystemHiddenPath
// allows them, so a registry-admitted dotfile would otherwise never reach
// discovery in filesystem mode (#8814). Credential-bearing dotfiles, .gitignore
// and every .mvn file except extensions.xml stay out.
func TestFilesystemSelectorPreservesUniversalConfigDotfiles(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	kept := []string{
		".editorconfig", ".gitattributes", ".dockerignore",
		".eslintrc", ".prettierrc", ".babelrc", ".npmignore", ".flake8",
		filepath.Join("web", ".editorconfig"),
		filepath.Join(".mvn", "extensions.xml"),
		filepath.Join("module", ".mvn", "extensions.xml"),
	}
	// .gitignore stays uncopied: the copy applies the source ignore rules
	// itself, and a copied file would let discovery filter a second time. The
	// other .mvn files are unadmitted or can carry -D credential properties.
	dropped := []string{
		".env", ".npmrc", ".pypirc", ".netrc", ".secret-config", ".gitignore",
		filepath.Join(".mvn", "jvm.config"),
		filepath.Join(".mvn", "maven.config"),
		filepath.Join(".mvn", "wrapper", "maven-wrapper.properties"),
	}
	for _, rel := range append(append([]string{}, kept...), dropped...) {
		writeSelectionTestFile(t, filepath.Join(sourceRepo, rel), "x = 1\n")
	}

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}

	for _, rel := range kept {
		if _, err := os.Stat(filepath.Join(reposDir, "eshu-hq", "service-a", rel)); err != nil {
			t.Errorf("managed copy missing admitted config %q: %v", rel, err)
		}
	}
	for _, rel := range dropped {
		if _, err := os.Stat(filepath.Join(reposDir, "eshu-hq", "service-a", rel)); err == nil {
			t.Errorf("managed copy kept unadmitted dotfile %q", rel)
		}
	}
}
