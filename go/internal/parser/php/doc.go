// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package php parses PHP source evidence for the parent parser engine using the
// tree-sitter PHP grammar.
//
// Parse reads one PHP source file through shared.ReadSource, parses it with the
// caller-provided tree-sitter parser, and emits the parser payload buckets for
// classes, traits, interfaces, functions, imports, variables, calls, and
// trait-use adaptations by walking AST nodes. A first pass collects
// declarations, import aliases, property and return types, bounded dead-code
// facts, and resolution-candidate node pointers for variables and calls;
// resolution runs in-memory against the collected type evidence. PreScan
// returns declaration names through a cheaper AST-only name pass whose output
// is kept aligned with Parse's declaration buckets by parent parser tests.
//
// Receiver and return-type inference resolves $this property chains, typed
// parameters, new expressions, self/static/parent scopes, static properties,
// and method and free-function return types so chained call receivers carry a
// concrete inferred_obj_type. The package emits exact Symfony Route attribute
// entries when a method attribute resolves to Symfony Route and carries a
// literal path and literal HTTP methods, plus bounded dead-code root hints for
// PHP entrypoints, constructors, known magic methods, same-file interface and
// trait methods, route-backed controller actions, literal route handlers,
// Symfony route attributes, and WordPress hook callbacks; broader autoload,
// reflection, and dynamic-dispatch behavior stays non-exact. The package is
// deterministic and depends only on shared parser helpers and the tree-sitter
// runtime.
package php
