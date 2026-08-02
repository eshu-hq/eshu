// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_ack && perf5740_completion

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const legacyContainerImageIdentityCompletionAckBatchQuery = `
UPDATE fact_work_items
SET status = 'succeeded',
    container_image_identity_v2_authorized_status = CASE
        WHEN container_image_identity_v2_required THEN 'succeeded' ELSE '' END,
    container_image_identity_v3_authorized_status = CASE
        WHEN container_image_identity_v3_required THEN 'succeeded' ELSE '' END,
    lease_owner = NULL, claim_until = NULL, visible_at = NULL,
    updated_at = $1, failure_class = NULL,
    failure_message = NULL, failure_details = NULL
WHERE work_item_id = ANY($3::text[])
  AND container_image_identity_claim_epoch = $4
  AND stage = 'reducer'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
`

func TestCrossScopeCompletionFinalShapePerformanceLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	baselineDB := openContainerImageIdentityAckCapabilityProofDB(t)
	candidateDB := openContainerImageIdentityAckCapabilityProofDB(t)
	pinContainerImageIdentityAckTheoryDB(baselineDB)
	pinContainerImageIdentityAckTheoryDB(candidateDB)
	if _, err := baselineDB.ExecContext(ctx, `
DROP TABLE cross_scope_completion_events;
DROP TABLE cross_scope_completion_upgrade_markers;
DROP INDEX fact_work_items_cross_scope_source_idx;
ALTER TABLE fact_work_items
    DROP COLUMN cross_scope_replay_required CASCADE,
    DROP COLUMN cross_scope_completion_ack_epoch CASCADE;
DROP FUNCTION enforce_cross_scope_required_replay();
DROP FUNCTION enqueue_cross_scope_completion_event()
`); err != nil {
		t.Fatalf("restore pre-093 completion baseline: %v", err)
	}
	const (
		owner      = "reducer-5740-performance"
		maxRows    = 800
		warmups    = 30
		iterations = 200
	)
	for _, db := range []*sql.DB{baselineDB, candidateDB} {
		seedCrossScopeCompletionPerformanceRows(t, ctx, db, maxRows, owner)
	}
	baselineQueue := ReducerQueue{
		db:            SQLDB{DB: baselineDB},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
	}
	candidateQueue := baselineQueue
	candidateQueue.db = SQLDB{DB: candidateDB}

	for _, size := range []int{1, 50, 500} {
		identity := crossScopeCompletionPerfIntents("identity", reducer.DomainContainerImageIdentity, size)
		before, after := measureContainerImageIdentityAckTwinPair(
			t, warmups, iterations,
			func() { resetCrossScopeCompletionPerfRows(t, ctx, baselineDB, owner, identity) },
			func() { resetCrossScopeCompletionPerfRows(t, ctx, candidateDB, owner, identity) },
			func() error { return legacyCrossScopeCompletionIdentityAck(ctx, baselineDB, owner, identity) },
			func() error { return candidateQueue.AckBatch(ctx, identity, nil) },
		)
		assertCrossScopeCompletionPerfDelta(t, fmt.Sprintf("identity batch%d", size), before, after, time.Millisecond)
		logCrossScopeCompletionPerf(t, fmt.Sprintf("identity_batch_%d", size), before, after)
	}

	mixed := append(
		crossScopeCompletionPerfIntents("mixed_identity", reducer.DomainContainerImageIdentity, 16),
		crossScopeCompletionPerfIntents("mixed_cicd", reducer.DomainCICDRunCorrelation, 16)...,
	)
	mixed = append(mixed, crossScopeCompletionPerfIntents("mixed_ownership", reducer.DomainOwnership, 16)...)
	mixedBefore, mixedAfter := measureContainerImageIdentityAckTwinPair(
		t, warmups, iterations,
		func() { resetCrossScopeCompletionPerfRows(t, ctx, baselineDB, owner, mixed) },
		func() { resetCrossScopeCompletionPerfRows(t, ctx, candidateDB, owner, mixed) },
		func() error { return legacyCrossScopeCompletionMixedAck(ctx, baselineDB, owner, mixed) },
		func() error { return candidateQueue.AckBatch(ctx, mixed, nil) },
	)
	assertCrossScopeCompletionPerfDelta(t, "mixed batch48", mixedBefore, mixedAfter, time.Millisecond)
	logCrossScopeCompletionPerf(t, "mixed_batch_48", mixedBefore, mixedAfter)

	unrelated := crossScopeCompletionPerfIntents("unrelated_ack", reducer.DomainOwnership, 500)
	unrelatedBefore, unrelatedAfter := measureContainerImageIdentityAckTwinPair(
		t, warmups, iterations,
		func() { resetCrossScopeCompletionPerfRows(t, ctx, baselineDB, owner, unrelated) },
		func() { resetCrossScopeCompletionPerfRows(t, ctx, candidateDB, owner, unrelated) },
		func() error { return baselineQueue.AckBatch(ctx, unrelated, nil) },
		func() error { return candidateQueue.AckBatch(ctx, unrelated, nil) },
	)
	assertCrossScopeCompletionPerfDelta(t, "unrelated batch500 ACK trigger tax", unrelatedBefore, unrelatedAfter, 250*time.Microsecond)
	logCrossScopeCompletionPerf(t, "unrelated_ack_batch_500", unrelatedBefore, unrelatedAfter)
	measureCrossScopeCompletionUnrelatedOperations(
		t, ctx, baselineDB, candidateDB, baselineQueue, candidateQueue, owner,
	)

	concurrent := crossScopeCompletionPerfIntents("concurrent_identity", reducer.DomainContainerImageIdentity, 800)
	baselineDB.SetMaxOpenConns(20)
	baselineDB.SetMaxIdleConns(20)
	candidateDB.SetMaxOpenConns(20)
	candidateDB.SetMaxIdleConns(20)
	concurrentBefore, concurrentAfter := measureContainerImageIdentityAckTwinPair(
		t, 10, 80,
		func() { resetCrossScopeCompletionPerfRows(t, ctx, baselineDB, owner, concurrent) },
		func() { resetCrossScopeCompletionPerfRows(t, ctx, candidateDB, owner, concurrent) },
		func() error {
			return runConcurrentCrossScopeCompletionAck(ctx, baselineDB, baselineQueue, owner, concurrent, true)
		},
		func() error {
			return runConcurrentCrossScopeCompletionAck(ctx, candidateDB, candidateQueue, owner, concurrent, false)
		},
	)
	assertCrossScopeCompletionPerfDelta(t, "16-client identity batch50", concurrentBefore, concurrentAfter, 5*time.Millisecond)
	logCrossScopeCompletionPerf(t, "identity_16_clients_batch_50", concurrentBefore, concurrentAfter)
}

func seedCrossScopeCompletionPerformanceRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	rowCount int,
	owner string,
) {
	t.Helper()
	const (
		scopeID    = "repository:5740-performance"
		generation = "generation:5740-performance"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate completion performance scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, attempt_count, lease_owner, claim_until,
    payload, created_at, updated_at, container_image_identity_claim_epoch
)
SELECT format('reducer_5740_perf_%s_%s', fixture.prefix, row_number),
       $1, $2, 'reducer', fixture.domain, 'claimed',
       'intent', format('reducer_5740_perf_%s_%s', fixture.prefix, row_number),
       1, $4, clock_timestamp() + INTERVAL '1 minute',
       jsonb_build_object('padding', repeat(md5(row_number::text || fixture.prefix), 1024)),
       clock_timestamp(), clock_timestamp(),
       CASE WHEN fixture.domain = 'container_image_identity' THEN 1 ELSE 0 END
FROM generate_series(1, $3) AS row_number
	CROSS JOIN (VALUES
    ('identity', 'container_image_identity'),
    ('mixed_identity', 'container_image_identity'),
    ('mixed_cicd', 'ci_cd_run_correlation'),
    ('mixed_ownership', 'ownership'),
	    ('unrelated_ack', 'ownership'),
	    ('claim', 'ownership'),
	    ('heartbeat', 'ownership'),
	    ('retry', 'ownership'),
	    ('fail', 'ownership'),
	    ('concurrent_identity', 'container_image_identity')
) AS fixture(prefix, domain)
`, scopeID, generation, rowCount, owner); err != nil {
		t.Fatalf("seed completion performance rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_work_items`); err != nil {
		t.Fatalf("analyze completion performance rows: %v", err)
	}
}

type crossScopeCompletionRetryablePerfError struct{}

func (crossScopeCompletionRetryablePerfError) Error() string   { return "synthetic retry" }
func (crossScopeCompletionRetryablePerfError) Retryable() bool { return true }

func measureCrossScopeCompletionUnrelatedOperations(
	t *testing.T,
	ctx context.Context,
	baselineDB *sql.DB,
	candidateDB *sql.DB,
	baselineQueue ReducerQueue,
	candidateQueue ReducerQueue,
	owner string,
) {
	t.Helper()
	claim := crossScopeCompletionPerfIntents("claim", reducer.DomainOwnership, 500)
	baselineClaimQueue := baselineQueue
	baselineClaimQueue.ClaimDomain = reducer.DomainOwnership
	candidateClaimQueue := candidateQueue
	candidateClaimQueue.ClaimDomain = reducer.DomainOwnership
	resetClaim := func(db *sql.DB) {
		ids := crossScopeCompletionPerfIDs(claim)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending', attempt_count = 0, lease_owner = NULL,
    claim_until = NULL, visible_at = clock_timestamp() - INTERVAL '1 second'
WHERE work_item_id = ANY($1::text[])
`, ids); err != nil {
			t.Fatalf("reset completion claim performance rows: %v", err)
		}
	}
	claimBefore, claimAfter := measureContainerImageIdentityAckTwinPair(
		t, 20, 100,
		func() { resetClaim(baselineDB) },
		func() { resetClaim(candidateDB) },
		func() error {
			intents, err := baselineClaimQueue.ClaimBatch(ctx, len(claim))
			if err == nil && len(intents) != len(claim) {
				return fmt.Errorf("baseline claim count=%d", len(intents))
			}
			return err
		},
		func() error {
			intents, err := candidateClaimQueue.ClaimBatch(ctx, len(claim))
			if err == nil && len(intents) != len(claim) {
				return fmt.Errorf("candidate claim count=%d", len(intents))
			}
			return err
		},
	)
	assertCrossScopeCompletionRelativePerf(t, "unrelated ClaimBatch500", claimBefore, claimAfter, 500*time.Microsecond)
	logCrossScopeCompletionPerf(t, "unrelated_claim_batch_500", claimBefore, claimAfter)

	heartbeat := crossScopeCompletionPerfIntents("heartbeat", reducer.DomainOwnership, 1)
	heartbeatBefore, heartbeatAfter := measureContainerImageIdentityAckTwinPair(
		t, 50, 500,
		func() { resetCrossScopeCompletionPerfRows(t, ctx, baselineDB, owner, heartbeat) },
		func() { resetCrossScopeCompletionPerfRows(t, ctx, candidateDB, owner, heartbeat) },
		func() error { return baselineQueue.Heartbeat(ctx, heartbeat[0]) },
		func() error { return candidateQueue.Heartbeat(ctx, heartbeat[0]) },
	)
	assertCrossScopeCompletionRelativePerf(t, "unrelated heartbeat", heartbeatBefore, heartbeatAfter, 25*time.Microsecond)
	logCrossScopeCompletionPerf(t, "unrelated_heartbeat", heartbeatBefore, heartbeatAfter)

	for _, operation := range []struct {
		name   string
		prefix string
		cause  error
	}{
		{name: "retry", prefix: "retry", cause: crossScopeCompletionRetryablePerfError{}},
		{name: "fail", prefix: "fail", cause: errors.New("synthetic terminal failure")},
	} {
		intent := crossScopeCompletionPerfIntents(operation.prefix, reducer.DomainOwnership, 1)
		before, after := measureContainerImageIdentityAckTwinPair(
			t, 50, 500,
			func() { resetCrossScopeCompletionPerfRows(t, ctx, baselineDB, owner, intent) },
			func() { resetCrossScopeCompletionPerfRows(t, ctx, candidateDB, owner, intent) },
			func() error {
				return legacyCrossScopeCompletionFail(ctx, baselineDB, owner, intent[0], operation.name == "retry")
			},
			func() error { return candidateQueue.Fail(ctx, intent[0], operation.cause) },
		)
		assertCrossScopeCompletionRelativePerf(t, "unrelated "+operation.name, before, after, 50*time.Microsecond)
		logCrossScopeCompletionPerf(t, "unrelated_"+operation.name, before, after)
	}
}

func legacyCrossScopeCompletionFail(
	ctx context.Context,
	db *sql.DB,
	owner string,
	intent reducer.Intent,
	retry bool,
) error {
	if retry {
		_, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'retrying', lease_owner = NULL, claim_until = NULL,
    visible_at = $1, next_attempt_at = $1, updated_at = clock_timestamp(),
    failure_class = 'reducer_retryable', failure_message = 'synthetic retry',
    failure_details = NULL
WHERE work_item_id = $2 AND stage = 'reducer' AND lease_owner = $3
  AND status IN ('claimed', 'running')
`, time.Now().UTC().Add(time.Second), intent.IntentID, owner)
		return err
	}
	_, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'dead_letter', lease_owner = NULL, claim_until = NULL,
    visible_at = NULL, updated_at = clock_timestamp(),
    failure_class = 'reducer_failed', failure_message = 'synthetic terminal failure',
    failure_details = NULL
WHERE work_item_id = $1 AND stage = 'reducer' AND lease_owner = $2
  AND status IN ('claimed', 'running')
`, intent.IntentID, owner)
	return err
}

func crossScopeCompletionPerfIDs(intents []reducer.Intent) []string {
	ids := make([]string, len(intents))
	for index, intent := range intents {
		ids[index] = intent.IntentID
	}
	return ids
}

func crossScopeCompletionPerfIntents(prefix string, domain reducer.Domain, count int) []reducer.Intent {
	intents := make([]reducer.Intent, count)
	for index := range count {
		intents[index] = reducer.Intent{
			IntentID: fmt.Sprintf("reducer_5740_perf_%s_%d", prefix, index+1),
			Domain:   domain,
		}
		if domain == reducer.DomainContainerImageIdentity {
			intents[index].ClaimEpoch = 1
		}
	}
	return intents
}

func resetCrossScopeCompletionPerfRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	owner string,
	intents []reducer.Intent,
) {
	t.Helper()
	ids := make([]string, len(intents))
	for index, intent := range intents {
		ids[index] = intent.IntentID
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', attempt_count = 1, lease_owner = $2,
    claim_until = clock_timestamp() + INTERVAL '1 minute', visible_at = NULL,
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE work_item_id = ANY($1::text[])
`, ids, owner); err != nil {
		t.Fatalf("reset completion performance rows: %v", err)
	}
}

func legacyCrossScopeCompletionIdentityAck(
	ctx context.Context,
	db *sql.DB,
	owner string,
	intents []reducer.Intent,
) error {
	ids := make([]string, len(intents))
	for index, intent := range intents {
		ids[index] = intent.IntentID
	}
	_, err := db.ExecContext(
		ctx,
		legacyContainerImageIdentityCompletionAckBatchQuery,
		time.Now().UTC(), owner, ids, int64(1),
	)
	return err
}

func legacyCrossScopeCompletionMixedAck(
	ctx context.Context,
	db *sql.DB,
	owner string,
	intents []reducer.Intent,
) error {
	var identity, unrelated []reducer.Intent
	for _, intent := range intents {
		if intent.Domain == reducer.DomainContainerImageIdentity {
			identity = append(identity, intent)
		} else {
			unrelated = append(unrelated, intent)
		}
	}
	if err := legacyCrossScopeCompletionIdentityAck(ctx, db, owner, identity); err != nil {
		return err
	}
	args := []any{time.Now().UTC(), owner}
	for _, intent := range unrelated {
		args = append(args, intent.IntentID)
	}
	_, err := db.ExecContext(ctx, ackReducerWorkBatchQuery(len(unrelated)), args...)
	return err
}

func runConcurrentCrossScopeCompletionAck(
	ctx context.Context,
	db *sql.DB,
	queue ReducerQueue,
	owner string,
	intents []reducer.Intent,
	legacy bool,
) error {
	const clients = 16
	var wait sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(clients)
	start := make(chan struct{})
	errorsFound := make(chan error, clients)
	for client := range clients {
		batch := intents[client*50 : (client+1)*50]
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			var err error
			if legacy {
				err = legacyCrossScopeCompletionIdentityAck(ctx, db, owner, batch)
			} else {
				err = queue.AckBatch(ctx, batch, nil)
			}
			if err != nil {
				errorsFound <- err
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		return err
	}
	return nil
}

func assertCrossScopeCompletionPerfDelta(
	t *testing.T,
	name string,
	before containerImageIdentityAckPerfStats,
	after containerImageIdentityAckPerfStats,
	maxP95Delta time.Duration,
) {
	t.Helper()
	if delta := after.p95 - before.p95; delta > maxP95Delta {
		t.Errorf("%s p95 delta=%s, want <=%s", name, delta, maxP95Delta)
	}
}

func assertCrossScopeCompletionRelativePerf(
	t *testing.T,
	name string,
	before containerImageIdentityAckPerfStats,
	after containerImageIdentityAckPerfStats,
	absMargin time.Duration,
) {
	t.Helper()
	limit := time.Duration(float64(before.p95)*1.10) + absMargin
	if after.p95 > limit {
		t.Errorf("%s p95=%s, want <=%s (baseline=%s)", name, after.p95, limit, before.p95)
	}
}

func logCrossScopeCompletionPerf(
	t *testing.T,
	name string,
	before containerImageIdentityAckPerfStats,
	after containerImageIdentityAckPerfStats,
) {
	t.Helper()
	t.Logf(
		"CROSSSCOPEPERF5740 operation=%s before_median_us=%.3f before_p95_us=%.3f after_median_us=%.3f after_p95_us=%.3f delta_p95_us=%.3f",
		name,
		ackPerfMicros(before.median),
		ackPerfMicros(before.p95),
		ackPerfMicros(after.median),
		ackPerfMicros(after.p95),
		ackPerfMicros(after.p95-before.p95),
	)
}
