// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func seedContainerImageIdentityAckOrderingScenario(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	suffix string,
	now time.Time,
	seedWorkItem bool,
) (string, string, string, string) {
	t.Helper()
	scopeID := "repository:5854-ack-ordering-" + suffix
	generationID := "generation:5854-ack-ordering-" + suffix
	workItemID := "ack-5854-ordering-" + suffix
	owner := "legacy-reducer-5854-ordering-" + suffix
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
	if seedWorkItem {
		seedContainerImageIdentityAckWorkItem(
			t,
			ctx,
			db,
			workItemID,
			scopeID,
			generationID,
			owner,
			now.Add(time.Minute),
			now,
		)
	}
	return scopeID, generationID, workItemID, owner
}

func runContainerImageIdentityLegacyAckAsync(
	ctx context.Context,
	db *sql.DB,
	now time.Time,
	workItemID string,
	owner string,
) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := db.ExecContext(
			ctx,
			legacyContainerImageIdentityAckQuery,
			now,
			workItemID,
			owner,
		)
		done <- err
	}()
	return done
}

func runContainerImageIdentityMarkerAsync(
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id,
    activated_by_work_item_id,
    activated_by_claim_epoch
)
SELECT
    $1,
    $2,
    work_item_id,
    container_image_identity_claim_epoch
FROM fact_work_items
WHERE scope_id = $1
  AND generation_id = $2
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeID, generationID)
		done <- err
	}()
	return done
}

func runContainerImageIdentityMarkerForEpochAsync(
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	workItemID string,
	claimEpoch int64,
) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id,
    activated_by_work_item_id,
    activated_by_claim_epoch
)
VALUES ($1, $2, $3, $4)
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeID, generationID, workItemID, claimEpoch)
		done <- err
	}()
	return done
}

func assertContainerImageIdentityAckStillBlocked(
	t *testing.T,
	done <-chan error,
	release string,
) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("operation returned before %s: %v", release, err)
	case <-time.After(150 * time.Millisecond):
	}
}

func assertContainerImageIdentityOrderingLegacyAckRejected(
	t *testing.T,
	done <-chan error,
) {
	t.Helper()
	select {
	case err := <-done:
		assertContainerImageIdentityStatementLegacyRejected(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("legacy ACK did not return after marker transaction completed")
	}
}

func assertContainerImageIdentityOrderingOperationSucceeded(
	t *testing.T,
	done <-chan error,
	name string,
) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not complete", name)
	}
}

func assertContainerImageIdentityOrderingOperationRejected(
	t *testing.T,
	done <-chan error,
	name string,
	want string,
) {
	t.Helper()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s error = %v, want %q", name, err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not complete", name)
	}
}

func assertContainerImageIdentityStatementLegacyRejected(
	t *testing.T,
	err error,
) {
	t.Helper()
	if err == nil || !strings.Contains(
		err.Error(),
		"fact_work_items_container_image_identity_v2_status_check",
	) {
		t.Fatalf("legacy ACK error = %v, want attempt-token rejection", err)
	}
	var sqlState interface{ SQLState() string }
	if !errors.As(err, &sqlState) ||
		sqlState.SQLState() != "23514" {
		t.Fatalf("legacy ACK SQLSTATE = %v, want 23514", sqlState)
	}
}

func assertContainerImageIdentityAckOrderingMarkerCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM container_image_identity_cutovers
WHERE scope_id = $1
  AND generation_id = $2
`, scopeID, generationID).Scan(&got); err != nil {
		t.Fatalf("count ordering markers: %v", err)
	}
	if got != want {
		t.Fatalf(
			"ordering markers for %s/%s = %d, want %d",
			scopeID,
			generationID,
			got,
			want,
		)
	}
}
