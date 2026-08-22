// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitcodeowners emits ownership facts from a repository's CODEOWNERS
// file.
//
// It is the git-collector hook over the sibling collector/codeowners parser:
// that package parses the rules, this one resolves which of the several legal
// CODEOWNERS locations wins for a repository and streams the parsed rules.
package gitcodeowners
