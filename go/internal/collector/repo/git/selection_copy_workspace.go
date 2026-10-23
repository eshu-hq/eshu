// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func cleanManagedWorkspace(reposDir string) error {
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read managed workspace %q: %w", reposDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".eshu-") || name == ".eshuignore" {
			continue
		}
		target := filepath.Join(reposDir, name)
		if entry.IsDir() {
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("remove managed directory %q: %w", target, err)
			}
			continue
		}
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("remove managed file %q: %w", target, err)
		}
	}
	return nil
}

// preservedHiddenFilesystemFileNames lists hidden (dot-prefixed) file basenames
// that the managed-workspace copy must keep even though they would otherwise be
// pruned as dotfiles. Unlike .github/workflows (a directory prefix), these are
// individual hidden config FILES that can live at the repo root or, for GitLab
// CI, in a subdirectory referenced via include:. They are matched by basename so
// the copy preserves them wherever they appear in the tree.
//
// The tool configuration names (#8814) are the dot-prefixed names the parser
// registry admits as plain text. .gitignore is deliberately absent: the copy
// applies the source repository's ignore rules itself, and copying the file
// would let discovery filter a second time, dropping files the copy kept
// because git tracks them (#5591). Credential-bearing dotfiles (.env, .npmrc,
// .pypirc, .netrc) stay pruned.
var preservedHiddenFilesystemFileNames = map[string]struct{}{
	".gitlab-ci.yml":  {},
	".gitlab-ci.yaml": {},
	".gitmodules":     {},
	".editorconfig":   {},
	".gitattributes":  {},
	".dockerignore":   {},
	".eslintrc":       {},
	".prettierrc":     {},
	".babelrc":        {},
	".npmignore":      {},
	".flake8":         {},
}

// preserveFilesystemHiddenPath reports whether the managed-workspace copy keeps
// the dot-prefixed entry at rel. The .mvn directory is kept so its
// extensions.xml reaches discovery; skipFilesystemMavenEntry then prunes every
// other .mvn entry.
func preserveFilesystemHiddenPath(rel string) bool {
	normalized := path.Clean(filepath.ToSlash(rel))
	if normalized == "." {
		return false
	}

	if _, ok := preservedHiddenFilesystemFileNames[path.Base(normalized)]; ok {
		return true
	}
	if path.Base(normalized) == ".mvn" {
		return true
	}

	return normalized == ".github" ||
		normalized == ".github/workflows" ||
		strings.HasPrefix(normalized, ".github/workflows/")
}

// skipFilesystemMavenEntry reports whether rel is a descendant of a .mvn
// directory other than .mvn/extensions.xml. Discovery admits only that one
// .mvn file; the rest (wrapper jars, jvm.config, maven.config) either are
// unadmitted or can carry credential properties, so the copy never takes them.
func skipFilesystemMavenEntry(rel string) bool {
	segments := strings.Split(path.Clean(filepath.ToSlash(rel)), "/")
	for i, segment := range segments {
		if segment != ".mvn" || i == len(segments)-1 {
			continue
		}
		return i != len(segments)-2 || segments[i+1] != "extensions.xml"
	}
	return false
}
