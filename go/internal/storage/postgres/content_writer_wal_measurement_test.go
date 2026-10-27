// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// writerWALSample is a PostgreSQL 18 per-backend counter reading. The measured
// writer pool has one connection, so a PID change invalidates the comparison.
type writerWALSample struct {
	pid   int
	bytes int64
	reset sql.NullTime
}

type walCounterConn interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func sampleWriterWAL(t *testing.T, ctx context.Context, db walCounterConn) writerWALSample {
	t.Helper()
	var beforePID int
	if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&beforePID); err != nil {
		t.Fatalf("writer backend PID: %v", err)
	}
	// PostgreSQL flushes cumulative stats when this backend next becomes idle.
	// Separate statements allow that flush before the fresh counter read.
	if _, err := db.ExecContext(ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
		t.Fatalf("force writer stats flush (PostgreSQL 18 required): %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT pg_stat_clear_snapshot()`); err != nil {
		t.Fatalf("clear writer stats snapshot: %v", err)
	}
	var sample writerWALSample
	if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid(), counters.wal_bytes::bigint, counters.stats_reset
        FROM pg_stat_get_backend_wal(pg_backend_pid()) AS counters`).Scan(&sample.pid, &sample.bytes, &sample.reset); err != nil {
		t.Fatalf("writer backend WAL (PostgreSQL 18 required): %v", err)
	}
	if sample.pid != beforePID {
		t.Fatalf("writer backend changed during WAL sample: %d -> %d", beforePID, sample.pid)
	}
	return sample
}

func writerWALBetween(t *testing.T, before, after writerWALSample) int64 {
	t.Helper()
	if !sameWriterWALCounter(before, after) {
		t.Fatalf("writer WAL counter changed identity or reset: before=%+v after=%+v", before, after)
	}
	return after.bytes - before.bytes
}

func sameWriterWALCounter(before, after writerWALSample) bool {
	return before.pid == after.pid && before.reset == after.reset && after.bytes >= before.bytes
}

func TestWriterWALCounterIdentity(t *testing.T) {
	baseline := writerWALSample{pid: 11, bytes: 100}
	reset := sql.NullTime{Valid: true}
	for _, tc := range []struct {
		name  string
		after writerWALSample
		want  bool
	}{
		{name: "same_backend", after: writerWALSample{pid: 11, bytes: 150}, want: true},
		{name: "different_backend", after: writerWALSample{pid: 12, bytes: 150}},
		{name: "counter_regressed", after: writerWALSample{pid: 11, bytes: 80}},
		{name: "counter_reset_then_regrew", after: writerWALSample{pid: 11, bytes: 150, reset: reset}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameWriterWALCounter(baseline, tc.after); got != tc.want {
				t.Errorf("counter identity valid = %v, want %v", got, tc.want)
			}
		})
	}

	validEpoch := sql.NullTime{Time: time.Unix(1700000000, 0).UTC(), Valid: true}
	validBaseline := writerWALSample{pid: 11, bytes: 100, reset: validEpoch}
	changedEpoch := sql.NullTime{Time: validEpoch.Time.Add(time.Second), Valid: true}
	for _, tc := range []struct {
		name  string
		after writerWALSample
		want  bool
	}{
		{name: "valid_same_epoch", after: writerWALSample{pid: 11, bytes: 150, reset: validEpoch}, want: true},
		{name: "valid_changed_epoch_after_regrowth", after: writerWALSample{pid: 11, bytes: 150, reset: changedEpoch}},
		{name: "valid_different_backend", after: writerWALSample{pid: 12, bytes: 150, reset: validEpoch}},
		{name: "valid_counter_regressed", after: writerWALSample{pid: 11, bytes: 80, reset: validEpoch}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameWriterWALCounter(validBaseline, tc.after); got != tc.want {
				t.Errorf("counter identity valid = %v, want %v", got, tc.want)
			}
		})
	}
}
