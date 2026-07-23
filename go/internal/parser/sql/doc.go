// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sql extracts SQL parser payloads without depending on the parent
// parser dispatch package.
//
// Parse reads one SQL source file and emits the payload buckets consumed by the
// parent parser and content materializer: schema objects, columns, routines,
// triggers, indexes, relationships, and migration metadata. All symbol
// extraction walks a tree-sitter SQL abstract syntax tree; the package holds no
// SQL DDL regular expressions. The source is segmented into statement-sized
// fragments so a single malformed statement cannot lose its neighbours, and
// CREATE PROCEDURE is recovered by a bounded rewrite to CREATE FUNCTION before
// parsing. The package keeps detection deterministic by sorting emitted buckets
// and by deduplicating entity and relationship keys before returning.
// Constraint clauses are scanned for bounded table references without being
// reported as column definitions.
// Foreign-key targets and routine read/write targets are also stamped into
// bounded entity metadata so downstream reducers can materialize the parser's
// relationship truth without reparsing SQL.
// Migration target metadata preserves distinct operations for the same target
// while retaining only the first source occurrence of an identical operation.
//
// A statement segment larger than maxSQLSegmentBytes is bounded before it
// reaches tree-sitter: an opaque dollar-quoted routine body of that size
// parses superlinearly and can hard-crash the process. The segment's
// dollar-quoted bodies are elided first (preserving the routine signature);
// if it is still oversized, the tree-sitter parse is skipped for that segment
// entirely. Either bound is recorded in payload["sql_parse_bounded"] and
// logged, never silently dropped.
package sql
