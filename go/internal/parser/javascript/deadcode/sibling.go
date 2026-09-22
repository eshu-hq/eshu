// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// SiblingSource supplies the parsed AST of another file in the same repository.
// Several dead-code root checks have to look at a file other than the one being
// parsed -- a Hapi handler directory's index, a TypeScript barrel's re-export
// target, a package's declared public surface -- and this is the only capability
// they need to do it.
//
// It is declared here, in the consumer, rather than in the parent javascript
// package that implements it. That direction is what lets this package exist at
// all: the parent owns the tree-sitter parser pool and the sibling-file cache
// (issue #6062 keeps that seam at the root pending the LanguageProvider
// decision), so a concrete dependency would point from here back to the parent
// and Go would reject the import cycle. Depending on a one-method interface the
// parent satisfies inverts that edge without moving the parse seam (issue #6771).
//
// Implementations must tolerate being nil: a nil implementation, or one with no
// parser behind it, returns ok false rather than panicking, and every caller
// here treats a false result as "no sibling evidence available" rather than as
// an error. That matches the behaviour the parent's cache had before this
// interface existed.
type SiblingSource interface {
	// RootForFile returns the root node and source bytes for path, or ok false
	// when the file cannot be read or parsed. Repeated calls for the same path
	// are expected to be cheap; the implementation owns any caching.
	RootForFile(path string) (root *tree_sitter.Node, source []byte, ok bool)
}
