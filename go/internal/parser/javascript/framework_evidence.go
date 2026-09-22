// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/deadcode"
	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
)

// frameworkEvidence adapts this package's framework-route detection to the
// deadcode.FrameworkEvidence seam.
//
// Route detection and dead-code root detection call into each other: a route
// registration makes a handler a root, and root detection needs to know which
// calls register routes. Before issue #6771 both lived in one package and the
// mutual recursion was invisible; splitting dead-code analysis out made it an
// import cycle. deadcode declares the interface and this type satisfies it, so
// the compiled dependency runs one way (javascript -> deadcode) while the call
// graph still runs both.
//
// It carries no state: every method forwards to a package-level function, so a
// zero value is usable and there is nothing to initialize or close.
type frameworkEvidence struct{}

// RegisteredRootKinds forwards to this package's framework route-registration
// scan.
func (frameworkEvidence) RegisteredRootKinds(
	root *tree_sitter.Node,
	source []byte,
	fastifyBases map[string]struct{},
	expressBases map[string]struct{},
	koaBases map[string]struct{},
) map[string][]string {
	return javaScriptFrameworkRegisteredDeadCodeRootKinds(root, source, fastifyBases, expressBases, koaBases)
}

// IsControllerMethod forwards to this package's NestJS controller-method check.
func (frameworkEvidence) IsControllerMethod(
	node *tree_sitter.Node,
	source []byte,
	parents *syntax.ParentLookup,
) bool {
	return javaScriptIsNestJSControllerMethod(node, source, parents)
}

// ExpressSemantics forwards to this package's Express route semantics.
func (frameworkEvidence) ExpressSemantics(root *tree_sitter.Node, source []byte) (map[string]any, bool) {
	return detectExpressSemantics(root, source)
}

// compile-time assertion that the adapter still satisfies the seam.
var _ deadcode.FrameworkEvidence = frameworkEvidence{}
