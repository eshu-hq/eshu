// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeConfigStateDriftRedriveRow is one in-memory ledger row. The zero value
// matches a freshly INSERTed row (attempt_count 0).
type fakeConfigStateDriftRedriveRow struct {
	attemptCount  int
	nextAttemptAt time.Time
}

// fakeConfigStateDriftRedriveDB is a stateful, in-memory reimplementation of
// the three queries drift_runtime_redrive.go issues
// (ensureConfigStateDriftRedriveScheduledQuery,
// claimAndAdvanceConfigStateDriftRedrivesQuery,
// claimAndDeleteExhaustedConfigStateDriftRedrivesQuery), used to prove
// ConfigStateDriftRedriveStore's CONVERGENCE and BOUNDED-GROWTH properties
// (issue #5593 P1-1, P1-B) cheaply and quickly. The underlying SQL's actual
// claim/advance/delete semantics -- including FOR UPDATE SKIP LOCKED's
// concurrent-claim safety -- are proven separately against a real Postgres
// in drift_runtime_redrive_live_test.go
// (TestConfigStateDriftRedriveClaimDueDoesNotDoubleClaimConcurrentlyLive);
// this fake exists to make the logic-level regression tests below fast and
// hermetic, not to stand in for that live proof.
type fakeConfigStateDriftRedriveDB struct {
	rows map[[2]string]*fakeConfigStateDriftRedriveRow
}

func (f *fakeConfigStateDriftRedriveDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if !strings.Contains(query, "INSERT INTO config_state_drift_redrive") {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: unexpected exec query: %s", query)
	}
	scopeID, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[0] not a string: %#v", args[0])
	}
	generationID, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[1] not a string: %#v", args[1])
	}
	firstAttemptAt, ok := args[2].(time.Time)
	if !ok {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[2] not a time.Time: %#v", args[2])
	}

	if f.rows == nil {
		f.rows = map[[2]string]*fakeConfigStateDriftRedriveRow{}
	}
	key := [2]string{scopeID, generationID}
	if _, exists := f.rows[key]; exists {
		// ON CONFLICT (scope_id, generation_id) DO NOTHING.
		return driverResult{}, nil
	}
	f.rows[key] = &fakeConfigStateDriftRedriveRow{attemptCount: 0, nextAttemptAt: firstAttemptAt}
	return driverResult{}, nil
}

func (f *fakeConfigStateDriftRedriveDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "UPDATE config_state_drift_redrive"):
		return f.queryClaimAndAdvance(args)
	case strings.Contains(query, "DELETE FROM config_state_drift_redrive"):
		return f.queryClaimAndDeleteExhausted(args)
	default:
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: unexpected query: %s", query)
	}
}

// queryClaimAndAdvance mirrors claimAndAdvanceConfigStateDriftRedrivesQuery:
// due rows (next_attempt_at <= now) whose NEXT attempt stays under the
// bound (attempt_count < maxAttempts-1) are advanced in place.
func (f *fakeConfigStateDriftRedriveDB) queryClaimAndAdvance(args []any) (Rows, error) {
	now, maxAttempts, limit, nextAttemptAt, err := parseClaimArgs(args)
	if err != nil {
		return nil, err
	}
	due := f.dueKeys(now, func(row *fakeConfigStateDriftRedriveRow) bool {
		return row.attemptCount < maxAttempts-1
	})
	if len(due) > limit {
		due = due[:limit]
	}
	claims := make([]ConfigStateDriftRedriveClaim, 0, len(due))
	for _, k := range due {
		row := f.rows[k.key]
		row.attemptCount++
		row.nextAttemptAt = nextAttemptAt
		claims = append(claims, ConfigStateDriftRedriveClaim{ScopeID: k.key[0], GenerationID: k.key[1], AttemptCount: row.attemptCount})
	}
	return &fakeConfigStateDriftRedriveRows{claims: claims}, nil
}

// queryClaimAndDeleteExhausted mirrors
// claimAndDeleteExhaustedConfigStateDriftRedrivesQuery: due rows whose
// attempt_count already equals maxAttempts-1 (this claim is their LAST) are
// claimed and DELETED from the map entirely.
func (f *fakeConfigStateDriftRedriveDB) queryClaimAndDeleteExhausted(args []any) (Rows, error) {
	now, ok := args[0].(time.Time)
	if !ok {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[0] not a time.Time: %#v", args[0])
	}
	maxAttempts, ok := args[1].(int)
	if !ok {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[1] not an int: %#v", args[1])
	}
	limit, ok := args[2].(int)
	if !ok {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[2] not an int: %#v", args[2])
	}
	due := f.dueKeys(now, func(row *fakeConfigStateDriftRedriveRow) bool {
		return row.attemptCount == maxAttempts-1
	})
	if len(due) > limit {
		due = due[:limit]
	}
	claims := make([]ConfigStateDriftRedriveClaim, 0, len(due))
	for _, k := range due {
		row := f.rows[k.key]
		claims = append(claims, ConfigStateDriftRedriveClaim{ScopeID: k.key[0], GenerationID: k.key[1], AttemptCount: row.attemptCount + 1, Exhausted: true})
		delete(f.rows, k.key)
	}
	return &fakeConfigStateDriftRedriveRows{claims: claims}, nil
}

func parseClaimArgs(args []any) (now time.Time, maxAttempts int, limit int, nextAttemptAt time.Time, err error) {
	now, ok := args[0].(time.Time)
	if !ok {
		return now, 0, 0, nextAttemptAt, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[0] not a time.Time: %#v", args[0])
	}
	maxAttempts, ok = args[1].(int)
	if !ok {
		return now, 0, 0, nextAttemptAt, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[1] not an int: %#v", args[1])
	}
	limit, ok = args[2].(int)
	if !ok {
		return now, 0, 0, nextAttemptAt, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[2] not an int: %#v", args[2])
	}
	nextAttemptAt, ok = args[3].(time.Time)
	if !ok {
		return now, 0, 0, nextAttemptAt, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[3] not a time.Time: %#v", args[3])
	}
	return now, maxAttempts, limit, nextAttemptAt, nil
}

type fakeConfigStateDriftRedriveDueKey struct {
	key           [2]string
	nextAttemptAt time.Time
}

// dueKeys returns keys whose row is due (next_attempt_at <= now) and matches
// predicate, ORDERed by next_attempt_at ASC (tie-broken by key) to mirror
// the real query's ORDER BY.
func (f *fakeConfigStateDriftRedriveDB) dueKeys(now time.Time, predicate func(*fakeConfigStateDriftRedriveRow) bool) []fakeConfigStateDriftRedriveDueKey {
	var due []fakeConfigStateDriftRedriveDueKey
	for key, row := range f.rows {
		if !row.nextAttemptAt.After(now) && predicate(row) {
			due = append(due, fakeConfigStateDriftRedriveDueKey{key: key, nextAttemptAt: row.nextAttemptAt})
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if !due[i].nextAttemptAt.Equal(due[j].nextAttemptAt) {
			return due[i].nextAttemptAt.Before(due[j].nextAttemptAt)
		}
		return due[i].key[0]+due[i].key[1] < due[j].key[0]+due[j].key[1]
	})
	return due
}

type fakeConfigStateDriftRedriveRows struct {
	claims []ConfigStateDriftRedriveClaim
	idx    int
}

func (r *fakeConfigStateDriftRedriveRows) Next() bool {
	return r.idx < len(r.claims)
}

func (r *fakeConfigStateDriftRedriveRows) Scan(dest ...any) error {
	if r.idx >= len(r.claims) {
		return fmt.Errorf("fakeConfigStateDriftRedriveRows: Scan called past last row")
	}
	claim := r.claims[r.idx]
	r.idx++
	*(dest[0].(*string)) = claim.ScopeID
	*(dest[1].(*string)) = claim.GenerationID
	*(dest[2].(*int)) = claim.AttemptCount
	return nil
}

func (r *fakeConfigStateDriftRedriveRows) Err() error   { return nil }
func (r *fakeConfigStateDriftRedriveRows) Close() error { return nil }

func TestConfigStateDriftRedriveStoreEnsureScheduledIsIdempotent(t *testing.T) {
	t.Parallel()

	db := &fakeConfigStateDriftRedriveDB{}
	store := NewConfigStateDriftRedriveStore(db)
	ctx := context.Background()
	first := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	later := first.Add(time.Hour)

	if err := store.EnsureScheduled(ctx, "state_snapshot:s3:hash-1", "gen-1", first); err != nil {
		t.Fatalf("EnsureScheduled() first call error = %v, want nil", err)
	}
	// A second call for the SAME key with a LATER firstAttemptAt must be a
	// no-op: the ledger row's original schedule wins (ON CONFLICT DO
	// NOTHING), so a retried Ack or duplicate caller cannot push a pending
	// redrive further out.
	if err := store.EnsureScheduled(ctx, "state_snapshot:s3:hash-1", "gen-1", later); err != nil {
		t.Fatalf("EnsureScheduled() second call error = %v, want nil", err)
	}

	if got, want := len(db.rows), 1; got != want {
		t.Fatalf("ledger row count = %d, want %d (second EnsureScheduled must not create a duplicate)", got, want)
	}
	row := db.rows[[2]string{"state_snapshot:s3:hash-1", "gen-1"}]
	if !row.nextAttemptAt.Equal(first) {
		t.Fatalf("next_attempt_at = %v, want the FIRST call's %v (second call must not overwrite it)", row.nextAttemptAt, first)
	}
}

// TestConfigStateDriftRedriveStoreClaimDueRevisitsTransientRace proves the
// issue #5593 P1-1 "revisited" half of the convergence requirement: a
// (scope, generation) whose config_state_drift evaluation raced ahead of its
// owning config-side repo's activation gets claimed again on its next
// scheduled attempt, before it exhausts its bounded attempt budget.
func TestConfigStateDriftRedriveStoreClaimDueRevisitsTransientRace(t *testing.T) {
	t.Parallel()

	db := &fakeConfigStateDriftRedriveDB{}
	store := NewConfigStateDriftRedriveStore(db)
	ctx := context.Background()

	t0 := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := store.EnsureScheduled(ctx, "state_snapshot:s3:hash-1", "gen-1", t0); err != nil {
		t.Fatalf("EnsureScheduled() error = %v, want nil", err)
	}

	const maxAttempts = 4
	store.Now = func() time.Time { return t0 }

	t1 := t0.Add(5 * time.Minute)
	claims, err := store.ClaimDue(ctx, maxAttempts, 100, t1)
	if err != nil {
		t.Fatalf("ClaimDue() attempt 1 error = %v, want nil", err)
	}
	if len(claims) != 1 || claims[0].AttemptCount != 1 || claims[0].Exhausted {
		t.Fatalf("ClaimDue() attempt 1 = %+v, want exactly one non-exhausted claim with AttemptCount=1", claims)
	}

	store.Now = func() time.Time { return t0.Add(time.Second) }
	claims, err = store.ClaimDue(ctx, maxAttempts, 100, t1.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ClaimDue() immediate re-claim error = %v, want nil", err)
	}
	if len(claims) != 0 {
		t.Fatalf("ClaimDue() immediate re-claim = %+v, want zero claims (row not due yet)", claims)
	}

	store.Now = func() time.Time { return t1.Add(time.Minute) }
	claims, err = store.ClaimDue(ctx, maxAttempts, 100, t1.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ClaimDue() attempt 2 error = %v, want nil", err)
	}
	if len(claims) != 1 || claims[0].AttemptCount != 2 || claims[0].Exhausted {
		t.Fatalf("ClaimDue() attempt 2 = %+v, want exactly one non-exhausted claim with AttemptCount=2 (revisited)", claims)
	}
	if got, want := claims[0].ScopeID, "state_snapshot:s3:hash-1"; got != want {
		t.Fatalf("claimed scope_id = %q, want %q", got, want)
	}
	if got, want := claims[0].GenerationID, "gen-1"; got != want {
		t.Fatalf("claimed generation_id = %q, want %q", got, want)
	}
}

// TestConfigStateDriftRedriveStoreClaimDueTerminatesAndDeletesAfterMaxAttempts
// proves BOTH the issue #5593 P1-1 "terminates" half of the convergence
// requirement AND the P1-B bounded-growth fix in one trace: a genuine "no
// config repo will EVER own this backend" case stops being claimed once
// attempt_count reaches the caller's bound, AND its ledger row is physically
// DELETED at that point -- not left behind with a frozen-in-the-past
// next_attempt_at that would otherwise re-satisfy the due-row scan forever.
func TestConfigStateDriftRedriveStoreClaimDueTerminatesAndDeletesAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	db := &fakeConfigStateDriftRedriveDB{}
	store := NewConfigStateDriftRedriveStore(db)
	ctx := context.Background()

	t0 := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := store.EnsureScheduled(ctx, "state_snapshot:s3:hash-1", "gen-1", t0); err != nil {
		t.Fatalf("EnsureScheduled() error = %v, want nil", err)
	}

	const maxAttempts = 3
	now := t0
	var lastClaims []ConfigStateDriftRedriveClaim
	for i := 0; i < maxAttempts; i++ {
		store.Now = func() time.Time { return now }
		claims, err := store.ClaimDue(ctx, maxAttempts, 100, now.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("ClaimDue() attempt %d error = %v, want nil", i+1, err)
		}
		if len(claims) != 1 {
			t.Fatalf("ClaimDue() attempt %d = %+v, want exactly one claim (attempts 1..maxAttempts must all succeed)", i+1, claims)
		}
		if got, want := claims[0].AttemptCount, i+1; got != want {
			t.Fatalf("ClaimDue() attempt %d AttemptCount = %d, want %d", i+1, got, want)
		}
		wantExhausted := i+1 == maxAttempts
		if claims[0].Exhausted != wantExhausted {
			t.Fatalf("ClaimDue() attempt %d Exhausted = %v, want %v", i+1, claims[0].Exhausted, wantExhausted)
		}
		lastClaims = claims
		now = now.Add(5 * time.Minute)
	}
	if lastClaims[0].AttemptCount != maxAttempts {
		t.Fatalf("final AttemptCount = %d, want %d (the exhausting attempt)", lastClaims[0].AttemptCount, maxAttempts)
	}

	// P1-B: the row must be GONE from the underlying table -- not merely
	// excluded by the attempt_count filter.
	if got, want := len(db.rows), 0; got != want {
		t.Fatalf("ledger row count after exhaustion = %d, want %d (P1-B: the row must be deleted, not left behind)", got, want)
	}

	// The (maxAttempts+1)-th claim, however far in the future "now" is
	// pushed, must return ZERO claims forever.
	for _, future := range []time.Time{now.Add(time.Hour), now.Add(24 * time.Hour)} {
		store.Now = func() time.Time { return future }
		claims, err := store.ClaimDue(ctx, maxAttempts, 100, future.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("ClaimDue() post-exhaustion error = %v, want nil", err)
		}
		if len(claims) != 0 {
			t.Fatalf("ClaimDue() post-exhaustion at %v = %+v, want zero claims (must terminate, not retry forever)", future, claims)
		}
	}
}

// TestConfigStateDriftRedriveStoreClaimDueBoundsAggregateBatchAcrossBothQueries
// proves ClaimDue's two internal queries (claimAndAdvance,
// claimAndDeleteExhausted) share ONE overall limit rather than each getting
// their own full budget -- otherwise a tick with many simultaneously-due
// rows in both states could claim up to 2x the configured batch size.
func TestConfigStateDriftRedriveStoreClaimDueBoundsAggregateBatchAcrossBothQueries(t *testing.T) {
	t.Parallel()

	db := &fakeConfigStateDriftRedriveDB{}
	store := NewConfigStateDriftRedriveStore(db)
	ctx := context.Background()
	t0 := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	const maxAttempts = 2
	// Two rows on their FINAL attempt (attempt_count = maxAttempts-1 = 1,
	// matches claimAndDeleteExhausted) and two rows on their FIRST attempt
	// (attempt_count = 0, matches claimAndAdvance).
	for i, scopeID := range []string{"state_snapshot:s3:final-1", "state_snapshot:s3:final-2"} {
		key := [2]string{scopeID, fmt.Sprintf("gen-%d", i)}
		db.rows = mapOrInit(db.rows)
		db.rows[key] = &fakeConfigStateDriftRedriveRow{attemptCount: maxAttempts - 1, nextAttemptAt: t0}
	}
	for i, scopeID := range []string{"state_snapshot:s3:fresh-1", "state_snapshot:s3:fresh-2"} {
		if err := store.EnsureScheduled(ctx, scopeID, fmt.Sprintf("gen-fresh-%d", i), t0); err != nil {
			t.Fatalf("EnsureScheduled() error = %v, want nil", err)
		}
	}

	store.Now = func() time.Time { return t0 }
	const batchLimit = 3
	claims, err := store.ClaimDue(ctx, maxAttempts, batchLimit, t0.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ClaimDue() error = %v, want nil", err)
	}
	if len(claims) > batchLimit {
		t.Fatalf("ClaimDue() returned %d claims, want <= %d (aggregate batch limit across both queries)", len(claims), batchLimit)
	}
}

func mapOrInit(m map[[2]string]*fakeConfigStateDriftRedriveRow) map[[2]string]*fakeConfigStateDriftRedriveRow {
	if m == nil {
		return map[[2]string]*fakeConfigStateDriftRedriveRow{}
	}
	return m
}

func TestConfigStateDriftRedriveStoreClaimDueSkipsNotYetDueRows(t *testing.T) {
	t.Parallel()

	db := &fakeConfigStateDriftRedriveDB{}
	store := NewConfigStateDriftRedriveStore(db)
	ctx := context.Background()

	t0 := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	future := t0.Add(time.Hour)
	if err := store.EnsureScheduled(ctx, "state_snapshot:s3:hash-1", "gen-1", future); err != nil {
		t.Fatalf("EnsureScheduled() error = %v, want nil", err)
	}

	store.Now = func() time.Time { return t0 }
	claims, err := store.ClaimDue(ctx, 4, 100, t0.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ClaimDue() error = %v, want nil", err)
	}
	if len(claims) != 0 {
		t.Fatalf("ClaimDue() before the schedule = %+v, want zero claims", claims)
	}
}

func TestConfigStateDriftRedriveStoreRequiresScopeAndGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeConfigStateDriftRedriveDB{}
	store := NewConfigStateDriftRedriveStore(db)
	ctx := context.Background()

	if err := store.EnsureScheduled(ctx, "", "gen-1", time.Now()); err == nil {
		t.Fatal("EnsureScheduled() with blank scope id: error = nil, want non-nil")
	}
	if err := store.EnsureScheduled(ctx, "state_snapshot:s3:hash-1", "", time.Now()); err == nil {
		t.Fatal("EnsureScheduled() with blank generation id: error = nil, want non-nil")
	}
}

func TestConfigStateDriftRedriveStoreClaimDueValidatesArguments(t *testing.T) {
	t.Parallel()

	db := &fakeConfigStateDriftRedriveDB{}
	store := NewConfigStateDriftRedriveStore(db)
	ctx := context.Background()

	if _, err := store.ClaimDue(ctx, 0, 100, time.Now()); err == nil {
		t.Fatal("ClaimDue() with maxAttempts=0: error = nil, want non-nil")
	}
	if _, err := store.ClaimDue(ctx, 4, 0, time.Now()); err == nil {
		t.Fatal("ClaimDue() with limit=0: error = nil, want non-nil")
	}
}

func TestConfigStateDriftRedriveStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	var store ConfigStateDriftRedriveStore
	if err := store.EnsureScheduled(context.Background(), "state_snapshot:s3:hash-1", "gen-1", time.Now()); err == nil {
		t.Fatal("nil DB EnsureScheduled: error = nil, want non-nil")
	}
	if _, err := store.ClaimDue(context.Background(), 4, 100, time.Now()); err == nil {
		t.Fatal("nil DB ClaimDue: error = nil, want non-nil")
	}
}
