// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command tokendiff reports whether two versions of a Go source file are
// behavior-identical after stripping plain `//` line comments -- the
// mechanism behind parser-relationship-kit's language-query-source
// comment-only exemption (issue #6647).
//
// # Why not a line-based diff
//
// A line-based classifier that treats every changed `//`-prefixed line as
// "just a comment" is unsafe: `//go:build`, `//go:generate`, `//go:embed`,
// `//line`, and `// +build` are all `//` lines that change compiled
// behavior, and a raw-string line that happens to start with `//` (embedded
// Cypher, SQL, or any other query text) is data, not a comment, and a
// line-based check cannot tell the two apart. tokendiff instead tokenizes
// both versions with go/scanner and compares the resulting token streams, so
// it inherits Go's own lexical rules for what is and is not a comment.
//
// # Rule
//
// Base and head are exempt (identical behavior) only when their filtered
// token streams are identical:
//
//   - Plain `//` line comments are dropped from both streams before
//     comparing -- they cannot change compiled behavior.
//   - Block comments (`/* ... */`) and directive comments (`//go:...`,
//     `//line ...`, `// +build ...`) are KEPT in the stream, so an edit to
//     any of those still counts as a real change.
//   - Automatically-inserted SEMICOLON tokens (go/scanner's end-of-line
//     semicolon insertion) are KEPT and compared BY KIND ONLY (not by
//     literal): dropping them would make a newline that moves a statement
//     boundary invisible, but a real semicolon and an auto-inserted one are
//     both real statement boundaries and must not be told apart.
//   - A file containing `import "C"` is NEVER exempt: its preamble comment
//     compiles as C source, not a Go comment.
//   - Any read, parse, or scan error on either side fails closed: report a
//     real change (exit 1 or 2), never exit 0.
//
// # Usage
//
//	tokendiff -base <path-to-base-version> -head <path-to-head-version>
//
// Exit 0: the streams are identical (safe to treat as a comment-only edit).
// Exit 1: the streams differ (a real change). Exit 2: an error occurred
// (bad flags, unreadable file, scan/parse failure). Callers MUST treat exit
// 2 the same as exit 1.
//
// # Caller contract
//
// tokendiff only classifies one file's two versions; it does not resolve
// git refs, detect added/deleted/renamed paths, or decide the diff base.
// The caller (scripts/verify-parser-relationship-kit.sh, via
// scripts/lib/parser_relationship_comment_only_diff.sh) is responsible for:
// resolving the gate's own $base (with its three-dot-then-two-dot
// merge-base fallback), reading the base blob with `git show`, and treating
// an added, deleted, or renamed path as changed without ever invoking this
// tool -- there is no meaningful "base version" to compare in those cases.
package main
