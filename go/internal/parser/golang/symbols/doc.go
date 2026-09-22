// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package symbols resolves tree-sitter Go syntax nodes into the receiver
// types, variable types, struct field types, interface targets, import
// aliases, and lexical-scope bindings every other internal/parser/golang leaf
// needs, with no knowledge of fact payloads, dead-code roots, control-flow
// lowering, or package pre-scan.
//
// symbols is the bottom layer of the split golang package and MUST NOT import
// golang, golang/dataflow, golang/deadcode, or golang/prescan: a package-level
// symbol census over the pre-split package showed those packages import each
// other in a cycle unless the shared tree-sitter-node vocabulary lives in one
// leaf every sibling can depend on (see #6774). It imports only
// internal/parser/shared and tree-sitter.
//
// ParentLookup, VariableTypeIndex, and ImportedVariableTypeIndex each amortize
// a full-tree pass so callers resolving many nodes in one file build the
// index once instead of re-walking the tree, or re-entering tree-sitter's
// O(depth) Node.Parent(), once per query node (#161). Every resolution
// function here is pure and deterministic given the same parsed tree, source
// bytes, and caller-supplied context (import aliases, struct/interface name
// sets, constructor-return types); scoped queries go through one of the
// *Index types so a result reflects the innermost lexical scope at that node,
// not the whole-file union.
package symbols
