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
// A nil SiblingSource means "no sibling evidence available" and is not an
// error. Callers inside this package must go through rootForFile rather than
// calling the method directly: the value replaced a concrete
// *javaScriptSiblingParser whose method guarded a nil receiver, and a nil
// typed pointer and a nil INTERFACE do not behave alike -- the first returns,
// the second panics. rootForFile restores the pre-#6771 behaviour exactly.
type SiblingSource interface {
	// RootForFile returns the root node and source bytes for path, or ok false
	// when the file cannot be read or parsed. Repeated calls for the same path
	// are expected to be cheap; the implementation owns any caching.
	RootForFile(path string) (root *tree_sitter.Node, source []byte, ok bool)
}

// rootForFile consults src, treating a nil SiblingSource as absence of
// evidence rather than as a programming error.
//
// This exists because the interface changed the nil semantics of the value it
// replaced. Before issue #6771 this was a *javaScriptSiblingParser and its
// method guarded `p == nil`, so a nil parser answered ok false; a method call
// on a nil interface panics instead. Production always supplies a real parser,
// but tests pass nil and the parent is entitled to, so the graceful answer is
// restored here rather than left to each call site to remember.
func rootForFile(src SiblingSource, path string) (root *tree_sitter.Node, source []byte, ok bool) {
	if src == nil {
		return nil, nil, false
	}
	return src.RootForFile(path)
}
