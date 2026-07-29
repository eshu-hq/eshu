// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

// hasPendingStateSnapshotGenerationQuery reports whether any state_snapshot:*
// ingestion scope has a generation still in status 'pending' -- registered and
// mid-ingestion, neither active (scope_generations_active_scope_idx) nor
// permanently failed (scope.go's GenerationStatusFailed). This is the
// readiness signal aws_cloud_runtime_drift's #5848 defer uses to tell "the
// Terraform state scope has not caught up yet" apart from "there genuinely is
// no Terraform state for this account" -- only the pending status proves
// ingestion is still in flight; an absent scope or a failed/active generation
// both mean nothing further is coming for that scope.
//
// This is coarse: it does not scope to the ONE state_snapshot locator that
// would resolve a specific ARN, because which backend owns a given ARN cannot
// be known until state activates and the ARN join resolves (see
// AWSCloudRuntimeDriftConfigResolver's same chicken-and-egg). A pending scope
// anywhere can only cause an unnecessary, bounded defer -- never an incorrect
// verdict, since the caller only holds back an orphaned_cloud_resource
// classification that a bound (go/internal/reducer/aws_cloud_runtime_drift_readiness.go's
// awsCloudRuntimeDriftStatePendingMaxWait, an ELAPSED-TIME bound since the
// intent was first enqueued, not a retry-count bound) eventually commits
// anyway.
const hasPendingStateSnapshotGenerationQuery = `
SELECT EXISTS (
    SELECT 1
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    WHERE scope.scope_id LIKE 'state_snapshot:%'
      AND generation.status = 'pending'
)
`

// PostgresAWSCloudRuntimeDriftReadinessChecker implements
// reducer.AWSCloudRuntimeDriftReadinessChecker over the shared fact store.
type PostgresAWSCloudRuntimeDriftReadinessChecker struct {
	DB Queryer
}

// HasPendingStateSnapshotEvidence reports whether any state_snapshot:*
// ingestion scope is still mid-ingestion.
func (c PostgresAWSCloudRuntimeDriftReadinessChecker) HasPendingStateSnapshotEvidence(
	ctx context.Context,
) (bool, error) {
	if c.DB == nil {
		return false, fmt.Errorf("aws cloud runtime drift readiness database is required")
	}
	rows, err := c.DB.QueryContext(ctx, hasPendingStateSnapshotGenerationQuery)
	if err != nil {
		return false, fmt.Errorf("check pending terraform state_snapshot generations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("iterate pending terraform state_snapshot generations: %w", err)
		}
		return false, nil
	}
	var pending bool
	if err := rows.Scan(&pending); err != nil {
		return false, fmt.Errorf("scan pending terraform state_snapshot generations: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate pending terraform state_snapshot generations: %w", err)
	}
	return pending, nil
}
