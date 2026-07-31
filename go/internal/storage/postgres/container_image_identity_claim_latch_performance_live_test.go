// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_ack

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

var preLatchContainerImageIdentityClaimQuery = strings.NewReplacer(
	`        container_image_identity_v2_required =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN TRUE
                ELSE work.container_image_identity_v2_required
            END,
`,
	"",
	`        container_image_identity_v2_authorized_status =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN 'claimed'
                ELSE work.container_image_identity_v2_authorized_status
            END,
`,
	"",
).Replace(claimReducerWorkQuery)

var preLatchContainerImageIdentityClaimBatchQuery = strings.NewReplacer(
	`        container_image_identity_v2_required =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN TRUE
                ELSE work.container_image_identity_v2_required
            END,
`,
	"",
	`        container_image_identity_v2_authorized_status =
            CASE
                WHEN work.domain = 'container_image_identity'
                    THEN 'claimed'
                ELSE work.container_image_identity_v2_authorized_status
            END,
`,
	"",
).Replace(claimReducerWorkBatchQuery)

func TestContainerImageIdentityClaimLatchPerformanceLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 31, 14, 0, 0, 0, time.UTC)
	const (
		trials           = 6
		warmups          = 100
		pairedIterations = 1000
	)
	beforeTrials := make([]containerImageIdentityAckPerfStats, 0, trials)
	afterTrials := make([]containerImageIdentityAckPerfStats, 0, trials)

	for trial := range trials {
		baselineDB, candidateDB := openContainerImageIdentityClaimPerfPair(t, trial)
		pinContainerImageIdentityAckTheoryDB(baselineDB)
		pinContainerImageIdentityAckTheoryDB(candidateDB)
		scopeID := fmt.Sprintf("repository:5854-claim-latch-%d", trial)
		generationID := fmt.Sprintf("generation:5854-claim-latch-%d", trial)
		workItemID := fmt.Sprintf("claim-5854-latch-%d", trial)
		owner := fmt.Sprintf("reducer-5854-claim-latch-%d", trial)
		for _, db := range []*sql.DB{baselineDB, candidateDB} {
			seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
			seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
			seedContainerImageIdentityAckWorkItem(
				t, ctx, db, workItemID, scopeID, generationID,
				owner, now.Add(time.Minute), now,
			)
			prepareContainerImageIdentityAckPerformanceTable(t, db)
		}
		baselineQueue := ReducerQueue{
			db: SQLDB{DB: baselineDB}, LeaseOwner: owner,
			LeaseDuration: time.Minute, Now: func() time.Time { return now },
			ClaimDomain: reducer.DomainContainerImageIdentity,
		}
		candidateQueue := baselineQueue
		candidateQueue.db = SQLDB{DB: candidateDB}
		before, after := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfPending(
					t, ctx, baselineDB, now, workItemID,
				)
			},
			func() {
				resetContainerImageIdentityClaimLatchPerfPending(
					t, ctx, candidateDB, now, workItemID,
				)
			},
			func() error {
				return claimContainerImageIdentityPerformanceRow(
					ctx, baselineQueue, preLatchContainerImageIdentityClaimQuery, workItemID,
				)
			},
			func() error {
				return claimContainerImageIdentityPerformanceRow(
					ctx, candidateQueue, claimReducerWorkQuery, workItemID,
				)
			},
		)
		beforeTrials = append(beforeTrials, before)
		afterTrials = append(afterTrials, after)
		t.Logf(
			"CLAIMLATCH5854 trial=%d warmups=%d pairs=%d before_median_us=%.3f "+
				"before_p95_us=%.3f after_median_us=%.3f after_p95_us=%.3f",
			trial+1, warmups, pairedIterations,
			ackPerfMicros(before.median), ackPerfMicros(before.p95),
			ackPerfMicros(after.median), ackPerfMicros(after.p95),
		)
	}
	before := medianContainerImageIdentityAckTrialStats(beforeTrials)
	after := medianContainerImageIdentityAckTrialStats(afterTrials)
	assertContainerImageIdentityAckPerfBudget(
		t,
		"same-schema claim-time capability latch",
		before,
		after,
	)
	t.Logf(
		"CLAIMLATCH5854 aggregate before_median_us=%.3f before_p95_us=%.3f "+
			"after_median_us=%.3f after_p95_us=%.3f",
		ackPerfMicros(before.median), ackPerfMicros(before.p95),
		ackPerfMicros(after.median), ackPerfMicros(after.p95),
	)
}

func TestContainerImageIdentityClaimLatchBatchPerformanceLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 31, 14, 30, 0, 0, time.UTC)

	for _, batchSize := range []int{1, 16, 64} {
		t.Run(fmt.Sprintf("batch_%d", batchSize), func(t *testing.T) {
			const (
				trials           = 4
				warmups          = 50
				pairedIterations = 200
			)
			beforeTrials := make([]containerImageIdentityAckPerfStats, 0, trials)
			afterTrials := make([]containerImageIdentityAckPerfStats, 0, trials)
			for trial := range trials {
				baselineDB, candidateDB := openContainerImageIdentityClaimPerfPair(t, trial)
				pinContainerImageIdentityAckTheoryDB(baselineDB)
				pinContainerImageIdentityAckTheoryDB(candidateDB)
				owner := fmt.Sprintf("reducer-5854-latch-batch-%d-%d", batchSize, trial)
				workItemIDs := make([]string, 0, batchSize)
				for index := range batchSize {
					scopeID := fmt.Sprintf(
						"repository:5854-latch-batch-%d-%d-%d",
						batchSize, trial, index,
					)
					generationID := fmt.Sprintf(
						"generation:5854-latch-batch-%d-%d-%d",
						batchSize, trial, index,
					)
					workItemID := fmt.Sprintf(
						"claim-5854-latch-batch-%d-%d-%d",
						batchSize, trial, index,
					)
					workItemIDs = append(workItemIDs, workItemID)
					for _, db := range []*sql.DB{baselineDB, candidateDB} {
						seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
						seedContainerImageIdentityAckGeneration(
							t, ctx, db, scopeID, generationID,
						)
						seedContainerImageIdentityAckWorkItem(
							t, ctx, db, workItemID, scopeID, generationID,
							owner, now.Add(time.Minute), now,
						)
					}
				}
				prepareContainerImageIdentityAckPerformanceTable(t, baselineDB)
				prepareContainerImageIdentityAckPerformanceTable(t, candidateDB)
				baselineQueue := ReducerQueue{
					db: SQLDB{DB: baselineDB}, LeaseOwner: owner,
					LeaseDuration: time.Minute, Now: func() time.Time { return now },
					ClaimDomain: reducer.DomainContainerImageIdentity,
				}
				candidateQueue := baselineQueue
				candidateQueue.db = SQLDB{DB: candidateDB}
				before, after := measureContainerImageIdentityAckTwinPair(
					t,
					warmups,
					pairedIterations,
					func() {
						resetContainerImageIdentityClaimBatchPerfPending(
							t, ctx, baselineDB, now, workItemIDs, false,
						)
					},
					func() {
						resetContainerImageIdentityClaimBatchPerfPending(
							t, ctx, candidateDB, now, workItemIDs, true,
						)
					},
					func() error {
						return claimContainerImageIdentityPerformanceBatch(
							ctx, baselineQueue,
							preLatchContainerImageIdentityClaimBatchQuery,
							batchSize, workItemIDs,
						)
					},
					func() error {
						return claimContainerImageIdentityPerformanceBatch(
							ctx, candidateQueue, claimReducerWorkBatchQuery,
							batchSize, workItemIDs,
						)
					},
				)
				beforeTrials = append(beforeTrials, before)
				afterTrials = append(afterTrials, after)
			}
			before := medianContainerImageIdentityAckTrialStats(beforeTrials)
			after := medianContainerImageIdentityAckTrialStats(afterTrials)
			assertContainerImageIdentityAckPerfBudget(
				t,
				fmt.Sprintf("same-schema claim-time latch batch %d", batchSize),
				before,
				after,
			)
			t.Logf(
				"CLAIMLATCHBATCH5854 batch=%d before_median_us=%.3f "+
					"before_p95_us=%.3f after_median_us=%.3f after_p95_us=%.3f",
				batchSize,
				ackPerfMicros(before.median), ackPerfMicros(before.p95),
				ackPerfMicros(after.median), ackPerfMicros(after.p95),
			)
		})
	}
}

func TestContainerImageIdentityPreLatchPerformanceQueryIsDistinct(t *testing.T) {
	if preLatchContainerImageIdentityClaimQuery == claimReducerWorkQuery {
		t.Fatal("derived pre-latch claim query equals current claim query")
	}
	if strings.Contains(
		preLatchContainerImageIdentityClaimQuery,
		"container_image_identity_v2_required =",
	) {
		t.Fatal("derived pre-latch claim query still writes the capability latch")
	}
	if !strings.Contains(
		preLatchContainerImageIdentityClaimQuery,
		"THEN work.container_image_identity_claim_epoch + 1",
	) {
		t.Fatal("derived pre-latch query lost the exact prior-head epoch advance")
	}
	if preLatchContainerImageIdentityClaimBatchQuery == claimReducerWorkBatchQuery {
		t.Fatal("derived pre-latch batch query equals current batch query")
	}
}

func resetContainerImageIdentityClaimBatchPerfPending(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	now time.Time,
	workItemIDs []string,
	latched bool,
) {
	t.Helper()
	authorizedStatus := ""
	if latched {
		authorizedStatus = "pending"
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending',
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required THEN $3
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
WHERE work_item_id = ANY($2::text[])
`, now, workItemIDs, authorizedStatus); err != nil {
		t.Fatalf("reset batch claim-latch perf rows: %v", err)
	}
}

func claimContainerImageIdentityPerformanceBatch(
	ctx context.Context,
	queue ReducerQueue,
	query string,
	limit int,
	expectedIDs []string,
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
		limit,
	)
	if err != nil {
		return fmt.Errorf("claim performance reducer batch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	actualIDs := make([]string, 0, len(expectedIDs))
	for rows.Next() {
		intent, scanErr := scanReducerIntent(rows)
		if scanErr != nil {
			return fmt.Errorf("scan performance reducer batch: %w", scanErr)
		}
		actualIDs = append(actualIDs, intent.IntentID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("claim performance reducer batch: %w", err)
	}
	sort.Strings(actualIDs)
	wantIDs := append([]string(nil), expectedIDs...)
	sort.Strings(wantIDs)
	if strings.Join(actualIDs, "\x00") != strings.Join(wantIDs, "\x00") {
		return fmt.Errorf(
			"performance batch IDs = %v, want %v",
			actualIDs,
			wantIDs,
		)
	}
	return nil
}
