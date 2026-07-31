// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_ack

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityClaimFixedSchemaPerformanceLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
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
		installContainerImageIdentityAckBaselineSchema(t, baselineDB)

		scopeID := fmt.Sprintf("repository:5854-target-claim-%d", trial)
		generationID := fmt.Sprintf("generation:5854-target-claim-%d", trial)
		workItemID := fmt.Sprintf("claim-5854-target-%d", trial)
		owner := fmt.Sprintf("reducer-5854-target-claim-%d", trial)
		for _, db := range []*sql.DB{baselineDB, candidateDB} {
			seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
			seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
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
			prepareContainerImageIdentityAckPerformanceTable(t, db)
		}

		baselineQueue := ReducerQueue{
			db:            SQLDB{DB: baselineDB},
			LeaseOwner:    owner,
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now },
			ClaimDomain:   reducer.DomainContainerImageIdentity,
		}
		candidateQueue := baselineQueue
		candidateQueue.db = SQLDB{DB: candidateDB}
		before, after := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfPending(t, ctx, baselineDB, now, workItemID)
			},
			func() {
				resetContainerImageIdentityAckPerfPending(t, ctx, candidateDB, now, workItemID)
			},
			func() error {
				return claimContainerImageIdentityLegacyPerformanceRow(
					ctx,
					baselineQueue,
					workItemID,
				)
			},
			func() error {
				return claimContainerImageIdentityPerformanceRow(
					ctx,
					candidateQueue,
					claimReducerWorkQuery,
					workItemID,
				)
			},
		)
		beforeTrials = append(beforeTrials, before)
		afterTrials = append(afterTrials, after)
		t.Logf(
			"CLAIMTWIN5854 trial=%d warmups=%d pairs=%d target_before_median_us=%.3f target_before_p95_us=%.3f target_after_median_us=%.3f target_after_p95_us=%.3f",
			trial+1,
			warmups,
			pairedIterations,
			ackPerfMicros(before.median),
			ackPerfMicros(before.p95),
			ackPerfMicros(after.median),
			ackPerfMicros(after.p95),
		)
	}

	before := medianContainerImageIdentityAckTrialStats(beforeTrials)
	after := medianContainerImageIdentityAckTrialStats(afterTrials)
	assertContainerImageIdentityAckPerfBudget(
		t,
		"twin-schema pre-cutover target claim",
		before,
		after,
	)
	t.Logf(
		"CLAIMTWIN5854 aggregate target_before_median_us=%.3f target_before_p95_us=%.3f target_after_median_us=%.3f target_after_p95_us=%.3f",
		ackPerfMicros(before.median),
		ackPerfMicros(before.p95),
		ackPerfMicros(after.median),
		ackPerfMicros(after.p95),
	)
}

func openContainerImageIdentityClaimPerfPair(
	t *testing.T,
	trial int,
) (*sql.DB, *sql.DB) {
	t.Helper()
	if trial%2 == 0 {
		return openContainerImageIdentityAckCapabilityProofDB(t),
			openContainerImageIdentityAckCapabilityProofDB(t)
	}
	candidateDB := openContainerImageIdentityAckCapabilityProofDB(t)
	baselineDB := openContainerImageIdentityAckCapabilityProofDB(t)
	return baselineDB, candidateDB
}
