//go:build perf5854_ack

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityAckFixedSchemaPerformanceLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 17, 0, 0, 0, time.UTC)
	const (
		trials           = 6
		warmups          = 100
		pairedIterations = 1000
		batchSize        = 64
		handlerBaseline  = 26023 * time.Microsecond
	)
	var (
		singleBeforeTrials    []containerImageIdentityAckPerfStats
		singleAfterTrials     []containerImageIdentityAckPerfStats
		batchBeforeTrials     []containerImageIdentityAckPerfStats
		batchAfterTrials      []containerImageIdentityAckPerfStats
		unrelatedBeforeTrials []containerImageIdentityAckPerfStats
		unrelatedAfterTrials  []containerImageIdentityAckPerfStats
		failBeforeTrials      []containerImageIdentityAckPerfStats
		failAfterTrials       []containerImageIdentityAckPerfStats
		claimBeforeTrials     []containerImageIdentityAckPerfStats
		claimAfterTrials      []containerImageIdentityAckPerfStats
	)

	for trial := range trials {
		var baselineDB, candidateDB *sql.DB
		if trial%2 == 0 {
			baselineDB = openContainerImageIdentityAckCapabilityProofDB(t)
			candidateDB = openContainerImageIdentityAckCapabilityProofDB(t)
		} else {
			candidateDB = openContainerImageIdentityAckCapabilityProofDB(t)
			baselineDB = openContainerImageIdentityAckCapabilityProofDB(t)
		}
		pinContainerImageIdentityAckTheoryDB(baselineDB)
		pinContainerImageIdentityAckTheoryDB(candidateDB)
		installContainerImageIdentityAckBaselineSchema(t, baselineDB)

		owner := fmt.Sprintf("reducer-5854-twin-%d", trial)
		ids := make([]string, batchSize)
		attempts := make([]int, batchSize)
		for index := range batchSize {
			scopeID := fmt.Sprintf(
				"repository:5854-twin-%d-%02d",
				trial,
				index,
			)
			generationID := fmt.Sprintf(
				"generation:5854-twin-%d-%02d",
				trial,
				index,
			)
			ids[index] = fmt.Sprintf("ack-5854-twin-%d-%02d", trial, index)
			attempts[index] = 1
			for _, db := range []*sql.DB{baselineDB, candidateDB} {
				seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
				seedContainerImageIdentityAckGeneration(
					t,
					ctx,
					db,
					scopeID,
					generationID,
				)
				seedContainerImageIdentityAckWorkItem(
					t,
					ctx,
					db,
					ids[index],
					scopeID,
					generationID,
					owner,
					now.Add(time.Minute),
					now,
				)
			}
			insertContainerImageIdentityCutoverMarker(
				t,
				ctx,
				candidateDB,
				scopeID,
				generationID,
			)
			if _, err := baselineDB.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id,
    activated_by_work_item_id,
    activated_by_claim_epoch
) VALUES ($1, $2, $3, 1)
`, scopeID, generationID, ids[index]); err != nil {
				t.Fatalf("match baseline cutover marker distribution: %v", err)
			}
			if _, err := baselineDB.ExecContext(
				ctx,
				`UPDATE fact_work_items SET status = 'running' WHERE work_item_id = $1`,
				ids[index],
			); err != nil {
				t.Fatalf("match baseline target status distribution: %v", err)
			}
		}
		unrelatedScopeID := fmt.Sprintf(
			"repository:5854-twin-%d-unrelated",
			trial,
		)
		unrelatedGenerationID := fmt.Sprintf(
			"generation:5854-twin-%d-unrelated",
			trial,
		)
		unrelatedID := fmt.Sprintf("ack-5854-twin-%d-unrelated", trial)
		for _, db := range []*sql.DB{baselineDB, candidateDB} {
			seedContainerImageIdentityAckScope(t, ctx, db, unrelatedScopeID)
			seedContainerImageIdentityAckGeneration(
				t,
				ctx,
				db,
				unrelatedScopeID,
				unrelatedGenerationID,
			)
			seedContainerImageIdentityAckWorkItem(
				t,
				ctx,
				db,
				unrelatedID,
				unrelatedScopeID,
				unrelatedGenerationID,
				owner,
				now.Add(time.Minute),
				now,
			)
			if _, err := db.ExecContext(
				ctx,
				`UPDATE fact_work_items SET domain = 'ownership' WHERE work_item_id = $1`,
				unrelatedID,
			); err != nil {
				t.Fatalf("mark twin-schema unrelated row: %v", err)
			}
		}
		prepareContainerImageIdentityAckPerformanceTable(t, baselineDB)
		prepareContainerImageIdentityAckPerformanceTable(t, candidateDB)

		singleBefore, singleAfter := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfClaimed(
					t, ctx, baselineDB, owner, now, ids[:1],
				)
			},
			func() {
				resetContainerImageIdentityAckAttemptTheoryClaimed(
					t, ctx, candidateDB, owner, now, ids[:1],
				)
			},
			func() error {
				_, err := baselineDB.ExecContext(
					ctx, legacyContainerImageIdentityAckQuery, now, ids[0], owner,
				)
				return err
			},
			func() error {
				_, err := candidateDB.ExecContext(
					ctx,
					ackContainerImageIdentityReducerWorkQuery,
					now,
					ids[0],
					owner,
					1,
				)
				return err
			},
		)
		singleBeforeTrials = append(singleBeforeTrials, singleBefore)
		singleAfterTrials = append(singleAfterTrials, singleAfter)

		baselineBatchQuery, baselineBatchArgs := legacyContainerImageIdentityAckBatchQuery(now, owner, ids)
		candidateIntents := make([]reducer.Intent, len(ids))
		for index := range ids {
			candidateIntents[index] = reducer.Intent{
				IntentID:     ids[index],
				Domain:       reducer.DomainContainerImageIdentity,
				AttemptCount: attempts[index],
				ClaimEpoch:   1,
			}
		}
		candidateBatchQuery, candidateBatchArgs := ackContainerImageIdentityReducerWorkBatchQuery(
			now,
			owner,
			candidateIntents,
		)
		batchBefore, batchAfter := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfClaimed(
					t, ctx, baselineDB, owner, now, ids,
				)
			},
			func() {
				resetContainerImageIdentityAckAttemptTheoryClaimed(
					t, ctx, candidateDB, owner, now, ids,
				)
			},
			func() error {
				_, err := baselineDB.ExecContext(
					ctx, baselineBatchQuery, baselineBatchArgs...,
				)
				return err
			},
			func() error {
				_, err := candidateDB.ExecContext(
					ctx, candidateBatchQuery, candidateBatchArgs...,
				)
				return err
			},
		)
		batchBeforeTrials = append(batchBeforeTrials, batchBefore)
		batchAfterTrials = append(batchAfterTrials, batchAfter)

		unrelatedBefore, unrelatedAfter := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfClaimed(
					t, ctx, baselineDB, owner, now, []string{unrelatedID},
				)
			},
			func() {
				resetContainerImageIdentityAckAttemptTheoryClaimed(
					t, ctx, candidateDB, owner, now, []string{unrelatedID},
				)
			},
			func() error {
				_, err := baselineDB.ExecContext(
					ctx, legacyContainerImageIdentityAckQuery, now, unrelatedID, owner,
				)
				return err
			},
			func() error {
				_, err := candidateDB.ExecContext(
					ctx, legacyContainerImageIdentityAckQuery, now, unrelatedID, owner,
				)
				return err
			},
		)
		unrelatedBeforeTrials = append(unrelatedBeforeTrials, unrelatedBefore)
		unrelatedAfterTrials = append(unrelatedAfterTrials, unrelatedAfter)
		baselineQueue := ReducerQueue{
			db:            SQLDB{DB: baselineDB},
			LeaseOwner:    owner,
			LeaseDuration: time.Minute,
			MaxAttempts:   1,
			Now:           func() time.Time { return now },
		}
		candidateQueue := baselineQueue
		candidateQueue.db = SQLDB{DB: candidateDB}
		failure := errors.New("synthetic fixed-schema failure")
		failBefore, failAfter := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfClaimed(
					t, ctx, baselineDB, owner, now, []string{unrelatedID},
				)
			},
			func() {
				resetContainerImageIdentityAckAttemptTheoryClaimed(
					t, ctx, candidateDB, owner, now, []string{unrelatedID},
				)
			},
			func() error {
				return baselineQueue.Fail(
					ctx,
					reducer.Intent{IntentID: unrelatedID, AttemptCount: 1},
					failure,
				)
			},
			func() error {
				return candidateQueue.Fail(
					ctx,
					reducer.Intent{IntentID: unrelatedID, AttemptCount: 1},
					failure,
				)
			},
		)
		failBeforeTrials = append(failBeforeTrials, failBefore)
		failAfterTrials = append(failAfterTrials, failAfter)
		claimBefore, claimAfter := measureContainerImageIdentityAckTwinPair(
			t,
			warmups,
			pairedIterations,
			func() {
				resetContainerImageIdentityAckPerfPending(
					t, ctx, baselineDB, now, unrelatedID,
				)
			},
			func() {
				resetContainerImageIdentityAckPerfPending(
					t, ctx, candidateDB, now, unrelatedID,
				)
			},
			func() error {
				return claimContainerImageIdentityLegacyPerformanceRow(
					ctx,
					baselineQueue,
					unrelatedID,
				)
			},
			func() error {
				return claimContainerImageIdentityPerformanceRow(
					ctx,
					candidateQueue,
					claimReducerWorkQuery,
					unrelatedID,
				)
			},
		)
		claimBeforeTrials = append(claimBeforeTrials, claimBefore)
		claimAfterTrials = append(claimAfterTrials, claimAfter)
		t.Logf(
			"ACKTWIN5854 trial=%d warmups=%d pairs=%d single_before_median_us=%.3f single_before_p95_us=%.3f single_after_median_us=%.3f single_after_p95_us=%.3f batch_before_median_us=%.3f batch_before_p95_us=%.3f batch_after_median_us=%.3f batch_after_p95_us=%.3f unrelated_before_median_us=%.3f unrelated_before_p95_us=%.3f unrelated_after_median_us=%.3f unrelated_after_p95_us=%.3f fail_before_median_us=%.3f fail_before_p95_us=%.3f fail_after_median_us=%.3f fail_after_p95_us=%.3f claim_before_median_us=%.3f claim_before_p95_us=%.3f claim_after_median_us=%.3f claim_after_p95_us=%.3f",
			trial+1,
			warmups,
			pairedIterations,
			ackPerfMicros(singleBefore.median),
			ackPerfMicros(singleBefore.p95),
			ackPerfMicros(singleAfter.median),
			ackPerfMicros(singleAfter.p95),
			ackPerfMicros(batchBefore.median),
			ackPerfMicros(batchBefore.p95),
			ackPerfMicros(batchAfter.median),
			ackPerfMicros(batchAfter.p95),
			ackPerfMicros(unrelatedBefore.median),
			ackPerfMicros(unrelatedBefore.p95),
			ackPerfMicros(unrelatedAfter.median),
			ackPerfMicros(unrelatedAfter.p95),
			ackPerfMicros(failBefore.median),
			ackPerfMicros(failBefore.p95),
			ackPerfMicros(failAfter.median),
			ackPerfMicros(failAfter.p95),
			ackPerfMicros(claimBefore.median),
			ackPerfMicros(claimBefore.p95),
			ackPerfMicros(claimAfter.median),
			ackPerfMicros(claimAfter.p95),
		)
	}

	singleBefore := medianContainerImageIdentityAckTrialStats(singleBeforeTrials)
	singleAfter := medianContainerImageIdentityAckTrialStats(singleAfterTrials)
	assertContainerImageIdentityAckScalarSingleBudget(
		t,
		singleBefore,
		singleAfter,
		handlerBaseline,
	)
	batchBefore := medianContainerImageIdentityAckTrialStats(batchBeforeTrials)
	batchAfter := medianContainerImageIdentityAckTrialStats(batchAfterTrials)
	assertContainerImageIdentityAckPerfBudget(
		t,
		"twin-schema target batch64",
		batchBefore,
		batchAfter,
	)
	unrelatedBefore := medianContainerImageIdentityAckTrialStats(
		unrelatedBeforeTrials,
	)
	unrelatedAfter := medianContainerImageIdentityAckTrialStats(
		unrelatedAfterTrials,
	)
	assertContainerImageIdentityAckPerfBudget(
		t,
		"twin-schema unrelated ACK",
		unrelatedBefore,
		unrelatedAfter,
	)
	if unrelatedAfter.p95-unrelatedBefore.p95 > 25*time.Microsecond {
		t.Errorf(
			"twin-schema unrelated ACK median-trial p95 delta = %s, exceeds 25µs",
			unrelatedAfter.p95-unrelatedBefore.p95,
		)
	}
	assertContainerImageIdentityAckPerfBudget(
		t,
		"twin-schema unrelated fail",
		medianContainerImageIdentityAckTrialStats(failBeforeTrials),
		medianContainerImageIdentityAckTrialStats(failAfterTrials),
	)
	assertContainerImageIdentityAckPerfBudget(
		t,
		"twin-schema unrelated claim",
		medianContainerImageIdentityAckTrialStats(claimBeforeTrials),
		medianContainerImageIdentityAckTrialStats(claimAfterTrials),
	)
}

func pinContainerImageIdentityAckTheoryDB(db *sql.DB) {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
}

func measureContainerImageIdentityAckTwinPair(
	t *testing.T,
	warmups int,
	iterations int,
	resetBefore func(),
	resetAfter func(),
	before func() error,
	after func() error,
) (containerImageIdentityAckPerfStats, containerImageIdentityAckPerfStats) {
	t.Helper()
	run := func(reset func(), operation func() error) time.Duration {
		reset()
		started := time.Now()
		if err := operation(); err != nil {
			t.Fatalf("twin-schema ACK operation: %v", err)
		}
		return time.Since(started)
	}
	for warmup := range warmups {
		if warmup%2 == 0 {
			_ = run(resetBefore, before)
			_ = run(resetAfter, after)
		} else {
			_ = run(resetAfter, after)
			_ = run(resetBefore, before)
		}
	}
	beforeSamples := make([]time.Duration, 0, iterations)
	afterSamples := make([]time.Duration, 0, iterations)
	for iteration := range iterations {
		if iteration%2 == 0 {
			beforeSamples = append(beforeSamples, run(resetBefore, before))
			afterSamples = append(afterSamples, run(resetAfter, after))
		} else {
			afterSamples = append(afterSamples, run(resetAfter, after))
			beforeSamples = append(beforeSamples, run(resetBefore, before))
		}
	}
	return ackPerfStats(beforeSamples), ackPerfStats(afterSamples)
}

func ackPerfMicros(duration time.Duration) float64 {
	return float64(duration) / float64(time.Microsecond)
}

func medianContainerImageIdentityAckTrialStats(
	trials []containerImageIdentityAckPerfStats,
) containerImageIdentityAckPerfStats {
	medians := make([]time.Duration, len(trials))
	p95s := make([]time.Duration, len(trials))
	for index, trial := range trials {
		medians[index] = trial.median
		p95s[index] = trial.p95
	}
	sort.Slice(medians, func(left, right int) bool {
		return medians[left] < medians[right]
	})
	sort.Slice(p95s, func(left, right int) bool {
		return p95s[left] < p95s[right]
	})
	return containerImageIdentityAckPerfStats{
		median: medians[len(medians)/2],
		p95:    p95s[len(p95s)/2],
	}
}
