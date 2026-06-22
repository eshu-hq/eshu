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
package sql
