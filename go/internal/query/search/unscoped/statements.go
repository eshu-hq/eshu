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
	savepointSQL         = `SAVEPOINT unscoped_search`
	releaseSQL           = `RELEASE SAVEPOINT unscoped_search`
	rollbackToSQL        = `ROLLBACK TO SAVEPOINT unscoped_search`
	setTimeoutSQL        = `SELECT set_config('statement_timeout', $1, true)`
	disableIndexScanSQL  = `SET LOCAL enable_indexscan = off`
	restoreIndexScanSQL  = `RESET enable_indexscan`
	kindHit              = "hit"
	kindEdge             = "edge"
	columnsFromContent   = `repo_id, relative_path, coalesce(commit_sha, ''), '', content_hash, line_count, coalesce(language, ''), coalesce(artifact_type, '')`
	columnsFromContentIn = `repo_id, relative_path, commit_sha, content_hash, line_count, language, artifact_type, content`
)

// execMode makes pgx plan each statement with its bound values (a custom
// plan) instead of caching a named prepared statement that can drift to a
// generic plan after five runs (#7359 precedent). pgx consumes it before SQL
// binds, so the placeholders stay $1..$n.
var execMode = pgx.QueryExecModeExec

// A step reads one key-ordered window of rows and tests the pattern only on
// the rows it visits. The inner subquery fixes the shape: an ordered
// primary-key walk with LIMIT, never the trigram index, so the cost depends on
// the window and not on the planner's row estimate. The outer filter stops at
// the requested number of matches. The edge branch returns the key of the last
// row of the window; it is absent when the window held fewer rows than asked,
// which means the key space is exhausted.
//
// Arguments: $1 pattern, $2 window rows, $3 matches wanted, then for the
// "after" form $4 cursor repo_id and $5 cursor relative_path.
const stepFirstSQL = `
(SELECT 'hit' AS kind, ` + columnsFromContent + `
 FROM (SELECT ` + columnsFromContentIn + `
       FROM content_files
       ORDER BY repo_id, relative_path
       LIMIT $2::bigint) head
 WHERE content ILIKE '%' || $1 || '%'
 ORDER BY repo_id, relative_path
 LIMIT $3::bigint)
UNION ALL
(SELECT 'edge', repo_id, relative_path, '', '', '', 0, '', ''
 FROM content_files
 ORDER BY repo_id, relative_path
 OFFSET ($2::bigint - 1) LIMIT 1)`

const stepAfterSQL = `
(SELECT 'hit' AS kind, ` + columnsFromContent + `
 FROM (SELECT ` + columnsFromContentIn + `
       FROM content_files
       WHERE (repo_id, relative_path) > ($4::text, $5::text)
       ORDER BY repo_id, relative_path
       LIMIT $2::bigint) head
 WHERE content ILIKE '%' || $1 || '%'
 ORDER BY repo_id, relative_path
 LIMIT $3::bigint)
UNION ALL
(SELECT 'edge', repo_id, relative_path, '', '', '', 0, '', ''
 FROM content_files
 WHERE (repo_id, relative_path) > ($4::text, $5::text)
 ORDER BY repo_id, relative_path
 OFFSET ($2::bigint - 1) LIMIT 1)`

// The tail reads every remaining row through the trigram index. It runs with
// index scans disabled so the planner can only choose the trigram bitmap,
// whatever its row estimate says. Arguments: $1 pattern, $2 matches wanted,
// then for the "after" form $3 cursor repo_id and $4 cursor relative_path.
const tailFirstSQL = `
SELECT 'hit' AS kind, ` + columnsFromContent + `
FROM content_files
WHERE content ILIKE '%' || $1 || '%'
ORDER BY repo_id, relative_path
LIMIT $2::bigint`

const tailAfterSQL = `
SELECT 'hit' AS kind, ` + columnsFromContent + `
FROM content_files
WHERE (repo_id, relative_path) > ($3::text, $4::text)
  AND content ILIKE '%' || $1 || '%'
ORDER BY repo_id, relative_path
LIMIT $2::bigint`

// StatementSet is the shipped text of the four bounded statements, exported so
// the live plan-shape and differential proofs derive their statements from the
// constants production runs instead of copying them.
type StatementSet struct {
	// StepFirst and StepAfter are the key-ordered window statements for the
	// first window and for a window after a cursor.
	StepFirst, StepAfter string
	// TailFirst and TailAfter are the trigram tail statements.
	TailFirst, TailAfter string
	// DisableIndexScan is the SET LOCAL the tail runs under.
	DisableIndexScan string
}

// Statements returns the shipped statement text.
func Statements() StatementSet {
	return StatementSet{
		StepFirst:        stepFirstSQL,
		StepAfter:        stepAfterSQL,
		TailFirst:        tailFirstSQL,
		TailAfter:        tailAfterSQL,
		DisableIndexScan: disableIndexScanSQL,
	}
}
