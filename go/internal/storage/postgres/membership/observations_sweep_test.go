// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// affected is a sql.Result reporting a fixed number of deleted rows.
type affected int64

func (affected) LastInsertId() (int64, error)   { return 0, nil }
func (a affected) RowsAffected() (int64, error) { return int64(a), nil }

func sweepResults(counts ...int64) []sql.Result {
	results := make([]sql.Result, len(counts))
	for i, count := range counts {
		results[i] = affected(count)
	}
	return results
}

func TestDeleteExpiredObservationsQueryShape(t *testing.T) {
	t.Parallel()

	query := membershipstore.DeleteExpiredObservationsQuery
	for _, want := range []string{
		"WHERE state <> $4",
		"evaluated_at + make_interval(secs => liveness_window_seconds) + make_interval(secs => $2::bigint) < $1::timestamptz",
		"LIMIT $3",
		"FOR UPDATE SKIP LOCKED",
		"WHERE o.scope_id = d.scope_id AND o.selector_id = d.selector_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("delete expired query missing %q: %s", want, query)
		}
	}
}

func TestDeleteExpiredObservationsBatchesUntilAShortBatch(t *testing.T) {
	t.Parallel()

	size := int64(membershipstore.ExpiredSweepBatchSize)
	database := &fake.ExecQueryer{ExecResults: sweepResults(size, size, 3)}
	deleted, err := membershipstore.NewObservationStore(database).DeleteExpiredObservations(context.Background(), evaluatedAt, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteExpiredObservations() error = %v", err)
	}
	if want := 2*size + 3; deleted != want {
		t.Fatalf("deleted = %d, want %d", deleted, want)
	}
	if len(database.Execs) != 3 {
		t.Fatalf("statements = %d, want 3 (two full batches, then a short one)", len(database.Execs))
	}
	want := []any{evaluatedAt.UTC(), int64(7 * 24 * 3600), membershipstore.ExpiredSweepBatchSize, "not_listed"}
	if got := database.Execs[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("bound args = %#v, want %#v (UTC now, grace seconds, batch size, the kept not_listed state)", got, want)
	}
}

func TestDeleteExpiredObservationsStopsAtTheBatchCap(t *testing.T) {
	t.Parallel()

	counts := make([]int64, membershipstore.ExpiredSweepMaxBatches+5)
	for i := range counts {
		counts[i] = int64(membershipstore.ExpiredSweepBatchSize)
	}
	database := &fake.ExecQueryer{ExecResults: sweepResults(counts...)}
	deleted, err := membershipstore.NewObservationStore(database).DeleteExpiredObservations(context.Background(), evaluatedAt, 0)
	if err != nil {
		t.Fatalf("DeleteExpiredObservations() error = %v", err)
	}
	if len(database.Execs) != membershipstore.ExpiredSweepMaxBatches {
		t.Fatalf("statements = %d, want the %d-batch cap", len(database.Execs), membershipstore.ExpiredSweepMaxBatches)
	}
	if want := int64(membershipstore.ExpiredSweepMaxBatches * membershipstore.ExpiredSweepBatchSize); deleted != want {
		t.Fatalf("deleted = %d, want %d", deleted, want)
	}
}

func TestDeleteExpiredObservationsReportsRowsDeletedBeforeAnError(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{ExecResults: sweepResults(int64(membershipstore.ExpiredSweepBatchSize))}
	store := membershipstore.NewObservationStore(&failAfter{ExecQueryer: database, ok: 1, err: errors.New("conn reset")})
	deleted, err := store.DeleteExpiredObservations(context.Background(), evaluatedAt, time.Hour)
	if err == nil || !strings.Contains(err.Error(), "conn reset") {
		t.Fatalf("DeleteExpiredObservations() error = %v, want the second batch's error", err)
	}
	if deleted != int64(membershipstore.ExpiredSweepBatchSize) {
		t.Fatalf("deleted = %d, want the first batch's %d rows", deleted, membershipstore.ExpiredSweepBatchSize)
	}
}

func TestDeleteExpiredObservationsRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		store membershipstore.ObservationStore
		now   time.Time
		grace time.Duration
		want  string
	}{
		{name: "no database", store: membershipstore.NewObservationStore(nil), now: evaluatedAt, want: "database is required"},
		{name: "zero now", store: membershipstore.NewObservationStore(&fake.ExecQueryer{}), want: "now is required"},
		{name: "negative grace", store: membershipstore.NewObservationStore(&fake.ExecQueryer{}), now: evaluatedAt, grace: -time.Second, want: "grace must not be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deleted, err := tc.store.DeleteExpiredObservations(context.Background(), tc.now, tc.grace)
			if err == nil || !strings.Contains(err.Error(), tc.want) || deleted != 0 {
				t.Fatalf("DeleteExpiredObservations() = (%d, %v), want (0, error containing %q)", deleted, err, tc.want)
			}
		})
	}
}

// failAfter passes the first ok ExecContext calls through and fails the rest.
type failAfter struct {
	*fake.ExecQueryer
	ok  int
	err error
}

func (f *failAfter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if f.ok == 0 {
		return nil, f.err
	}
	f.ok--
	return f.ExecQueryer.ExecContext(ctx, query, args...)
}
