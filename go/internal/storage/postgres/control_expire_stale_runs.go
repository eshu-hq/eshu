// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// expireStaleOpenRunsLimit bounds one expiry pass like the retire skip: a
// stuck fleet drains over repeated maintenance ticks instead of one giant
// write, and a repeated call drains any residue.
const expireStaleOpenRunsLimit = 100

// ExpireStaleOpenRuns terminal-fails pending and claimed work items of
// non-terminal runs older than now-maxAge with the run_age_bound_exceeded
// class (eshu-hq/eshu#10791): a stuck-open run can no longer hold its
// target's open-target guard forever, so the next reconcile closes the run
// and the planner re-mints the target. Claimed rows have their claim row
// finished in the same statement, so an in-flight completion is fenced and
// exactly one of expiry/completion wins (see the statement contract in
// control_expire_stale_runs_sql.go).
//
// An empty result with a nil error means nothing was eligible, so a
// repeated call is a safe no-op.
func (s *WorkflowControlStore) ExpireStaleOpenRuns(ctx context.Context, now time.Time, maxAge time.Duration) ([]workflow.ExpiredStaleRunItem, error) {
	if s.database == nil {
		return nil, fmt.Errorf("workflow control store database is required")
	}
	if maxAge <= 0 {
		return nil, fmt.Errorf("max open run age must be positive")
	}
	cutoff := now.UTC().Add(-maxAge)
	message := fmt.Sprintf("open run exceeded max age %s", maxAge)
	rows, err := s.database.QueryContext(ctx, expireStaleOpenRunsQuery,
		cutoff.UTC(), expireStaleOpenRunsLimit, now.UTC(),
		string(workflow.FailureClassRunAgeBoundExceeded), message,
		string(workflow.FailureOperationRunAgeExpire))
	if err != nil {
		return nil, fmt.Errorf("expire stale open runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	expired := make([]workflow.ExpiredStaleRunItem, 0)
	for rows.Next() {
		var item workflow.ExpiredStaleRunItem
		if err := rows.Scan(&item.WorkItemID, &item.ScopeID, &item.RunID); err != nil {
			return nil, fmt.Errorf("expire stale open runs: %w", err)
		}
		expired = append(expired, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expire stale open runs: %w", err)
	}
	return expired, nil
}
