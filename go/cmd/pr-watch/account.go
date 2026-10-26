// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// personalReposAccount is the GitHub login that owns repositories checked out
// under ~/personal-repos, which includes every eshu-hq/eshu worktree.
const personalReposAccount = "linuxdynasty"

// resolveAccount picks the GitHub login the watcher pins. An explicit flag
// wins. Otherwise the account comes from where the checkout lives: the shared
// gh keyring holds more than one login, so guessing from "whoever is active"
// is how a watcher ends up reading as the wrong user (a 404 that never
// matches). An unknown path is an error, not a default.
func resolveAccount(flagValue, dir, home string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if dir == "" || home == "" {
		return "", fmt.Errorf("cannot infer the GitHub account without a checkout directory and home; pass -account")
	}
	if under(filepath.Join(home, "personal-repos"), dir) {
		return personalReposAccount, nil
	}
	return "", fmt.Errorf("cannot infer the GitHub account for %q (only ~/personal-repos is known); pass -account", dir)
}

// under reports whether dir is root or a descendant of it. It compares the
// paths as given and again with symlinks resolved, because a macOS temp or
// home directory is often reached through a symlink.
func under(root, dir string) bool {
	if within(root, dir) {
		return true
	}
	realRoot, errRoot := filepath.EvalSymlinks(root)
	realDir, errDir := filepath.EvalSymlinks(dir)
	return errRoot == nil && errDir == nil && within(realRoot, realDir)
}

func within(root, dir string) bool {
	absRoot, errRoot := filepath.Abs(root)
	absDir, errDir := filepath.Abs(dir)
	if errRoot != nil || errDir != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
