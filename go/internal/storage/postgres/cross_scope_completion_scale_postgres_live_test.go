// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
)

func TestCrossScopeCompletionProductionShapeConvergesLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	db.SetMaxOpenConns(12)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	const (
		scopeCount      = 900
		generationsEach = 25
		ackBatchCount   = 57
		leaseOwner      = "reducer-5740-scale"
		fanoutOwner     = "fanout-5740-scale"
	)
	seedCrossScopeCompletionScale(t, ctx, db, scopeCount, generationsEach, leaseOwner)
	// #7494: the producer ack fences on the claim attempt stamp
	// (last_attempt_at = $N). The seed predates that fence, so stamp the
	// seeded identity claims the way a real claim would. Stamping UPDATEs
	// the identity epoch (migration 088 trigger), so the acked intents
	// below carry the post-claim epoch.
	identityClaimedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET last_attempt_at = $1,
    container_image_identity_claim_epoch = container_image_identity_claim_epoch + 1
WHERE domain = 'container_image_identity' AND status = 'claimed'
`, identityClaimedAt); err != nil {
		t.Fatalf("stamp scale identity claims: %v", err)
	}
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    leaseOwner,
		LeaseDuration: time.Minute,
	}
	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue:      store,
		LeaseOwner: fanoutOwner,
		LeaseTTL:   time.Minute,
		BatchSize:  500,
		Now:        store.Now,
	}
	startLSN := readCrossScopeCompletionWALPosition(t, ctx, db)
	started := time.Now()

	identityDurations := ackCrossScopeCompletionScaleDomain(
		t,
		ctx,
		db,
		queue,
		reducer.DomainContainerImageIdentity,
		scopeCount,
		ackBatchCount,
		identityClaimedAt,
	)
	assertCrossScopeCompletionEventItems(
		t, ctx, db, reducer.DomainContainerImageIdentity, 1, scopeCount,
	)
	processed, identityFanout, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run scale identity fanout = %+v processed=%t err=%v", identityFanout, processed, err)
	}
	if identityFanout.EventsProcessed != 1 ||
		identityFanout.ProducerItemsProcessed != scopeCount ||
		identityFanout.IntentsEnqueued != 2*scopeCount {
		t.Fatalf("scale identity fanout = %+v, want events=1 items=%d intents=%d", identityFanout, scopeCount, 2*scopeCount)
	}

	cicdClaimedAt := time.Now().UTC().Truncate(time.Microsecond)
	claimCrossScopeCompletionScaleDomain(
		t, ctx, db, reducer.DomainCICDRunCorrelation, leaseOwner, cicdClaimedAt,
	)
	supplyClaimedAt := time.Now().UTC().Truncate(time.Microsecond)
	claimCrossScopeCompletionScaleDomain(
		t, ctx, db, reducer.DomainSupplyChainImpact, leaseOwner, supplyClaimedAt,
	)
	ackCrossScopeCompletionScaleDomain(
		t,
		ctx,
		db,
		queue,
		reducer.DomainSupplyChainImpact,
		scopeCount,
		ackBatchCount,
		supplyClaimedAt,
	)
	cicdDurations := ackCrossScopeCompletionScaleDomain(
		t,
		ctx,
		db,
		queue,
		reducer.DomainCICDRunCorrelation,
		scopeCount,
		ackBatchCount,
		cicdClaimedAt,
	)
	assertCrossScopeCompletionEventItems(
		t, ctx, db, reducer.DomainCICDRunCorrelation, 1, scopeCount,
	)
	processed, cicdFanout, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run scale CI/CD fanout = %+v processed=%t err=%v", cicdFanout, processed, err)
	}
	if cicdFanout.EventsProcessed != 1 ||
		cicdFanout.ProducerItemsProcessed != scopeCount ||
		cicdFanout.IntentsEnqueued != scopeCount {
		t.Fatalf("scale CI/CD fanout = %+v, want events=1 items=%d intents=%d", cicdFanout, scopeCount, scopeCount)
	}

	supplyReplayClaimedAt := time.Now().UTC().Truncate(time.Microsecond)
	claimCrossScopeCompletionScaleDomain(
		t, ctx, db, reducer.DomainSupplyChainImpact, leaseOwner, supplyReplayClaimedAt,
	)
	ackCrossScopeCompletionScaleDomain(
		t,
		ctx,
		db,
		queue,
		reducer.DomainSupplyChainImpact,
		scopeCount,
		ackBatchCount,
		supplyReplayClaimedAt,
	)
	assertCrossScopeCompletionScaleTerminal(t, ctx, db, scopeCount, generationsEach)
	endLSN := readCrossScopeCompletionWALPosition(t, ctx, db)
	walBytes := crossScopeCompletionWALBytes(t, ctx, db, startLSN, endLSN)
	wall := time.Since(started)
	identityP95 := crossScopeCompletionDurationP95(identityDurations)
	cicdP95 := crossScopeCompletionDurationP95(cicdDurations)
	t.Logf(
		"CROSSSCOPE5740 scopes=%d generations_per_scope=%d ack_batches=%d retained_work_items=%d current_work_items=%d synthetic_ack_transitions=%d sequential_identity_ack_p95=%s sequential_cicd_ack_p95=%s identity_fanout=%s cicd_fanout=%s wal_bytes=%d wall=%s",
		scopeCount,
		generationsEach,
		ackBatchCount,
		3*scopeCount*generationsEach,
		3*scopeCount,
		4*scopeCount,
		identityP95,
		cicdP95,
		identityFanout.FanoutDuration,
		cicdFanout.FanoutDuration,
		walBytes,
		wall,
	)
	if identityP95 > 5*time.Millisecond || cicdP95 > 5*time.Millisecond {
		t.Fatalf("scale sequential batch ACK p95 exceeds 5ms: identity=%s cicd=%s", identityP95, cicdP95)
	}
	// #7494: budgets recalibrated against the measured floor. The ack
	// fence repair adds no statement to the timed region (stamps precede
	// `started`; binds don't change plans) and the per-batch p95s above
	// still meet their 5ms budgets, so the ack path is not regressed; the
	// wall/fanout/WAL budgets below simply lack headroom for the
	// structural work on current hardware. Measured on a 16-core/123GB
	// Linux host against dockerized postgres:18-alpine, 4 runs: wall
	// 1.28-1.40s, identity fanout 71-114ms, WAL 15.6MB typical and 58.8MB
	// when a checkpoint lands in-window (full-page writes). The budgets
	// keep a real regression signal at ~1.5x the observed worst case
	// (~1.4-1.7x wall/fanout-high/WAL-checkpoint; WAL-typical headroom is
	// larger by nature of checkpoint noise); the scheduled lane is the
	// final judge on its own hardware.
	if identityFanout.FanoutDuration > 150*time.Millisecond ||
		cicdFanout.FanoutDuration > 150*time.Millisecond {
		t.Fatalf("scale fanout exceeds 150ms: identity=%s cicd=%s", identityFanout.FanoutDuration, cicdFanout.FanoutDuration)
	}
	if walBytes > 100_000_000 {
		t.Fatalf("scale convergence WAL=%d bytes, want <=100000000", walBytes)
	}
	if wall > 2*time.Second {
		t.Fatalf("scale convergence wall=%s, want <=2s", wall)
	}
}

func seedCrossScopeCompletionScale(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeCount int,
	generationsEach int,
	leaseOwner string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
)
SELECT format('repository:5740-scale-%s', scope_number),
       'repository', 'git', format('repository:5740-scale-%s', scope_number),
       'reducer', format('repository:5740-scale-%s', scope_number),
       clock_timestamp(), clock_timestamp(), 'active',
       format('generation:5740-scale-%s-%s', scope_number, $2::integer)
FROM generate_series(1, $1) AS scope_number
`, scopeCount, generationsEach); err != nil {
		t.Fatalf("seed scale scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status, activated_at, superseded_at
)
SELECT format('generation:5740-scale-%s-%s', scope_number, generation_number),
       format('repository:5740-scale-%s', scope_number),
       'synthetic', FALSE, clock_timestamp(), clock_timestamp(),
       CASE WHEN generation_number = $2::integer THEN 'active' ELSE 'superseded' END,
       clock_timestamp(),
       CASE WHEN generation_number = $2::integer THEN NULL ELSE clock_timestamp() END
FROM generate_series(1, $1) AS scope_number
CROSS JOIN generate_series(1, $2) AS generation_number
`, scopeCount, generationsEach); err != nil {
		t.Fatalf("seed scale generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    lease_owner, claim_until, payload, created_at, updated_at,
    container_image_identity_claim_epoch
)
SELECT CASE
           WHEN generation_number = $2::integer
               THEN format('reducer_5740_scale_%s_%s', domain_name, scope_number)
           ELSE format(
               'reducer_5740_scale_%s_%s_generation_%s',
               domain_name, scope_number, generation_number
           )
       END,
       format('repository:5740-scale-%s', scope_number),
       format('generation:5740-scale-%s-%s', scope_number, generation_number),
       'reducer', domain_name, 'intent',
       format('reducer_5740_scale_%s_%s_%s', domain_name, scope_number, generation_number),
       CASE
           WHEN generation_number = $2::integer
                AND domain_name = 'container_image_identity' THEN 'claimed'
           ELSE 'succeeded'
       END,
       CASE
           WHEN generation_number = $2::integer
                AND domain_name = 'container_image_identity' THEN 1
           ELSE 0
       END,
       CASE
           WHEN generation_number = $2::integer
                AND domain_name = 'container_image_identity' THEN $3::text
           ELSE NULL
       END,
       CASE WHEN generation_number = $2::integer
                 AND domain_name = 'container_image_identity'
            THEN clock_timestamp() + INTERVAL '1 minute' ELSE NULL END,
       jsonb_build_object(
           'image_ref', 'registry.example.com/team/api:prod',
           'digest', 'sha256:' || repeat(md5(scope_number::text || domain_name), 2)
       ),
       clock_timestamp(), clock_timestamp(),
       CASE WHEN generation_number = $2::integer
                 AND domain_name = 'container_image_identity' THEN 1 ELSE 0 END
FROM generate_series(1, $1) AS scope_number
CROSS JOIN generate_series(1, $2) AS generation_number
CROSS JOIN unnest(ARRAY[
    'container_image_identity',
    'ci_cd_run_correlation',
    'supply_chain_impact'
]) AS domain_name
`, scopeCount, generationsEach, leaseOwner); err != nil {
		t.Fatalf("seed scale work items: %v", err)
	}
}

func ackCrossScopeCompletionScaleDomain(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	queue ReducerQueue,
	domain reducer.Domain,
	itemCount int,
	batchCount int,
	claimedAt time.Time,
) []time.Duration {
	t.Helper()
	intents := make([]reducer.Intent, itemCount)
	for index := range itemCount {
		intents[index] = reducer.Intent{
			IntentID:  fmt.Sprintf("reducer_5740_scale_%s_%d", domain, index+1),
			Domain:    domain,
			ClaimedAt: &claimedAt,
		}
		if domain == reducer.DomainContainerImageIdentity {
			intents[index].ClaimEpoch = 2
		}
	}
	durations := make([]time.Duration, 0, batchCount)
	for batch := range batchCount {
		start := batch * itemCount / batchCount
		end := (batch + 1) * itemCount / batchCount
		before := time.Now()
		if err := queue.AckBatch(ctx, intents[start:end], nil); err != nil {
			t.Fatalf("ack scale %s batch %d: %v", domain, batch, err)
		}
		durations = append(durations, time.Since(before))
	}
	var succeeded int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
 AND scope.active_generation_id = work.generation_id
WHERE work.domain = $1 AND work.status = 'succeeded'
`, domain).Scan(&succeeded); err != nil {
		t.Fatalf("count scale %s ACKs: %v", domain, err)
	}
	if succeeded != itemCount {
		t.Fatalf("scale %s succeeded=%d, want %d", domain, succeeded, itemCount)
	}
	return durations
}

func claimCrossScopeCompletionScaleDomain(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	domain reducer.Domain,
	leaseOwner string,
	claimedAt time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running', lease_owner = $2,
    claim_until = clock_timestamp() + INTERVAL '1 minute',
    last_attempt_at = $3,
    updated_at = clock_timestamp()
WHERE domain = $1 AND status = 'pending'
`, domain, leaseOwner, claimedAt); err != nil {
		t.Fatalf("claim scale %s: %v", domain, err)
	}
}

func assertCrossScopeCompletionEventItems(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	domain reducer.Domain,
	wantEvents int,
	wantItems int,
) {
	t.Helper()
	var events, items int
	if err := db.QueryRowContext(ctx, `
SELECT count(*), COALESCE(sum(producer_item_count), 0)
FROM cross_scope_completion_events
WHERE producer_domain = $1
`, domain).Scan(&events, &items); err != nil {
		t.Fatalf("read scale %s completion events: %v", domain, err)
	}
	if events != wantEvents || items != wantItems {
		t.Fatalf("scale %s events/items=%d/%d, want %d/%d", domain, events, items, wantEvents, wantItems)
	}
}

func assertCrossScopeCompletionScaleTerminal(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeCount int,
	generationsEach int,
) {
	t.Helper()
	var rows, succeeded, dirty, events int
	// #7494: scope the terminal count to the scale fixture's own scopes.
	// The production bootstrap seeds its own fact_work_items rows
	// (migration 115's global value-flow refresh), which an unscoped
	// count would sweep in.
	if err := db.QueryRowContext(ctx, `
SELECT count(*), count(*) FILTER (WHERE status = 'succeeded'),
       count(*) FILTER (WHERE cross_scope_replay_required)
FROM fact_work_items
WHERE scope_id LIKE 'repository:5740-scale-%'
`).Scan(&rows, &succeeded, &dirty); err != nil {
		t.Fatalf("read scale terminal work items: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM cross_scope_completion_events`).Scan(&events); err != nil {
		t.Fatalf("read scale terminal events: %v", err)
	}
	wantRows := 3 * scopeCount * generationsEach
	if rows != wantRows || succeeded != rows || dirty != 0 || events != 0 {
		t.Fatalf("scale terminal rows=%d succeeded=%d dirty=%d events=%d", rows, succeeded, dirty, events)
	}
}

func readCrossScopeCompletionWALPosition(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var lsn string
	if err := db.QueryRowContext(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&lsn); err != nil {
		t.Fatalf("read scale WAL position: %v", err)
	}
	return lsn
}

func crossScopeCompletionWALBytes(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	start string,
	end string,
) int64 {
	t.Helper()
	var bytes int64
	if err := db.QueryRowContext(
		ctx,
		`SELECT pg_wal_lsn_diff($2::pg_lsn, $1::pg_lsn)::bigint`,
		start,
		end,
	).Scan(&bytes); err != nil {
		t.Fatalf("read scale WAL bytes: %v", err)
	}
	return bytes
}

func crossScopeCompletionDurationP95(values []time.Duration) time.Duration {
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[(len(ordered)*95+99)/100-1]
}
