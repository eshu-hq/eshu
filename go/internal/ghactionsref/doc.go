// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ghactionsref is the single, dependency-free implementation of
// GitHub Actions `uses:` reference splitting and full-commit-SHA pin
// classification.
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
// This package is intentionally a leaf: it imports nothing from
// go/internal/*, so both the reducer/graph-projection path
// (go/internal/relationships, go/internal/reducer,
// go/internal/storage/cypher) and the query/read-model path
// (go/internal/query) depend on it without any risk of an import cycle. It
// replaces two independently maintained copies of the same @-index logic:
// relationships.parseGitHubRefParts and the query package's local `uses:`
// split helpers.
package ghactionsref
