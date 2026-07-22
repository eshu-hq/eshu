// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ghactionsref is the single, dependency-free implementation of
// GitHub Actions `uses:` reference splitting, edge-target slug detection, and
// full-commit-SHA pin classification.
//
// Parse splits a raw reference value (an action's or reusable workflow's
// `uses:` string, or a local reusable-workflow path) into its repository
// slug, in-repo path, and @ref components. Pinned reports whether an @ref
// value is a full-length commit SHA (40-hex today, 64-hex reserved for a
// future SHA-256 object id) -- the one property of a ref string that is
// statically provable without calling GitHub. A branch name and a tag are not
// classified beyond "not pinned": both are just ref strings in the workflow
// file, and a tag is mutable regardless, so asserting which one a ref is
// would be fabrication issue #5372 explicitly avoids.
//
// ReusableWorkflowRepo, ActionRepo, and LocalReusableWorkflowPath (issue
// #5526) are the shape-specific edge-target slug detectors built on top of
// that same split: which owner/repo (or in-repo path) a `uses:` value
// resolves to, for the three distinct reference shapes GitHub Actions
// supports -- a remote reusable workflow, a marketplace/third-party action,
// and a local reusable workflow, respectively.
//
// This package is intentionally a leaf: it imports nothing from
// go/internal/*, so both the reducer/graph-projection path
// (go/internal/relationships, go/internal/reducer,
// go/internal/storage/cypher) and the query/read-model path
// (go/internal/query) depend on it without any risk of an import cycle. It
// replaces independently maintained copies of the same @-index and
// edge-target slug-detection logic: relationships.parseGitHubRefParts, the
// query package's local `uses:` split helpers (issue #5372), and each
// package's own owner/repo@ref slug detectors for reusable workflows,
// actions, and local reusable workflows (issue #5526).
package ghactionsref
