// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
)

// FrameworkEvidence supplies the framework-route judgements that dead-code root
// detection consumes but does not own.
//
// Deciding that a call registers an Express, Fastify, Koa or NestJS route is
// route detection, not dead-code analysis: the parent javascript package owns
// it, and its route-entry model is shared with the payload buckets this package
// never touches. Dead-code detection only needs the answers.
//
// Like SiblingSource, this interface is declared in the consumer so the
// dependency points from the parent into this package rather than back out of
// it. Both directions existed before issue #6771 -- that mutual recursion is
// exactly why the ten dead_code_*.go files could not be a package -- and
// inverting these two seams is what broke it.
type FrameworkEvidence interface {
	// RegisteredRootKinds returns the dead-code root kinds implied by framework
	// route registrations found in root, keyed by lower-cased handler name.
	RegisteredRootKinds(
		root *tree_sitter.Node,
		source []byte,
		fastifyBases map[string]struct{},
		expressBases map[string]struct{},
		koaBases map[string]struct{},
	) map[string][]string

	// IsControllerMethod reports whether node is a NestJS controller method,
	// which makes it a dead-code root even with no in-repo caller.
	IsControllerMethod(node *tree_sitter.Node, source []byte, parents *syntax.ParentLookup) bool

	// ExpressSemantics returns the Express route semantics for a parsed file, or
	// ok false when the file declares no Express usage.
	ExpressSemantics(root *tree_sitter.Node, source []byte) (map[string]any, bool)
}
