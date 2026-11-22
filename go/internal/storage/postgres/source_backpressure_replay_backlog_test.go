// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build replayseed

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReplaySeedBacklog plants the reducer backlog of the replay: the #10862
// mix (8,713 rows, seed source "replay") plus source A's aged claimable rows at
// the 10x shape. A's rows sit in replayDomainA with one conflict key each, so a
// single ClaimBatch can take a whole chunk, and are aged between the engage age
// plus 10 minutes and the engage age plus 2 hours: every one is past the engage
// age and, until the catch-up step completes them, past the release age too.
func TestReplaySeedBacklog(t *testing.T) {
	database := replayDB(t)
	src := replaySourceFromEnv(t, "A")
	rows := replayEnvInt(t, "ESHU_REPLAY_A_ROWS", 13000)
	engageAge := replayEnvDuration(t, "ESHU_REPLAY_ENGAGE_AGE", time.Hour)
	ctx := context.Background()
	now := time.Now().UTC()

	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open replay connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if n := seedReducerQueue10862Replay(t, ctx, conn, "public", now); n != 8713 {
		t.Fatalf("seeded mix rows = %d, want 8713", n)
	}

	const scopeCount = 100
	oldest := now.Add(-engageAge - 2*time.Hour)
	if _, err := conn.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id, collector_kind,
  partition_key, status, active_generation_id, payload, observed_at, ingested_at)
SELECT 'replay-areduce-' || s, 'repository', $1, 'replay-areduce-' || s, NULL, $2, 'replay', 'active',
  'replay-areduce-g-' || s, '{}'::jsonb, $3::timestamptz, $3::timestamptz
FROM generate_series(1, $4::int) AS s`, src.System, string(src.Collector), oldest, scopeCount); err != nil {
		t.Fatalf("insert source A reducer scopes: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'replay-areduce-g-' || s, 'replay-areduce-' || s, 'snapshot', $1::timestamptz, $1::timestamptz, 'active',
  $1::timestamptz, '{}'::jsonb
FROM generate_series(1, $2::int) AS s`, oldest, scopeCount); err != nil {
		t.Fatalf("insert source A reducer generations: %v", err)
	}
	// Row i of n is created at engage+10m .. engage+2h ago, evenly spread.
	span := 110 * time.Minute
	if os.Getenv("ESHU_REPLAY_A_SHAPE") == "notakeable" {
		seedNotTakeableA(ctx, t, conn, src, rows, scopeCount, now.Add(-engageAge-10*time.Minute), span)
		return
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, visible_at, payload, created_at, updated_at)
SELECT 'replay-a-work-' || i, 'replay-areduce-' || (1 + i % $5::int), 'replay-areduce-g-' || (1 + i % $5::int),
  'reducer', $1, 'replay-a', 'replay-a-key-' || i, 'pending', 0, NULL,
  jsonb_build_object('entity_key', 'replay-a-entity-' || i, 'reason', '10882-replay',
    'fact_id', 'replay-a-fact-' || i, 'source_system', $2::text),
  $3::timestamptz - ($4::bigint * (i::float8 / $6::float8) * interval '1 microsecond'),
  $3::timestamptz - ($4::bigint * (i::float8 / $6::float8) * interval '1 microsecond')
FROM generate_series(0, $6::int - 1) AS i`,
		string(replayDomainA), src.System, now.Add(-engageAge-10*time.Minute), span.Microseconds(), scopeCount, rows); err != nil {
		t.Fatalf("insert source A reducer rows: %v", err)
	}
	t.Logf("replay seed: mix 8713 rows + %d aged source A rows (%s)", rows, src.System)
}

// TestReplayCompleteA is the catch-up step (arbiter ruling F1): it completes
// source A's aged reducer rows through the production queue completion call,
// ReducerQueue.ClaimBatch then AckBatch, restricted to replayDomainA. Chunk size
// and interval are fixed per replay and identical in every arm. Every claimed row
// must be source A's seeded row; anything else is a harness bug and fails.
func TestReplayCompleteA(t *testing.T) {
	database := replayDB(t)
	src := replaySourceFromEnv(t, "A")
	chunk := replayEnvInt(t, "ESHU_REPLAY_COMPLETE_CHUNK", 500)
	every := replayEnvDuration(t, "ESHU_REPLAY_COMPLETE_EVERY", time.Second)
	ctx := context.Background()

	queue := NewReducerQueue(SQLDB{DB: database}, "replay-catchup", 5*time.Minute)
	queue.ClaimDomains = []reducercontract.Domain{replayDomainA}
	completed := 0
	for {
		intents, err := queue.ClaimBatch(ctx, chunk)
		if err != nil {
			t.Fatalf("ClaimBatch: %v", err)
		}
		if len(intents) == 0 {
			break
		}
		results := make([]reducercontract.Result, 0, len(intents))
		for _, intent := range intents {
			if intent.Domain != replayDomainA || intent.SourceSystem != src.System {
				t.Fatalf("claimed %s (domain %s, source %q), want only seeded source %q rows in %s",
					intent.IntentID, intent.Domain, intent.SourceSystem, src.System, replayDomainA)
			}
			results = append(results, reducercontract.Result{
				IntentID: intent.IntentID, Domain: intent.Domain, Status: reducercontract.ResultStatusSucceeded,
				EvidenceSummary: "10882 replay catch-up", CompletedAt: time.Now().UTC(),
			})
		}
		if err := queue.AckBatch(ctx, intents, results); err != nil {
			t.Fatalf("AckBatch: %v", err)
		}
		completed += len(intents)
		time.Sleep(every)
	}
	var left int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND domain = $1 AND status <> 'succeeded'`,
		string(replayDomainA)).Scan(&left); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if left != 0 {
		t.Fatalf("%d source A rows left uncompleted", left)
	}
	fmt.Printf("replay catch-up: completed %d rows for source %s\n", completed, src.System)
}

// seedNotTakeableA is the T3 (#11187) shape of source A's backlog: every aged
// row is one the paced intake cannot clear and the signal must therefore not
// count. Half park on a wait row of their own (retrying, visible, with a
// reducer_waiting_items row: the case the signal's parked-wait test exists
// for) and half are readiness-gated (a requirement domain with no committed
// phase). The latch must never engage on them.
func seedNotTakeableA(ctx context.Context, t *testing.T, conn *sql.Conn, src replaySource, rows, scopeCount int, newest time.Time, span time.Duration) {
	t.Helper()
	half := rows / 2
	if _, err := conn.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, visible_at, payload, created_at, updated_at)
SELECT 'replay-a-work-' || i, 'replay-areduce-' || (1 + i % $5::int), 'replay-areduce-g-' || (1 + i % $5::int),
  'reducer', $1, 'replay-a', 'replay-a-key-' || i, 'retrying', 1, $3::timestamptz,
  jsonb_build_object('entity_key', 'replay-a-entity-' || i, 'reason', '10882-replay',
    'fact_id', 'replay-a-fact-' || i, 'source_system', $2::text),
  $3::timestamptz - ($4::bigint * (i::float8 / $6::float8) * interval '1 microsecond'),
  $3::timestamptz - ($4::bigint * (i::float8 / $6::float8) * interval '1 microsecond')
FROM generate_series(0, $6::int - 1) AS i`,
		string(replayDomainA), src.System, newest, span.Microseconds(), scopeCount, half); err != nil {
		t.Fatalf("insert parked source A rows: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id, domain,
  failure_class, first_deferred_at, updated_at)
SELECT work_item_id, 'phase_committed', jsonb_build_object('k', work_item_id), scope_id, generation_id, domain,
  'reducer_wait', created_at, created_at
FROM fact_work_items WHERE work_item_id LIKE 'replay-a-work-%'`); err != nil {
		t.Fatalf("insert source A wait rows: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, visible_at, payload, created_at, updated_at)
SELECT 'replay-a-work-' || (i + $5::int), 'replay-areduce-' || (1 + i % $4::int), 'replay-areduce-g-' || (1 + i % $4::int),
  'reducer', 'aws_relationship_materialization', 'replay-a', 'replay-a-key-' || (i + $5::int), 'pending', 0, NULL,
  jsonb_build_object('entity_key', 'replay-a-entity-' || i, 'reason', '10882-replay',
    'fact_id', 'replay-a-fact-' || i, 'source_system', $1::text),
  $2::timestamptz - ($3::bigint * (i::float8 / $5::float8) * interval '1 microsecond'),
  $2::timestamptz - ($3::bigint * (i::float8 / $5::float8) * interval '1 microsecond')
FROM generate_series(0, $6::int - 1) AS i`,
		src.System, newest, span.Microseconds(), scopeCount, half, rows-half); err != nil {
		t.Fatalf("insert readiness-gated source A rows: %v", err)
	}
	t.Logf("replay seed: mix 8713 rows + %d not-takeable aged source A rows (%d parked, %d gated)", rows, half, rows-half)
}
