// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import "github.com/jackc/pgx/v5"

// readinessSQL is the first statement of every search: it raises SQLSTATE 55000
// when the durable substring indexes are not ready, so the search never falls
// back to an unindexed scan.
const readinessSQL = `SELECT eshu_require_content_substring_indexes_ready()`

// The control statements run around each bounded statement. A statement that
// the server cancels aborts the transaction; ROLLBACK TO the savepoint makes
// it usable again and also reverts any SET LOCAL made inside the savepoint.
const (
	savepointSQL        = `SAVEPOINT unscoped_search`
	releaseSQL          = `RELEASE SAVEPOINT unscoped_search`
	rollbackToSQL       = `ROLLBACK TO SAVEPOINT unscoped_search`
	setTimeoutSQL       = `SELECT set_config('statement_timeout', $1, true)`
	disableIndexScanSQL = `SET LOCAL enable_indexscan = off`
	restoreIndexScanSQL = `RESET enable_indexscan`
	kindHit             = "hit"
	kindEdge            = "edge"
)

// execMode makes pgx plan each statement with its bound values (a custom
// plan) instead of caching a named prepared statement that can drift to a
// generic plan after five runs (#7359 precedent). pgx consumes it before SQL
// binds, so the placeholders stay $1..$n.
var execMode = pgx.QueryExecModeExec

// lowerReadinessSQL is the second statement of every file search: it raises
// SQLSTATE 55000 when the stored-lowercase trigram index is not ready, so the
// search never falls back to an unindexed scan (#7975).
const lowerReadinessSQL = `SELECT eshu_require_content_lower_trgm_index_ready()`

// countAfterSQL counts the documents no completed statement read: the rows
// strictly after the resume cursor (#7975). It touches only the primary key,
// never a TOASTed value, so it stays an index-only scan. A zero cursor passes
// (0, empty string) which precedes every row, so the count covers the whole table.
const countAfterSQL = `
SELECT count(*)::bigint
FROM content_files
WHERE (repo_id, relative_path) > ($1::text, $2::text)`

// countUnknown reports that the not-searched count itself could not be
// measured: the count statement was cancelled or failed, so the answer cannot
// say zero.
const countUnknown = int64(-1)

// StatementSet is the shipped text of the four bounded statements of the
// content_files shape (files.go), exported so the live plan-shape and
// differential proofs derive their statements from the constants production
// runs instead of copying them.
type StatementSet struct {
	// StepFirst and StepAfter are the key-ordered window statements for the
	// first window and for a window after a cursor.
	StepFirst, StepAfter string
	// TailFirst and TailAfter are the trigram tail statements.
	TailFirst, TailAfter string
	// DisableIndexScan is the SET LOCAL the tail runs under.
	DisableIndexScan string
	// CountAfter is the not-searched count statement for a partial page.
	CountAfter string
}

// Statements returns the shipped statement text of the content_files shape.
func Statements() StatementSet {
	return StatementSet{
		StepFirst:        stepFirstSQL,
		StepAfter:        stepAfterSQL,
		TailFirst:        tailFirstSQL,
		TailAfter:        tailAfterSQL,
		DisableIndexScan: disableIndexScanSQL,
		CountAfter:       countAfterSQL,
	}
}
