// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10873 before/after proof for the unchanged-row guard on disposable PostgreSQL: the same 1000-row corpus replayed through the frozen pre-change statement shape adds n_tup_upd +1000 with zero HOT while the guarded production path adds zero updates, and the changed-batch benchmark bounds the payload-compare cost; about 93s locally with pg_stat settling waits, wall time logged not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

// Before/after cost proof for the issue #10873 unchanged-row guard.
//
// TestUnchangedSkipGuardBeforeAfter replays the same corpus through the
// pre-change (unguarded) statement shape and the guarded production path and
// reports pg_stat deltas for each: the unguarded replay must rewrite every
// row, the guarded replay must write nothing. BenchmarkUpsertFactBatchGuard
// times fully changed batches under both shapes to bound the payload-compare
// cost of the guard.
//
// Live-Postgres only; skipped without ESHU_POSTGRES_DSN.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// factGuardUnguardedSuffixForBench is the pre-#10873 upsertFactBatchSuffix,
// frozen for measurement only. It takes the fencing predicate (#4444) without
// the unchanged-row guard, so the before/after comparison below runs the same
// corpus through both shapes. It must never ship: production reads
// upsertFactBatchSuffix.
const factGuardUnguardedSuffixForBench = `
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind = EXCLUDED.fact_kind,
    stable_fact_key = EXCLUDED.stable_fact_key,
    schema_version = EXCLUDED.schema_version,
    collector_kind = EXCLUDED.collector_kind,
    fencing_token = EXCLUDED.fencing_token,
    source_confidence = EXCLUDED.source_confidence,
    source_system = EXCLUDED.source_system,
    source_fact_key = EXCLUDED.source_fact_key,
    source_uri = EXCLUDED.source_uri,
    source_record_id = EXCLUDED.source_record_id,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    is_tombstone = EXCLUDED.is_tombstone,
    payload = EXCLUDED.payload
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token
`

func execUnguardedFactBatch(t testing.TB, ctx context.Context, store SQLDB, batch []facts.Envelope) {
	t.Helper()
	query, args, err := buildUpsertFactBatchQuery(factGuardUnguardedSuffixForBench, batch)
	if err != nil {
		t.Fatalf("build unguarded batch query: %v", err)
	}
	if _, err := store.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec unguarded batch: %v", err)
	}
}

// TestUnchangedSkipGuardBeforeAfter replays one corpus through both statement
// shapes: the unguarded shape rewrites all N rows (the waste being removed),
// the guarded shape writes none.
func TestUnchangedSkipGuardBeforeAfter(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-bench", "gen-bench", now)
	schema := schemaOf(t, ctx, db)

	const corpus = 1000
	first := make([]facts.Envelope, 0, corpus)
	for i := 0; i < corpus; i++ {
		first = append(first, unchangedSkipEnvelope(factIDForCounter(i), "scope-bench", "gen-bench", 10, now))
	}
	if err := upsertFacts(ctx, store, first); err != nil {
		t.Fatalf("upsertFacts(first) error = %v, want nil", err)
	}
	_, updSeeded, _ := waitForStatCounters(t, ctx, db, schema, 2*time.Minute, func(ins, _ int64) bool {
		return ins >= corpus
	})

	// BEFORE: the unguarded shape rewrites every identical row.
	before := make([]facts.Envelope, 0, corpus)
	for i := 0; i < corpus; i++ {
		before = append(before, unchangedSkipEnvelope(factIDForCounter(i), "scope-bench", "gen-bench", 10, now.Add(time.Hour)))
	}
	for i := 0; i < len(before); i += factBatchSize {
		end := i + factBatchSize
		if end > len(before) {
			end = len(before)
		}
		execUnguardedFactBatch(t, ctx, store, before[i:end])
	}
	_, updBefore, hotBefore := waitForStatCounters(t, ctx, db, schema, 2*time.Minute, func(_, upd int64) bool {
		return upd-updSeeded >= corpus
	})
	t.Logf("BEFORE (unguarded) identical replay of %d facts: n_tup_upd delta=%d n_tup_hot_upd=%d", corpus, updBefore-updSeeded, hotBefore)
	if updBefore-updSeeded != corpus {
		t.Fatalf("unguarded replay added %d updates, want %d", updBefore-updSeeded, corpus)
	}

	// AFTER: the guarded shape skips the same replay (timestamps bumped again
	// so only the timestamps differ).
	after := make([]facts.Envelope, 0, corpus)
	for i := 0; i < corpus; i++ {
		after = append(after, unchangedSkipEnvelope(factIDForCounter(i), "scope-bench", "gen-bench", 10, now.Add(2*time.Hour)))
	}
	if err := upsertFacts(ctx, store, after); err != nil {
		t.Fatalf("upsertFacts(after) error = %v, want nil", err)
	}
	_, updAfter, hotAfter := waitForStatCounters(t, ctx, db, schema, 90*time.Second, func(_, upd int64) bool {
		return upd != updBefore
	})
	t.Logf("AFTER (guarded) identical replay of %d facts: n_tup_upd delta=%d n_tup_hot_upd=%d", corpus, updAfter-updBefore, hotAfter)
	if updAfter != updBefore {
		t.Fatalf("guarded replay added %d updates, want 0", updAfter-updBefore)
	}
}

// BenchmarkUpsertFactBatchGuard times fully changed 500-row batches under the
// guarded and unguarded shapes. Every iteration flips the payload version so
// both shapes write every row: the delta isolates the guard's compare cost.
func BenchmarkUpsertFactBatchGuard(b *testing.B) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		b.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(b, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(b, ctx, db, "scope-benchcost", "gen-benchcost", now)

	const corpus = 500
	seed := make([]facts.Envelope, 0, corpus)
	for i := 0; i < corpus; i++ {
		seed = append(seed, unchangedSkipEnvelope(factIDForCounter(i), "scope-benchcost", "gen-benchcost", 10, now))
	}
	if err := upsertFacts(ctx, store, seed); err != nil {
		b.Fatalf("upsertFacts(seed): %v", err)
	}

	// Every iteration flips the payload parity bit so both shapes write
	// every row on every iteration: the timing delta isolates the guard's
	// compare cost instead of comparing a skip against a write.
	mkChanged := func(tag string, i int) []facts.Envelope {
		out := make([]facts.Envelope, 0, corpus)
		for j := 0; j < corpus; j++ {
			e := unchangedSkipEnvelope(factIDForCounter(j), "scope-benchcost", "gen-benchcost", 10, now.Add(time.Duration(i)*time.Second))
			e.Payload = map[string]any{"version": fmt.Sprintf("%s-%d", tag, i%2)}
			out = append(out, e)
		}
		return out
	}

	b.Run("guarded", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := upsertFactBatch(ctx, store, mkChanged("guarded", i)); err != nil {
				b.Fatalf("upsertFactBatch: %v", err)
			}
		}
	})
	b.Run("unguarded", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			execUnguardedFactBatch(b, ctx, store, mkChanged("unguarded", i))
		}
	})
}
