// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
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

// filesystemCopyRegistry is the parser registry the managed-workspace copy
// consults for its kept dotfiles. The registry is immutable and fixed for
// the life of the process, so one shared value covers every walk.
var filesystemCopyRegistry = parser.DefaultRegistry()

// registryKeepsHiddenPath reports whether the parser registry admits the
// dot-prefixed entry at rel under the store content-store policy, so the
// managed-workspace copy keeps it (#8904). Probing Registry.LookupByPath
// keeps one source of truth: a dotfile name added to the registry is kept
// with no second edit, whether it binds by exact name (.editorconfig), by
// extension (.gitlab-ci.yml via .yml), or by a path rule, and a definition
// that binds metadata or redact (.npmrc, .yarnrc.yml, *.tfvars) is pruned
// because a governed definition wins every dispatch. .gitignore is excluded
// even though the registry admits it as plain text: the copy applies the
// source repository's ignore rules itself, and copying the file would let
// discovery filter a second time, dropping files the copy kept because git
// tracks them (#5591). Matching is case-insensitive, like the registry, and
// only the basename matters for the dot rule, so the copy preserves an
// admitted name wherever it appears under non-hidden or allowlisted
// directories. A name under any other, unallowlisted dot-directory, such as
// tools/.config/.eslintrc, is not copied: the walk prunes that dot-directory
// whole, so clone mode admits it and filesystem mode does not
// (TestFilesystemCopyPrunesAdmittedDotfilesUnderUnknownDotDirectories).
func registryKeepsHiddenPath(registry parser.Registry, rel string) bool {
	normalized := path.Clean(filepath.ToSlash(rel))
	if normalized == "." {
		return false
	}

	lowered := strings.ToLower(normalized)
	base := path.Base(lowered)
	if !strings.HasPrefix(base, ".") {
		return false
	}
	if base == ".gitignore" {
		return false
	}
	definition, ok := registry.LookupByPath(lowered)
	if !ok {
		return false
	}
	return definition.ContentStore.Policy == parser.ContentStoreStore
}

// preserveFilesystemHiddenPath reports whether the managed-workspace copy keeps
// the dot-prefixed entry at rel. Dotfiles the parser registry admits under
// the store policy are kept via registryKeepsHiddenPath, so .EditorConfig and
// .MVN reach discovery in filesystem mode exactly as they do in clone mode.
// The rest is structural, not registry-derived: the .github and .mvn
// directories are kept whole (.mvn/extensions.xml binds by path rule, and
// discovery applies the registry rules to what is inside each directory, as
// it does in clone mode), and .gitmodules is kept because the submodule
// facts read it from the copy even though no parser definition binds it. The
// walk calls this only for entries whose parent directories were kept, so
// none of this reaches inside an unknown dot-directory.
// TestPreserveFilesystemHiddenPathCoversRegistryDotfiles fails when the
// derived set drifts from what the copy keeps.
func preserveFilesystemHiddenPath(rel string) bool {
	normalized := path.Clean(filepath.ToSlash(rel))
	if normalized == "." {
		return false
	}

	lowered := strings.ToLower(normalized)
	base := path.Base(lowered)
	// .htaccess must reach the PHP parser even though its content-store body is withheld.
	if base == ".htaccess" {
		return true
	}
	if base == ".mvn" || base == ".gitmodules" {
		return true
	}

	if lowered == ".github" ||
		lowered == ".github/workflows" ||
		strings.HasPrefix(lowered, ".github/workflows/") {
		return true
	}
	return registryKeepsHiddenPath(filesystemCopyRegistry, lowered)
}
