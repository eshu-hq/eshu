// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package haskell parses Haskell source evidence for the parent parser engine.
//
// Parse reads one Haskell source file and emits the parser payload buckets for
// modules, imports, data/newtype/type and class declarations, functions,
// where-block variables, and bounded function-call evidence. Every source symbol
// is extracted from the tree-sitter AST: module headers, export lists, imports
// with aliases, type-level declarations, typeclass and instance methods,
// top-level binds, and type signatures are keyed by parse-tree node spans rather
// than a line-by-line regex scan. Where-block local bindings stay in the
// variables bucket rather than becoming top-level functions. Parse annotates
// explicit module exports, main functions, typeclass methods, and instance
// methods with dead-code root metadata.
//
// Function-call rows remain bounded lexical evidence read from the right-hand
// side of each binding, the documented permanent exception this package keeps:
// they report observed call tokens, not compiler-resolved Haskell name binding.
//
// ParseWithParser and PreScanWithParser let the parent engine reuse a
// caller-owned runtime parser without importing parser dispatcher internals.
// PreScan returns declaration names from the same payload path.
//
// No-Regression Evidence: the AST migration replaces a per-line text scan
// (`strings.Split` plus eight compiled regexps over every line, layered with a
// partial tree-sitter augmentation pass) with a single recursive walk of the
// tree-sitter parse tree the parser already builds for the file. The walk visits
// each named declaration once, so work is bounded by AST node count rather than
// line count times pattern count. No allocation-per-line, goroutine, channel,
// lock, queue, or graph-write behavior is introduced; the package stays
// single-threaded under the caller-owned parser. Verified by `go test
// ./internal/parser/haskell -count=1` (byte-parity characterization goldens plus
// behavior tests) and `go test ./internal/parser/... ./internal/reducer
// ./internal/query -count=1`.
//
// No-Observability-Change: this package emits no metrics, spans, logs, or
// status. The change is parse-internal and adds none, so operator-facing signals
// are identical before and after.
package haskell
