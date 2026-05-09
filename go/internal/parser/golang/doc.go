// Package golang extracts Go parser evidence that can stay independent from
// the parent parser dispatch package.
//
// The package currently owns embedded SQL extraction from Go string literals.
// EmbeddedSQLQueries returns typed query rows for conservative database/sql and
// sqlx call sites, leaving payload map assembly and tree-sitter parsing to the
// parent parser package.
package golang
