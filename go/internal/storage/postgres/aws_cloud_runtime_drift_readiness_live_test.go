// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"testing"
	"time"
)

// TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive is the deterministic
// reproduction of #5837's actual root cause: the AWS EC2 scope drains its
// drift intent before the Terraform state_snapshot scope's generation
// activates. Rather than racing real collectors (the 1-in-3 shape the issue
// observed), this forces the exact evidence-state ordering directly against
// Postgres, turning it into a 100%-deterministic assertion the way the issue
// describes forcing collector order does for the golden-corpus gate.
//
// No state_snapshot scope at all -> not pending (a genuine orphan, nothing to
// wait for). A state_snapshot scope registered with a 'pending' generation ->
// pending (the race window). That generation activated -> not pending again
// (state caught up).
func TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)
	checker := PostgresAWSCloudRuntimeDriftReadinessChecker{DB: SQLDB{DB: sqlDB}}
	now := time.Now().UTC()

	suffix := fmt.Sprintf("5837-readiness-%d", time.Now().UnixNano())
	stateScopeID := "state_snapshot:s3:" + suffix
	pendingGenerationID := "gen-pending-" + suffix

	t.Run("registered but pending state_snapshot generation is pending", func(t *testing.T) {
		seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, stateScopeID, "terraform_state", "", now)
		seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, pendingGenerationID, stateScopeID, "pending", now)

		pending, err := checker.HasPendingStateSnapshotEvidence(ctx)
		if err != nil {
			t.Fatalf("HasPendingStateSnapshotEvidence() error = %v", err)
		}
		if !pending {
			t.Fatal("HasPendingStateSnapshotEvidence() = false, want true: a registered state_snapshot scope with a 'pending' generation is the exact #5837 race window")
		}
	})

	t.Run("activated state_snapshot generation is no longer pending", func(t *testing.T) {
		if _, err := sqlDB.ExecContext(
			ctx,
			`UPDATE scope_generations SET status = 'active', activated_at = $1 WHERE generation_id = $2`,
			now, pendingGenerationID,
		); err != nil {
			t.Fatalf("activate pending generation: %v", err)
		}
		if _, err := sqlDB.ExecContext(
			ctx,
			`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			pendingGenerationID, stateScopeID,
		); err != nil {
			t.Fatalf("set active_generation_id: %v", err)
		}

		pending, err := checker.HasPendingStateSnapshotEvidence(ctx)
		if err != nil {
			t.Fatalf("HasPendingStateSnapshotEvidence() error = %v", err)
		}
		if pending {
			t.Fatal("HasPendingStateSnapshotEvidence() = true after activation, want false: state caught up, nothing left to wait for")
		}
	})
}
