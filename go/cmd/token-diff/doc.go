// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command token-diff reports whether two versions of a Go source file are
// behavior-identical after stripping plain `//` line comments -- the
// mechanism behind parser-relationship-kit's language-query-source
// comment-only exemption (issue #6647) and its internal import rename
// exemption (issue #6818).
//
// # Why not a line-based diff
//
// A line-based classifier that treats every changed `//`-prefixed line as
// "just a comment" is unsafe: `//go:build`, `//go:generate`, `//go:embed`,
// `//line`, and `// +build` are all `//` lines that change compiled
// behavior, and a raw-string line that happens to start with `//` (embedded
// Cypher, SQL, or any other query text) is data, not a comment, and a
// line-based check cannot tell the two apart. token-diff instead tokenizes
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
// # Internal import rename (opt-in)
//
// With -allow-internal-import-rename, a file also exits 0 when its only
// change is one repository-internal import substitution, the shape a package
// move leaves in each importer (issue #6818), for example query/queryspan
// becoming query/tracing:
//
//   - Exactly one import differs between base and head: one unaliased path
//     under github.com/eshu-hq/eshu/go/internal/ removed and one added. Every
//     other import (name and path) is unchanged. Import order and grouping
//     inside the block are not compared, so a gofumpt re-sort still passes.
//   - The import declarations hold nothing but import tokens and plain //
//     comments; a block comment or directive there refuses the rename.
//   - The qualifier names are the two paths' last elements. In the base
//     file, every use of the old name is a package qualifier (old.X, not a
//     selector field x.old and not a declared name), and the new name does
//     not already appear. Each old.X becomes new.X, and the resulting token
//     stream must equal head's, under the same comment and SEMICOLON rules
//     as above.
//
// Anything else is a real change: a rename plus any other code edit, a
// qualifier renamed inconsistently, a second renamed import, an aliased
// import, a path outside the internal tree, or a string literal edited to
// mention the new name. The check runs on tokens, so text inside strings is
// never rewritten.
//
// This mode only proves the file-level substitution. It cannot tell a real
// move from a swap to another package that already exists, so the caller
// must also prove the move from the tree (see "Caller contract"). On a
// rename verdict stdout is exactly one line, which the caller parses:
//
//	token-diff: internal import rename only: <old-path> -> <new-path> (qualifier <old>. -> <new>.)
//
// # Usage
//
//	token-diff [-allow-internal-import-rename] -base <path-to-base-version> -head <path-to-head-version>
//
// Exit 0: the streams are identical (safe to treat as a comment-only edit),
// or, with the flag, the file differs only by one internal import rename.
// Exit 1: the streams differ (a real change). Exit 2: an error occurred
// (bad flags, unreadable file, scan/parse failure). Callers MUST treat exit
// 2 the same as exit 1.
//
// # Caller contract
//
// token-diff only classifies one file's two versions; it does not resolve
// git refs, detect added/deleted/renamed paths, or decide the diff base.
// The caller (scripts/verify-parser-relationship-kit.sh, via
// scripts/lib/parser_relationship_comment_only_diff.sh) is responsible for:
// resolving the gate's own $base (with its three-dot-then-two-dot
// merge-base fallback), reading the base blob with `git show`, and treating
// an added, deleted, or renamed path as changed without ever invoking this
// tool -- there is no meaningful "base version" to compare in those cases.
// For a rename verdict the caller must also prove that the old import path's
// package directory had Go files at the base and none at head, and that the
// new one had none at the base and some at head; otherwise the verdict is a
// real change.
package main
