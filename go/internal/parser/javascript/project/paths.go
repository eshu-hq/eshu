// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package project

import (
	"path/filepath"
	"strings"
)

// RelativeSlashPath returns path expressed relative to repoRoot with
// forward-slash separators, and false when path is not repoRoot itself or
// beneath it (an out-of-repo path, or a filepath.Rel error).
func RelativeSlashPath(repoRoot string, path string) (string, bool) {
	relativePath, err := filepath.Rel(repoRoot, path)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(relativePath)), true
}

// CleanPath returns the absolute, cleaned form of path, or the empty string
// when path is blank or cannot be made absolute. Every project-scope lookup
// (tsconfig.json, package.json, cache keys) normalizes through this so the
// same on-disk location always produces the same comparison key.
func CleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

// PathWithin reports whether path is root itself or nested beneath it, after
// cleaning both through CleanPath. It bounds the upward directory walks used
// to find the nearest tsconfig.json/package.json so a symlink or relative
// escape can never resolve outside the repository being scanned.
func PathWithin(root string, path string) bool {
	root = CleanPath(root)
	path = CleanPath(path)
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
