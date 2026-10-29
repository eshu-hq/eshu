// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestPreserveFilesystemHiddenPathCoversRegistryDotfiles ties the managed
// workspace copy to the parser registry (#8904). The copy derives its kept
// dotfiles from the registry, so a dot-prefixed exact name added to the
// registry is kept with no second edit. The exemptions are .gitignore, which
// the copy must not duplicate because it applies the source ignore rules
// itself, and the names a metadata or redact content-store policy governs,
// which stay pruned. A change that moves a dotfile name across those lines
// fails here until the copy follows.
func TestPreserveFilesystemHiddenPathCoversRegistryDotfiles(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	checked := 0
	for _, definition := range registry.Definitions() {
		for _, name := range definition.ExactNames {
			if !strings.HasPrefix(name, ".") {
				continue
			}
			checked++
			rel := filepath.ToSlash(filepath.Join("nested", "dir", name))
			if strings.EqualFold(name, ".htaccess") {
				if !preserveFilesystemHiddenPath(name) || !preserveFilesystemHiddenPath(rel) {
					t.Errorf("metadata-only PHP config %q must reach the parser", name)
				}
				continue
			}
			if strings.EqualFold(name, ".gitignore") || definition.ContentStore.Policy != parser.ContentStoreStore {
				if preserveFilesystemHiddenPath(name) || preserveFilesystemHiddenPath(rel) {
					t.Errorf("registry exempts %q (%s) but the filesystem copy keeps it", name, definition.ParserKey)
				}
				continue
			}
			for _, keep := range []string{name, rel} {
				if !preserveFilesystemHiddenPath(keep) {
					t.Errorf("registry admits %q (%s) but the filesystem copy prunes it", keep, definition.ParserKey)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no dot-prefixed registry exact names found; the guard checked nothing")
	}
	for _, dispatch := range parser.PathDispatches() {
		for _, example := range dispatch.Examples {
			segments := strings.Split(filepath.ToSlash(example), "/")
			for i, segment := range segments {
				if !strings.HasPrefix(segment, ".") {
					continue
				}
				if rel := strings.Join(segments[:i+1], "/"); !preserveFilesystemHiddenPath(rel) {
					t.Errorf("path rule %v admits %q but the filesystem copy prunes %q", dispatch.Patterns, example, rel)
				}
			}
		}
	}
}

// TestPreserveFilesystemHiddenPathMatchesCaseInsensitively pins that the copy
// allowlist matches the registry, which lowercases every exact name and the
// path rules before comparing: .EditorConfig and .MVN are admitted in clone
// mode, so the filesystem copy must keep them too.
func TestPreserveFilesystemHiddenPathMatchesCaseInsensitively(t *testing.T) {
	t.Parallel()

	for _, rel := range []string{
		".EditorConfig", "web/.ESLINTRC", ".Flake8", ".MVN", "module/.Mvn",
		".GITHUB", ".Github", ".GITHUB/workflows", ".github/WORKFLOWS",
		".GITHUB/workflows/.eslintrc", ".Github/Workflows/ci.yml",
		"nested/.GitLab-CI.YAML",
	} {
		if !preserveFilesystemHiddenPath(rel) {
			t.Errorf("preserveFilesystemHiddenPath(%q) = false, want true", rel)
		}
	}
	for _, rel := range []string{
		".ENV", ".PypiRC", ".GitIgnore", ".GITHUBX", ".GITHUB/other", ".GITHUB/workflowsx",
		".NpmRC", ".Yarnrc.Yml", ".Auto.Tfvars",
	} {
		if preserveFilesystemHiddenPath(rel) {
			t.Errorf("preserveFilesystemHiddenPath(%q) = true, want false", rel)
		}
	}
}

// TestFilesystemCopyKeepsMvnDirectoryAndDiscoveryAppliesRegistryRules proves
// copy-then-discover end to end for .mvn. The copy keeps the whole .mvn
// directory, so filesystem mode treats .mvn exactly like clone mode: the
// registry decides. .mvn/extensions.xml is admitted as plain text, a Java file
// under .mvn/wrapper is admitted as Java like any other .java file, and
// .mvn/jvm.config, .mvn/maven.config and the wrapper properties have no
// registry entry, so discovery refuses them and they never reach the content
// store.
func TestFilesystemCopyKeepsMvnDirectoryAndDiscoveryAppliesRegistryRules(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	mvnFiles := map[string]bool{
		".mvn/extensions.xml":                       true,
		".mvn/wrapper/MavenWrapperDownloader.java":  true,
		".mvn/jvm.config":                           false,
		".mvn/maven.config":                         false,
		".mvn/wrapper/maven-wrapper.properties":     false,
		"module/.mvn/extensions.xml":                true,
		"module/.mvn/jvm.config":                    false,
		"module/.mvn/wrapper/maven-wrapper.jarinfo": false,
	}
	for rel := range mvnFiles {
		body := "x = 1\n"
		if strings.HasSuffix(rel, ".java") {
			body = "public class MavenWrapperDownloader { public static void main(String[] a) {} }\n"
		}
		writeSelectionTestFile(t, filepath.Join(sourceRepo, filepath.FromSlash(rel)), body)
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

	copied := filepath.Join(reposDir, "eshu-hq", "service-a")
	resolvedCopy, err := filepath.EvalSymlinks(copied)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", copied, err)
	}
	fileSet, _, err := resolveNativeSnapshotFileSet(
		resolvedCopy, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions(),
	)
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}
	admitted := map[string]bool{}
	for _, file := range fileSet.Files {
		rel, relErr := filepath.Rel(resolvedCopy, file.Path)
		if relErr != nil {
			t.Fatalf("Rel(%q) error = %v", file.Path, relErr)
		}
		admitted[filepath.ToSlash(rel)] = true
	}
	for rel, want := range mvnFiles {
		if got := admitted[rel]; got != want {
			t.Errorf("%s: admitted from the managed copy = %t, want %t", rel, got, want)
		}
	}
}

// TestFilesystemCopyPrunesAdmittedDotfilesUnderUnknownDotDirectories pins a
// known mode-parity difference (#8814). Clone mode admits an admitted dotfile
// at any depth because the registry matches by basename. The managed-workspace
// copy keeps one only when every directory above it is a non-hidden directory
// or an allowlisted one (.github, .github/workflows, .mvn, or a dot-directory
// whose name the registry admits): it prunes any other, unallowlisted dot-directory
// whole with filepath.SkipDir, so tools/.config/.eslintrc never reaches
// discovery in filesystem mode. The pruning of unknown dot-directories
// is intentional and long-standing; if it changes, update this test and the
// wording in the copy allowlist comment, git/AGENTS.md, the changelog and
// docs/public/languages/admitted-files.md.
func TestFilesystemCopyPrunesAdmittedDotfilesUnderUnknownDotDirectories(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	kept := []string{
		".eslintrc", filepath.Join("web", ".eslintrc"), filepath.Join("a", "b", ".eslintrc"),
		// Allowlisted dot-directories keep what is inside them.
		filepath.Join(".github", ".editorconfig"),
		filepath.Join(".github", "workflows", ".eslintrc"),
		// The allowlist matches names case-insensitively, like the registry.
		filepath.Join(".GITHUB", "workflows", ".eslintrc"),
		filepath.Join(".Github", "WORKFLOWS", ".editorconfig"),
		filepath.Join(".mvn", ".editorconfig"),
		filepath.Join(".mvn", "wrapper", ".flake8"),
		// A dot-directory named like an allowlisted file is itself kept.
		filepath.Join(".babelrc", "inner", ".prettierrc"),
	}
	pruned := []string{
		filepath.Join("tools", ".config", ".eslintrc"),
		filepath.Join(".config", ".eslintrc"),
		filepath.Join("web", ".cache", "x", ".editorconfig"),
	}
	for _, rel := range append(append([]string{}, kept...), pruned...) {
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

	copied := filepath.Join(reposDir, "eshu-hq", "service-a")
	for _, rel := range kept {
		if _, err := os.Stat(filepath.Join(copied, rel)); err != nil {
			t.Errorf("managed copy missing %q under non-hidden directories: %v", rel, err)
		}
	}
	for _, rel := range pruned {
		if _, err := os.Stat(filepath.Join(copied, rel)); err == nil {
			t.Errorf("managed copy kept %q under an unknown dot-directory; update the documented parity difference", rel)
		}
	}
}
