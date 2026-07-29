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
// exactly the two queries drift_runtime_redrive.go issues
// (ensureConfigStateDriftRedriveScheduledQuery and
// claimDueConfigStateDriftRedrivesQuery), used to prove
// ConfigStateDriftRedriveStore's CONVERGENCE properties (issue #5593 P1-1)
// without a live Postgres. The claim semantics this fake implements --
// atomic claim-and-advance, attempt_count < maxAttempts excludes exhausted
// rows, next_attempt_at <= now gates due rows -- were proven against a real
// Postgres 16 instance before this file was written (scratch DDL/EXPLAIN
// session cited in migrations/087_config_state_drift_runtime_redrive.sql and
// drift_runtime_redrive.go's claimDueConfigStateDriftRedrivesQuery doc
// comment); this fake mirrors that proven behavior in Go so the bounded
// revisit/terminate guarantee has a fast, repeatable regression test.
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
	if !strings.Contains(query, "UPDATE config_state_drift_redrive") {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: unexpected query: %s", query)
	}
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
	nextAttemptAt, ok := args[3].(time.Time)
	if !ok {
		return nil, fmt.Errorf("fakeConfigStateDriftRedriveDB: arg[3] not a time.Time: %#v", args[3])
	}

	type dueKey struct {
		key [2]string
		row *fakeConfigStateDriftRedriveRow
	}
	var due []dueKey
	for key, row := range f.rows {
		if row.attemptCount < maxAttempts && !row.nextAttemptAt.After(now) {
			due = append(due, dueKey{key: key, row: row})
		}
	}
	// ORDER BY next_attempt_at ASC, tie-broken by key for determinism.
	sort.Slice(due, func(i, j int) bool {
		if !due[i].row.nextAttemptAt.Equal(due[j].row.nextAttemptAt) {
			return due[i].row.nextAttemptAt.Before(due[j].row.nextAttemptAt)
		}
		return due[i].key[0]+due[i].key[1] < due[j].key[0]+due[j].key[1]
	})
	if len(due) > limit {
		due = due[:limit]
	}

	claims := make([]ConfigStateDriftRedriveClaim, 0, len(due))
	for _, d := range due {
		d.row.attemptCount++
		d.row.nextAttemptAt = nextAttemptAt
		claims = append(claims, ConfigStateDriftRedriveClaim{
			ScopeID:      d.key[0],
			GenerationID: d.key[1],
			AttemptCount: d.row.attemptCount,
		})
	}
	return &fakeConfigStateDriftRedriveRows{claims: claims}, nil
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
// scheduled attempt, before it exhausts its bounded attempt budget --
// exactly the shape needed for a correlation that resolves within the
// bounded window to get a fresh Handle() call via ReplayDomain.
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

	// Attempt 1: the row is due at t0, gets claimed, and rescheduled for its
	// next attempt.
	t1 := t0.Add(5 * time.Minute)
	claims, err := store.ClaimDue(ctx, maxAttempts, 100, t1)
	if err != nil {
		t.Fatalf("ClaimDue() attempt 1 error = %v, want nil", err)
	}
	if len(claims) != 1 || claims[0].AttemptCount != 1 {
		t.Fatalf("ClaimDue() attempt 1 = %+v, want exactly one claim with AttemptCount=1", claims)
	}

	// Immediately re-claiming (same "now") must return NOTHING -- the row was
	// just rescheduled to t1, which is in the future relative to t0.
	store.Now = func() time.Time { return t0.Add(time.Second) }
	claims, err = store.ClaimDue(ctx, maxAttempts, 100, t1.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ClaimDue() immediate re-claim error = %v, want nil", err)
	}
	if len(claims) != 0 {
		t.Fatalf("ClaimDue() immediate re-claim = %+v, want zero claims (row not due yet)", claims)
	}

	// Attempt 2: advance the clock past t1 -- simulating the correlation
	// still being unresolved at the SECOND scheduled attempt too -- and the
	// SAME row is due again and gets claimed a second time. This is the
	// "revisited" proof: the trigger's original enqueue produced a terminal
	// "no owner" success, and NOTHING but this ledger would otherwise ever
	// touch that (scope, generation) again.
	store.Now = func() time.Time { return t1.Add(time.Minute) }
	claims, err = store.ClaimDue(ctx, maxAttempts, 100, t1.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ClaimDue() attempt 2 error = %v, want nil", err)
	}
	if len(claims) != 1 || claims[0].AttemptCount != 2 {
		t.Fatalf("ClaimDue() attempt 2 = %+v, want exactly one claim with AttemptCount=2 (revisited)", claims)
	}
	if got, want := claims[0].ScopeID, "state_snapshot:s3:hash-1"; got != want {
		t.Fatalf("claimed scope_id = %q, want %q", got, want)
	}
	if got, want := claims[0].GenerationID, "gen-1"; got != want {
		t.Fatalf("claimed generation_id = %q, want %q", got, want)
	}
}

// TestConfigStateDriftRedriveStoreClaimDueTerminatesAfterMaxAttempts proves
// the issue #5593 P1-1 "terminates" half of the convergence requirement: a
// genuine "no config repo will EVER own this backend" case stops being
// claimed once attempt_count reaches the caller's bound -- it does not retry
// forever.
func TestConfigStateDriftRedriveStoreClaimDueTerminatesAfterMaxAttempts(t *testing.T) {
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
		lastClaims = claims
		now = now.Add(5 * time.Minute)
	}
	if lastClaims[0].AttemptCount != maxAttempts {
		t.Fatalf("final AttemptCount = %d, want %d (the exhausting attempt)", lastClaims[0].AttemptCount, maxAttempts)
	}

	// The (maxAttempts+1)-th claim, however far in the future "now" is
	// pushed, must return ZERO claims forever: attempt_count == maxAttempts
	// is NOT < maxAttempts, so the row never matches the claim predicate
	// again. This is the bounded-termination guarantee itself -- proven
	// twice, at two different future timestamps, to rule out "it just
	// hasn't come due yet" as an alternate explanation for zero claims.
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

	row := db.rows[[2]string{"state_snapshot:s3:hash-1", "gen-1"}]
	if row.attemptCount != maxAttempts {
		t.Fatalf("stored attempt_count = %d, want %d (must not keep advancing past the bound)", row.attemptCount, maxAttempts)
	}
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
