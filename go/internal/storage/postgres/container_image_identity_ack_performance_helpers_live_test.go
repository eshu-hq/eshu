// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_ack

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func resetContainerImageIdentityAckPerfClaimed(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	owner string,
	now time.Time,
	ids []string,
) {
	t.Helper()
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+3)
	args = append(args, owner, now.Add(time.Minute), now)
	for index, id := range ids {
		placeholders[index] = fmt.Sprintf("$%d", index+4)
		args = append(args, id)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
UPDATE fact_work_items
SET status = 'claimed',
    attempt_count = 1,
    lease_owner = $1,
    claim_until = $2,
    visible_at = NULL,
    last_attempt_at = $3,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = $3
WHERE work_item_id IN (%s)
`, strings.Join(placeholders, ", ")), args...); err != nil {
		t.Fatalf("reset ACK perf claimed rows: %v", err)
	}
}

func resetContainerImageIdentityAckPerfPending(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	now time.Time,
	workItemID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    last_attempt_at = NULL,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = $1
WHERE work_item_id = $2
`, now, workItemID); err != nil {
		t.Fatalf("reset ACK perf pending row: %v", err)
	}
}

func resetContainerImageIdentityClaimLatchPerfPending(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	now time.Time,
	workItemID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending',
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required THEN 'pending'
        ELSE ''
    END,
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    next_attempt_at = NULL,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL,
    updated_at = $1
WHERE work_item_id = $2
`, now, workItemID); err != nil {
		t.Fatalf("reset claim-latch perf pending row: %v", err)
	}
}

func legacyContainerImageIdentityAckBatchQuery(
	now time.Time,
	owner string,
	ids []string,
) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, owner)
	for index, id := range ids {
		placeholders[index] = fmt.Sprintf("$%d", index+3)
		args = append(args, id)
	}
	return fmt.Sprintf(`
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id IN (%s)
  AND stage = 'reducer'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
`, strings.Join(placeholders, ", ")), args
}

func assertContainerImageIdentityAckScalarSingleBudget(
	t *testing.T,
	before containerImageIdentityAckPerfStats,
	after containerImageIdentityAckPerfStats,
	handlerBaseline time.Duration,
) {
	t.Helper()
	medianDelta := after.median - before.median
	p95Delta := after.p95 - before.p95
	if medianDelta > 25*time.Microsecond {
		t.Errorf("scalar single ACK median delta = %s, exceeds 25µs", medianDelta)
	}
	if p95Delta > 50*time.Microsecond {
		t.Errorf("scalar single ACK p95 delta = %s, exceeds 50µs", p95Delta)
	}
	contribution := math.Max(float64(medianDelta), 0) / float64(handlerBaseline)
	if contribution >= 0.001 {
		t.Errorf(
			"scalar single ACK handler contribution = %.4f%%, want <0.1%%",
			contribution*100,
		)
	}
}
