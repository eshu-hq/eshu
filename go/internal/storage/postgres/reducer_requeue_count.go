// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// scheduledReplayCount is the outcome of one CASE replay-debt schedule
// statement: whether any row was in the eligible set (matched), and how many
// of those rows actually transitioned (requeues). The statement's RETURNING
// clause reports one boolean per updated row — true when that row reopened
// from a finished state or took replay debt for the first time — so the
// caller counts transitions, never mere matches. Rows already pending or
// retrying, and rows already flagged, report false and keep their prior
// reason.
type scheduledReplayCount struct {
	matched  bool
	requeues int64
}

// countScheduledReplay scans the single-boolean RETURNING rows of a CASE
// replay-debt scheduler. what names the statement for error wrapping.
func countScheduledReplay(rows db.Rows, what string) (scheduledReplayCount, error) {
	defer func() { _ = rows.Close() }()
	var out scheduledReplayCount
	for rows.Next() {
		var requeued bool
		if err := rows.Scan(&requeued); err != nil {
			return scheduledReplayCount{}, fmt.Errorf("%s: scan schedule outcome: %w", what, err)
		}
		out.matched = true
		if requeued {
			out.requeues++
		}
	}
	if err := rows.Err(); err != nil {
		return scheduledReplayCount{}, fmt.Errorf("%s: iterate schedule outcome: %w", what, err)
	}
	return out, nil
}
