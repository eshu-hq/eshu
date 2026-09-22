// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deadcode decides which JavaScript and TypeScript declarations are
// reachable entry points rather than dead code, and emits the evidence the
// parser payload carries as dead_code_root_kinds and
// dead_code_file_root_kinds.
//
// A declaration with no in-repository caller is not necessarily dead: it may be
// a package entry point, a framework route handler, a CommonJS or ES module
// export, a Hapi handler or plugin, a Next.js route export, a NestJS controller
// method, or part of a TypeScript package's declared public surface. Each of
// those is a root, and each is proven from local syntax plus repository layout
// rather than assumed. RootEvidence gathers them once per file; RootKinds
// answers per declaration.
//
// # Why this is a package
//
// It was not one before issue #6771. The ten dead_code_*.go files sat in a
// 17-file mutually recursive component with the route detectors and the
// semantics files: route registration decides roots, and root detection needs
// to know what registers routes. A go/types census measured 24 symbol edges in
// the outward direction. Most were AST primitives that moved to
// javascript/syntax on their own merits; the rest are the two seams below.
//
// # The two inverted seams
//
// SiblingSource and FrameworkEvidence are declared here, in the consumer, and
// implemented by the parent javascript package. That inversion is the whole
// reason this package can exist: the parent owns the tree-sitter parser pool,
// the sibling-file cache and the route-entry model, so a concrete dependency
// would point back at the parent and Go would reject the cycle. Depending on
// two small interfaces the parent satisfies keeps the compiled dependency
// one-way (javascript imports deadcode) while the call graph still runs both
// ways. Nothing about the parse seam moved, which #6062 requires stay at the
// root pending the LanguageProvider decision.
//
// Both seams tolerate a nil implementation: a nil SiblingSource answers "no
// sibling evidence" rather than panicking, and every caller treats that as
// absence of evidence, not as an error.
//
// This package may import javascript/syntax and javascript/project. It must
// never import the parent javascript package.
package deadcode
