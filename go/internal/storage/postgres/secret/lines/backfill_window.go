// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The batch statements and the window and lock reads of Finalize's per-file
// batches. backfill.go owns the batch loop and the retry policy.

const listReposSQL = `SELECT DISTINCT repo_id FROM content_files ORDER BY repo_id`

// windowBatchSQL reads, without locking, the next window of file keys of one
// repository after the keyset cursor $2: at most $3 keys, cut to the longest
// prefix whose content bytes stay within $4, but never fewer than one. It is a
// plain ordered index range scan, so its keys come back in order and its last
// key is a cursor that cannot jump: unlike a locking read, no concurrent update
// can change a row it returns. Every row also carries window_rows, the size of
// the LIMIT window before the byte cut, so the caller can tell the end of the
// repository (every row of a not-full window kept) from a cut (more remain).
// octet_length reads the stored datum header, not the detoasted value; the
// running-sum window scan over 500 rows measured 7 ms.
const windowBatchSQL = `
SELECT relative_path, window_rows
FROM (
    SELECT relative_path,
           count(*) OVER () AS window_rows,
           row_number() OVER (ORDER BY relative_path) AS position,
           sum(coalesce(octet_length(content), 0)) OVER (ORDER BY relative_path) AS running_bytes
    FROM (
        SELECT relative_path, content
        FROM content_files
        WHERE repo_id = $1 AND relative_path > $2
        ORDER BY relative_path
        LIMIT $3
    ) w
) sized
WHERE position = 1 OR running_bytes <= $4
ORDER BY relative_path`

// lockBatchSQL locks the window's rows FOR SHARE in primary-key order. FOR SHARE
// conflicts with a writer's row update, so the batch derives from content no
// writer can change until this transaction commits; a writer that commits first
// is seen through the READ COMMITTED recheck, and its own trigger has already
// produced the rows this batch re-derives to the same values. A row a writer
// deleted or moved to a key outside the window fails the recheck and drops out.
// The lock is taken by key, not by the cursor range, because a locking read
// that waits on a concurrently updated row returns the row's new version at its
// old position: with a range and LIMIT the keys arrive out of order and a
// cursor taken from the last one can skip files.
const lockBatchSQL = `
SELECT relative_path
FROM content_files
WHERE repo_id = $1 AND relative_path = ANY($2)
ORDER BY relative_path
FOR SHARE`

const deleteBatchSQL = `
DELETE FROM content_file_secret_lines
WHERE repo_id = $1 AND relative_path = ANY($2)`

const insertBatchSQL = `
INSERT INTO content_file_secret_lines (repo_id, relative_path, line_number, language, finding_kind, line_text)
SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, ''), d.finding_kind, d.line_text
FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d
WHERE f.repo_id = $1 AND f.relative_path = ANY($2)`

// scanWindow returns the next window of file keys after the cursor without
// locking them, cut to options.BatchSize keys and options.MaxBatchBytes bytes
// (always at least one key), and the size of the window before the byte cut.
func scanWindow(ctx context.Context, tx db.Transaction, repoID, cursor string, options Options) (keys []string, windowRows int, err error) {
	rows, err := tx.QueryContext(ctx, windowBatchSQL, repoID, cursor, options.BatchSize, options.MaxBatchBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("scan secret lines batch window: %w", err)
	}
	defer func() { _ = rows.Close() }()
	keys = make([]string, 0, options.BatchSize)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path, &windowRows); err != nil {
			return nil, 0, fmt.Errorf("scan secret lines batch key: %w", err)
		}
		keys = append(keys, path)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan secret lines batch window: %w", err)
	}
	return keys, windowRows, nil
}

// lockBatch locks the window's files FOR SHARE in primary-key order and returns
// the keys that are still present.
func lockBatch(ctx context.Context, tx db.Transaction, repoID string, window []string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, lockBatchSQL, repoID, array.Of(window))
	if err != nil {
		return nil, fmt.Errorf("lock secret lines batch: %w", err)
	}
	return scanKeys(rows, len(window), "lock secret lines batch")
}

func scanKeys(rows db.Rows, capacity int, action string) ([]string, error) {
	defer func() { _ = rows.Close() }()
	keys := make([]string, 0, capacity)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan secret lines batch key: %w", err)
		}
		keys = append(keys, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return keys, nil
}
