// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitsubmodule emits submodule facts from a repository's .gitmodules
// file and resolves each submodule's pinned gitlink SHA.
//
// It is the git-collector hook over the sibling collector/submodule parser: the
// parser understands .gitmodules, and this package decides when to run it and
// how the resulting facts reach the stream.
package gitsubmodule
