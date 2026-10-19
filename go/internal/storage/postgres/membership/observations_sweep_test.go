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

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
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

// deletedBatch stages one sweep batch's RETURNING rows: count rows of state.
func deletedBatch(state string, count int) fake.Rows {
	data := make([][]any, count)
	for i := range data {
		data[i] = []any{state}
	}
	return fake.Rows{Data: data}
}

func TestDeleteExpiredObservationsQueryShape(t *testing.T) {
	t.Parallel()

	query := membershipstore.DeleteExpiredObservationsQuery
	for _, want := range []string{
		"LEFT JOIN repository_selectors s ON s.selector_id = o.selector_id",
		"o.evaluated_at + make_interval(secs => o.liveness_window_seconds) + make_interval(secs => $2::bigint) < $1::timestamptz",
		"(o.state <> $4 OR s.selector_id IS NULL",
		"s.last_cycle_at + make_interval(secs => o.liveness_window_seconds) + make_interval(secs => $2::bigint) < $1::timestamptz",
		"LIMIT $3",
		"FOR UPDATE OF o SKIP LOCKED",
		"WHERE o.scope_id = d.scope_id AND o.selector_id = d.selector_id",
		"RETURNING o.state",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("delete expired query missing %q: %s", want, query)
		}
	}
}

func TestDeleteExpiredObservationsBatchesUntilAShortBatch(t *testing.T) {
	t.Parallel()

	size := membershipstore.ExpiredSweepBatchSize
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{
		deletedBatch("selected", size), deletedBatch("selected", size), deletedBatch("selected", 3),
	}}
	counts, err := membershipstore.NewObservationStore(database).DeleteExpiredObservations(context.Background(), evaluatedAt, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteExpiredObservations() error = %v", err)
	}
	if want := (membership.SweepCounts{Deleted: int64(2*size + 3)}); counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
	if len(database.Queries) != 3 {
		t.Fatalf("statements = %d, want 3 (two full batches, then a short one)", len(database.Queries))
	}
	want := []any{evaluatedAt.UTC(), int64(7 * 24 * 3600), membershipstore.ExpiredSweepBatchSize, "not_listed"}
	if got := database.Queries[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("bound args = %#v, want %#v (UTC now, grace seconds, batch size, the guarded not_listed state)", got, want)
	}
}

func TestDeleteExpiredObservationsStopsAtTheBatchCap(t *testing.T) {
	t.Parallel()

	responses := make([]fake.Rows, membershipstore.ExpiredSweepMaxBatches+5)
	for i := range responses {
		responses[i] = deletedBatch("selected", membershipstore.ExpiredSweepBatchSize)
	}
	database := &fake.ExecQueryer{QueryResponses: responses}
	counts, err := membershipstore.NewObservationStore(database).DeleteExpiredObservations(context.Background(), evaluatedAt, 0)
	if err != nil {
		t.Fatalf("DeleteExpiredObservations() error = %v", err)
	}
	if len(database.Queries) != membershipstore.ExpiredSweepMaxBatches {
		t.Fatalf("statements = %d, want the %d-batch cap", len(database.Queries), membershipstore.ExpiredSweepMaxBatches)
	}
	if want := int64(membershipstore.ExpiredSweepMaxBatches * membershipstore.ExpiredSweepBatchSize); counts.Deleted != want {
		t.Fatalf("deleted = %d, want %d", counts.Deleted, want)
	}
}

func TestDeleteExpiredObservationsReportsRowsDeletedBeforeAnError(t *testing.T) {
	t.Parallel()

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{
		deletedBatch("selected", membershipstore.ExpiredSweepBatchSize),
		{FailWith: errors.New("conn reset")},
	}}
	store := membershipstore.NewObservationStore(database)
	counts, err := store.DeleteExpiredObservations(context.Background(), evaluatedAt, time.Hour)
	if err == nil || !strings.Contains(err.Error(), "conn reset") {
		t.Fatalf("DeleteExpiredObservations() error = %v, want the second batch's error", err)
	}
	if counts.Deleted != int64(membershipstore.ExpiredSweepBatchSize) {
		t.Fatalf("deleted = %d, want the first batch's %d rows", counts.Deleted, membershipstore.ExpiredSweepBatchSize)
	}
}

// errAfterRows is a db.Rows that yields its states, then reports err from
// Err, as a DELETE ... RETURNING stream that fails mid-batch does.
type errAfterRows struct {
	states []string
	index  int
	err    error
}

func (r *errAfterRows) Next() bool { return r.index < len(r.states) }

func (r *errAfterRows) Scan(dest ...any) error {
	target, ok := dest[0].(*string)
	if !ok {
		return errors.New("errAfterRows: scan target is not *string")
	}
	*target = r.states[r.index]
	r.index++
	return nil
}

func (r *errAfterRows) Err() error   { return r.err }
func (r *errAfterRows) Close() error { return nil }

// errAfterRowsDB serves one errAfterRows for every query.
type errAfterRowsDB struct{ rows *errAfterRows }

func (d errAfterRowsDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return d.rows, nil
}

func (d errAfterRowsDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return affected(0), nil
}

func TestDeleteExpiredObservationsScanErrorCountsNothingForFailedBatch(t *testing.T) {
	t.Parallel()

	// The second batch streams one not_listed row, then its next Scan
	// fails. The batch is one atomic statement, so it contributes zero:
	// only the first, completed batch's counts stand.
	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{
		deletedBatch("selected", membershipstore.ExpiredSweepBatchSize),
		{Data: [][]any{{"not_listed"}, {123}}},
	}}
	counts, err := membershipstore.NewObservationStore(database).DeleteExpiredObservations(context.Background(), evaluatedAt, time.Hour)
	if err == nil {
		t.Fatal("DeleteExpiredObservations() error = nil, want the scan error")
	}
	want := membership.SweepCounts{Deleted: int64(membershipstore.ExpiredSweepBatchSize)}
	if counts != want {
		t.Fatalf("counts = %+v, want %+v (failed batch counts nothing)", counts, want)
	}
}

func TestDeleteExpiredObservationsRowsErrorCountsNothingForFailedBatch(t *testing.T) {
	t.Parallel()

	boom := errors.New("stream reset")
	database := errAfterRowsDB{rows: &errAfterRows{states: []string{"selected", "not_listed"}, err: boom}}
	counts, err := membershipstore.NewObservationStore(database).DeleteExpiredObservations(context.Background(), evaluatedAt, time.Hour)
	if !errors.Is(err, boom) {
		t.Fatalf("DeleteExpiredObservations() error = %v, want it to wrap %v", err, boom)
	}
	if counts != (membership.SweepCounts{}) {
		t.Fatalf("counts = %+v, want zero (failed batch counts nothing)", counts)
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
			counts, err := tc.store.DeleteExpiredObservations(context.Background(), tc.now, tc.grace)
			if err == nil || !strings.Contains(err.Error(), tc.want) || counts != (membership.SweepCounts{}) {
				t.Fatalf("DeleteExpiredObservations() = (%+v, %v), want (zero, error containing %q)", counts, err, tc.want)
			}
		})
	}
}
