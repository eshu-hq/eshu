// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package code groups the reducer's source-code analysis families: code-call
// materialization (call), the code-intelligence reachability projection
// (codeintel, still at the reducer root until its relocation lands), code
// taint and interprocedural evidence (taint), the value-flow fixpoint
// (value), shell-exec edge materialization (shell), and the function/
// namespace parenting durable value-flow function-summary persistence
// (function/summary). It is a documentation namespace and owns no runtime
// behavior; each child is its own Go package with its own contract (issue
// #6061, tree step 3 in docs/internal/design/reducer-target-tree.md).
package code
