// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10873 fencing, tombstone, streaming-admission, concurrent-replay, and pg_stat write-counter proof for the unchanged-row guard on disposable PostgreSQL: higher-token same-content writes land, lower-token writes never land, tombstone flips always write, unchanged-skipped rows stay admitted for afterBatch derivation, concurrent replays skip without deadlock or misclassification, and an identical replay adds zero pg_stat updates; matrix green locally in about 1s of test time, wall time logged not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

// Fencing, tombstone, admission, concurrency, and write-counter proof for the
// issue #10873 unchanged-fact skip guard. Companions the
// unchanged_skip_proof_test.go change matrix: the guard must keep the
// issue #4444 fencing protection exactly as it is (a higher-token same-content
// write still lands to advance the stored token; a lower-token write never
// lands whatever its content), tombstone flips must always write,
// RETURNING-absent unchanged rows must stay admitted for afterBatch
// derivation, concurrent replays must not deadlock or misclassify, and an
// identical replay must add zero pg_stat updates.
//
// Live-Postgres only; skipped without ESHU_POSTGRES_DSN.

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestUpsertFactBatchUnchangedGuardFencingMatrix keeps the #4444 contract
// pinned under the new guard: token in the compared tuple, higher-token
// same-content still writes, lower-token never writes.
func TestUpsertFactBatchUnchangedGuardFencingMatrix(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-fence", "gen-fence", now)

	const factID = "fact-fence-matrix"
	v5 := unchangedSkipEnvelope(factID, "scope-fence", "gen-fence", 5, now)
	v5.Payload = map[string]any{"version": "X"}
	if err := upsertFactBatch(ctx, store, []facts.Envelope{v5}); err != nil {
		t.Fatalf("upsertFactBatch(v5) error = %v, want nil", err)
	}

	// Higher-token same-content must still land to advance the stored token;
	// otherwise a later intermediate-token different-content writer would
	// overwrite a fact it should lose to.
	v10same := unchangedSkipEnvelope(factID, "scope-fence", "gen-fence", 10, now.Add(time.Hour))
	v10same.Payload = map[string]any{"version": "X"}
	before := readUnchangedSkipRow(t, ctx, db, factID)
	accepted, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{v10same})
	if err != nil {
		t.Fatalf("upsertFactBatchReturningAccepted(v10same) error = %v, want nil", err)
	}
	if _, ok := accepted[factID]; !ok {
		t.Fatalf("higher-token same-content write missing from accepted set")
	}
	if got := readUnchangedSkipRow(t, ctx, db, factID); got.xmin == before.xmin || got.token != 10 {
		t.Fatalf("higher-token same-content did not advance the row: xmin %s -> %s, token %d", before.xmin, got.xmin, got.token)
	}

	// Lower-token different-content stays fenced and excluded.
	v7diff := unchangedSkipEnvelope(factID, "scope-fence", "gen-fence", 7, now.Add(2*time.Hour))
	v7diff.Payload = map[string]any{"version": "Y"}
	before = readUnchangedSkipRow(t, ctx, db, factID)
	accepted, err = upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{v7diff})
	if err != nil {
		t.Fatalf("upsertFactBatchReturningAccepted(v7diff) error = %v, want nil", err)
	}
	if _, ok := accepted[factID]; ok {
		t.Fatalf("lower-token different-content write admitted: fencing regressed")
	}
	if got := readUnchangedSkipRow(t, ctx, db, factID); got.xmin != before.xmin || got.token != 10 {
		t.Fatalf("lower-token different-content rewrote the row: xmin %s -> %s, token %d", before.xmin, got.xmin, got.token)
	}

	// Lower-token same-content stays fenced too: no downgrade, no skip.
	v7same := unchangedSkipEnvelope(factID, "scope-fence", "gen-fence", 7, now.Add(3*time.Hour))
	v7same.Payload = map[string]any{"version": "X"}
	accepted, err = upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{v7same})
	if err != nil {
		t.Fatalf("upsertFactBatchReturningAccepted(v7same) error = %v, want nil", err)
	}
	if _, ok := accepted[factID]; ok {
		t.Fatalf("lower-token same-content write admitted: fencing regressed")
	}
	if got := readUnchangedSkipRow(t, ctx, db, factID); got.token != 10 || got.xmin != before.xmin {
		t.Fatalf("lower-token same-content moved the row: token %d, xmin %s -> %s", got.token, before.xmin, got.xmin)
	}

	// Equal-token different-content keeps the old overwrite semantics.
	v10diff := unchangedSkipEnvelope(factID, "scope-fence", "gen-fence", 10, now.Add(4*time.Hour))
	v10diff.Payload = map[string]any{"version": "Z"}
	accepted, err = upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{v10diff})
	if err != nil {
		t.Fatalf("upsertFactBatchReturningAccepted(v10diff) error = %v, want nil", err)
	}
	if _, ok := accepted[factID]; !ok {
		t.Fatalf("equal-token different-content write missing from accepted set")
	}
	if got := readUnchangedSkipRow(t, ctx, db, factID); got.xmin == before.xmin {
		t.Fatalf("equal-token different-content did not write: xmin stayed %s", before.xmin)
	}
}

// TestUpsertFactBatchUnchangedGuardTombstones proves deletion markers always
// write: is_tombstone is inside the compared tuple, so live->tombstone and
// tombstone->live flips can never skip.
func TestUpsertFactBatchUnchangedGuardTombstones(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-tomb", "gen-tomb", now)

	const factID = "fact-tombstone-1"
	live := unchangedSkipEnvelope(factID, "scope-tomb", "gen-tomb", 10, now)
	if err := upsertFactBatch(ctx, store, []facts.Envelope{live}); err != nil {
		t.Fatalf("upsertFactBatch(live) error = %v, want nil", err)
	}
	stored := readUnchangedSkipRow(t, ctx, db, factID)

	retire := live.Clone()
	retire.ObservedAt = now.Add(time.Hour)
	retire.IsTombstone = true
	if _, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{retire}); err != nil {
		t.Fatalf("upsertFactBatchReturningAccepted(retire) error = %v, want nil", err)
	}
	if got := readUnchangedSkipRow(t, ctx, db, factID); !got.tombstone || got.xmin == stored.xmin {
		t.Fatalf("live->tombstone flip did not write: tombstone=%v xmin %s -> %s", got.tombstone, stored.xmin, got.xmin)
	}

	revive := retire.Clone()
	revive.ObservedAt = now.Add(2 * time.Hour)
	revive.IsTombstone = false
	before := readUnchangedSkipRow(t, ctx, db, factID)
	accepted, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{revive})
	if err != nil {
		t.Fatalf("upsertFactBatchReturningAccepted(revive) error = %v, want nil", err)
	}
	if _, ok := accepted[factID]; !ok {
		t.Fatalf("tombstone->live flip missing from accepted set")
	}
	if got := readUnchangedSkipRow(t, ctx, db, factID); got.tombstone || got.xmin == before.xmin {
		t.Fatalf("tombstone->live flip did not write: tombstone=%v xmin %s -> %s", got.tombstone, before.xmin, got.xmin)
	}
}

// TestUpsertStreamingFactsAdmitsAllSkippedReplay drives the production
// streaming path end to end: an identical replay must reach afterBatch with
// the full batch (filtered == attempted, the advisory-completeness seam in
// ingestion.go) while writing nothing.
func TestUpsertStreamingFactsAdmitsAllSkippedReplay(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-stream", "gen-stream", now)

	feed := func(envelopes []facts.Envelope) <-chan facts.Envelope {
		ch := make(chan facts.Envelope, len(envelopes))
		for _, e := range envelopes {
			ch <- e
		}
		close(ch)
		return ch
	}

	var mu sync.Mutex
	var got, attempted []int
	afterBatch := func(batch []facts.Envelope, n int) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, len(batch))
		attempted = append(attempted, n)
		return nil
	}

	original := []facts.Envelope{
		unchangedSkipEnvelope("fact-stream-1", "scope-stream", "gen-stream", 10, now),
		unchangedSkipEnvelope("fact-stream-2", "scope-stream", "gen-stream", 10, now),
		unchangedSkipEnvelope("fact-stream-3", "scope-stream", "gen-stream", 10, now),
	}
	if _, err := upsertStreamingFacts(ctx, store, feed(original), "scope-stream", "gen-stream", afterBatch); err != nil {
		t.Fatalf("upsertStreamingFacts(first) error = %v, want nil", err)
	}
	before1 := readUnchangedSkipRow(t, ctx, db, "fact-stream-1")

	replay := []facts.Envelope{
		unchangedSkipEnvelope("fact-stream-1", "scope-stream", "gen-stream", 10, now.Add(time.Hour)),
		unchangedSkipEnvelope("fact-stream-2", "scope-stream", "gen-stream", 10, now.Add(time.Hour)),
		unchangedSkipEnvelope("fact-stream-3", "scope-stream", "gen-stream", 10, now.Add(time.Hour)),
	}
	if _, err := upsertStreamingFacts(ctx, store, feed(replay), "scope-stream", "gen-stream", afterBatch); err != nil {
		t.Fatalf("upsertStreamingFacts(replay) error = %v, want nil", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != 3 || got[1] != 3 || attempted[0] != 3 || attempted[1] != 3 {
		t.Fatalf("afterBatch saw got=%v attempted=%v, want [3 3] and [3 3]: skipped replay must stay admitted", got, attempted)
	}
	if after := readUnchangedSkipRow(t, ctx, db, "fact-stream-1"); after.xmin != before1.xmin {
		t.Fatalf("streaming replay rewrote the row: xmin %s -> %s", before1.xmin, after.xmin)
	}
}

// TestUpsertFactBatchUnchangedGuardConcurrentReplay interleaves two sessions
// on the EvalPlanQual path: session A commits a changed row while session B's
// identical replay is blocked on the row lock. B must unblock, skip (not
// rewrite), report accepted, and leave A's row intact — no deadlock, no lost
// update, no stale admission.
func TestUpsertFactBatchUnchangedGuardConcurrentReplay(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-race", "gen-race", now)

	// The opener pins one connection; the race needs two sessions plus the
	// driver, so open a second handle on the same database.
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open second postgres handle: %v", err)
	}
	defer func() { _ = db2.Close() }()

	const factID = "fact-race-1"
	seed := unchangedSkipEnvelope(factID, "scope-race", "gen-race", 10, now)
	if err := upsertFactBatch(ctx, SQLDB{DB: db}, []facts.Envelope{seed}); err != nil {
		t.Fatalf("upsertFactBatch(seed) error = %v, want nil", err)
	}

	schema := schemaOf(t, ctx, db)
	beginTx := func(db *sql.DB) *sql.Tx {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+schema); err != nil {
			t.Fatalf("set search_path: %v", err)
		}
		return tx
	}

	changed := seed.Clone()
	changed.ObservedAt = now.Add(time.Hour)
	changed.Payload = map[string]any{"version": "v2"}

	txA := beginTx(db)
	if _, err := upsertFactBatchReturningAccepted(ctx, SQLTx{Tx: txA}, []facts.Envelope{changed}); err != nil {
		t.Fatalf("txA upsert error = %v, want nil", err)
	}

	replayV2 := changed.Clone()
	replayV2.ObservedAt = now.Add(2 * time.Hour)
	txB := beginTx(db2)
	type result struct {
		accepted map[string]struct{}
		err      error
	}
	done := make(chan result, 1)
	go func() {
		accepted, err := upsertFactBatchReturningAccepted(ctx, SQLTx{Tx: txB}, []facts.Envelope{replayV2})
		done <- result{accepted: accepted, err: err}
	}()

	// Let B block on A's row lock, then commit A: B's ON CONFLICT recheck
	// (EvalPlanQual) must see A's committed v2, skip, and admit.
	time.Sleep(500 * time.Millisecond)
	if err := txA.Commit(); err != nil {
		t.Fatalf("txA commit error = %v, want nil", err)
	}
	afterA := readUnchangedSkipRow(t, ctx, db, factID)

	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		t.Fatalf("session B did not unblock after A committed: suspected deadlock")
	}
	if res.err != nil {
		t.Fatalf("txB upsert error = %v, want nil", res.err)
	}
	if _, ok := res.accepted[factID]; !ok {
		t.Fatalf("blocked identical replay missing from accepted set")
	}
	if err := txB.Commit(); err != nil {
		t.Fatalf("txB commit error = %v, want nil", err)
	}
	if got := readUnchangedSkipRow(t, ctx, db, factID); got.xmin != afterA.xmin {
		t.Fatalf("blocked replay rewrote A's row: xmin %s -> %s", afterA.xmin, got.xmin)
	} else if !containsPayloadVersion(t, ctx, db, factID, "v2") {
		t.Fatalf("blocked replay lost A's committed payload")
	}
}

// schemaOf reports the search_path schema the opener pinned.
func schemaOf(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	return schema
}

func containsPayloadVersion(t *testing.T, ctx context.Context, db *sql.DB, factID, version string) bool {
	t.Helper()
	var payload string
	if err := db.QueryRowContext(
		ctx, `SELECT payload::text FROM fact_records WHERE fact_id = $1`, factID,
	).Scan(&payload); err != nil {
		t.Fatalf("read payload %q: %v", factID, err)
	}
	return strings.Contains(payload, `"`+version+`"`)
}

// factStatCounters reads this backend's insert/update counters for
// fact_records.
func factStatCounters(t *testing.T, ctx context.Context, db *sql.DB, schema string) (ins, upd, hot int64) {
	t.Helper()
	if err := db.QueryRowContext(
		ctx,
		`SELECT n_tup_ins, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relid = $1::regclass`,
		schema+".fact_records",
	).Scan(&ins, &upd, &hot); err != nil {
		t.Fatalf("read pg_stat counters: %v", err)
	}
	return ins, upd, hot
}

// waitForStatCounters polls pg_stat_user_tables until cond holds or the
// deadline passes. Cumulative-stats visibility can lag a burst-DDL session by
// tens of seconds (observed on the local PG18 instance), so stats assertions
// must poll, never read once. Returns the last reading.
func waitForStatCounters(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	schema string,
	timeout time.Duration,
	cond func(ins, upd int64) bool,
) (ins, upd, hot int64) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ins, upd, hot = factStatCounters(t, ctx, db, schema)
		if cond(ins, upd) {
			return ins, upd, hot
		}
		if time.Now().After(deadline) {
			return ins, upd, hot
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestUpsertFactsUnchangedReplayAddsNoUpdates is the issue's headline
// acceptance probe on a production-shaped corpus: 2,000 unchanged facts
// replayed through the shared upsertFacts path must add zero pg_stat updates
// (and zero HOT updates), while a fully changed batch still lands every row.
func TestUpsertFactsUnchangedReplayAddsNoUpdates(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-counters", "gen-counters", now)
	schema := schemaOf(t, ctx, db)

	const corpus = 2000
	first := make([]facts.Envelope, 0, corpus)
	for i := 0; i < corpus; i++ {
		e := unchangedSkipEnvelope(factIDForCounter(i), "scope-counters", "gen-counters", 10, now)
		first = append(first, e)
	}
	if err := upsertFacts(ctx, store, first); err != nil {
		t.Fatalf("upsertFacts(first) error = %v, want nil", err)
	}
	// Wait for the inserts to surface in pg_stat: this proves the stats path
	// is flowing for this table before any zero-delta assertion below.
	_, updBefore, _ := waitForStatCounters(t, ctx, db, schema, 2*time.Minute, func(ins, _ int64) bool {
		return ins >= corpus
	})
	t.Logf("seeded %d facts: settled n_tup_upd=%d", corpus, updBefore)

	replay := make([]facts.Envelope, 0, corpus)
	for i := 0; i < corpus; i++ {
		e := unchangedSkipEnvelope(factIDForCounter(i), "scope-counters", "gen-counters", 10, now.Add(time.Hour))
		replay = append(replay, e)
	}
	if err := upsertFacts(ctx, store, replay); err != nil {
		t.Fatalf("upsertFacts(replay) error = %v, want nil", err)
	}
	// The replay must add no update even after the stats lag window: poll for
	// 90s and fail fast on any movement. (xmin assertions in the matrix tests
	// already prove the row versions do not move; this is the pg_stat
	// acceptance corroboration.)
	_, updAfter, hotAfter := waitForStatCounters(t, ctx, db, schema, 90*time.Second, func(_, upd int64) bool {
		return upd != updBefore
	})
	t.Logf("unchanged replay of %d facts: n_tup_upd delta=%d n_tup_hot_upd=%d", corpus, updAfter-updBefore, hotAfter)
	if updAfter != updBefore {
		t.Fatalf("unchanged replay added %d pg_stat updates, want 0", updAfter-updBefore)
	}

	changed := make([]facts.Envelope, 0, corpus)
	for i := 0; i < corpus; i++ {
		e := unchangedSkipEnvelope(factIDForCounter(i), "scope-counters", "gen-counters", 10, now.Add(2*time.Hour))
		e.Payload = map[string]any{"version": "v2"}
		changed = append(changed, e)
	}
	if err := upsertFacts(ctx, store, changed); err != nil {
		t.Fatalf("upsertFacts(changed) error = %v, want nil", err)
	}
	_, updChanged, _ := waitForStatCounters(t, ctx, db, schema, 2*time.Minute, func(_, upd int64) bool {
		return upd-updAfter >= corpus
	})
	t.Logf("fully changed batch of %d facts: n_tup_upd delta=%d", corpus, updChanged-updAfter)
	if updChanged-updAfter != corpus {
		t.Fatalf("changed batch added %d pg_stat updates, want %d", updChanged-updAfter, corpus)
	}
}

func factIDForCounter(i int) string {
	return "fact-counter-" + strconv.Itoa(i)
}
