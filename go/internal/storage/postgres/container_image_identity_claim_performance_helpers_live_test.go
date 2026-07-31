//go:build perf5854_ack

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

func claimContainerImageIdentityPerformanceRow(
	ctx context.Context,
	queue ReducerQueue,
	query string,
	expectedID string,
) error {
	now := queue.now()
	rows, err := queue.db.QueryContext(
		ctx,
		query,
		now,
		queue.claimDomainFilters(),
		queue.LeaseOwner,
		now.Add(queue.LeaseDuration),
		queue.RequireProjectorDrainBeforeClaim,
		queue.ExpectedSourceLocalProjectors,
		queue.semanticEntityClaimLimit(),
	)
	if err != nil {
		return fmt.Errorf("claim performance reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("claim performance reducer work: %w", err)
		}
		return fmt.Errorf("claim performance reducer work: no row")
	}
	intent, err := scanReducerIntent(rows)
	if err != nil {
		return fmt.Errorf("scan performance reducer work: %w", err)
	}
	if intent.IntentID != expectedID {
		return fmt.Errorf(
			"performance claim intent ID = %q, want %q",
			intent.IntentID,
			expectedID,
		)
	}
	if rows.Next() {
		return fmt.Errorf("claim performance reducer work: multiple rows")
	}
	return rows.Err()
}
