// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

// gitmodulesPath is the single repo-relative location git ever reads
// submodule declarations from. Unlike CODEOWNERS, which honors one of three
// possible locations, git only recognizes a ".gitmodules" file at the
// repository root.
const gitmodulesPath = ".gitmodules"

// IsGitmodulesPath reports whether relPath is exactly the repo-root
// ".gitmodules" file: a forward-slash, exact, case-sensitive match against
// ".gitmodules". Callers must pass an already-cleaned, forward-slash,
// repo-relative path.
func IsGitmodulesPath(relPath string) bool {
	return relPath == gitmodulesPath
}
