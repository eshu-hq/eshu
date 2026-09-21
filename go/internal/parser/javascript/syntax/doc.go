// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package syntax provides AST-level extraction primitives for the
// JavaScript/TypeScript family: given a tree-sitter node and its source
// bytes, it returns the structural facts a caller needs — declared names,
// docstrings and function/method kinds (Docstring, FunctionKind,
// FunctionName), type parameters and type references
// (TypeParameterNames, AppendTypeReferenceCalls, TypeReferenceLeafName),
// implemented interfaces (ImplementedInterfaces), member-expression bases
// and properties (MemberBaseAndProperty, IsExpressRouteChain,
// ExpressHandlerNames, IdentifierName), parameter counts (ParameterCount),
// and a parent-pointer index for a parsed tree (ParentLookup,
// BuildParentLookup) that amortizes tree-sitter's per-call cgo cost of
// Node.Parent() (issue #3586).
//
// It answers "what does this node say", never "what does this framework
// mean" — recognizing an Express route, a NestJS controller, or a CommonJS
// export shape is the parent javascript package's job, built on these
// primitives. This package is a leaf, split out of the javascript package's
// root as part of the directory-size reduction in issue #6771
// (golangci-lint's 40-file dirgate cap): it must not import the parent
// javascript package.
//
// member_expression.go is the only file in this package with framework
// awareness baked in (the Express `app.route(path).get(...)` chain unwrap in
// MemberBaseAndProperty/IsExpressRouteChain/ExpressHandlerNames); everything
// else here is framework-agnostic AST shape extraction.
package syntax
