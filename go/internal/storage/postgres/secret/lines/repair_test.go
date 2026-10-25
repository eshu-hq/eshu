// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestRepairBackoffDoublesToTheCapAndResets(t *testing.T) {
	t.Parallel()

	r := NewRepairer(nil, nil, RepairOptions{PollInterval: 30 * time.Second, MaxBackoff: time.Hour})
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := r.nextDelay(RepairFailed); got != w {
			t.Fatalf("failure %d: delay %v, want %v", i+1, got, w)
		}
	}
	if got := r.nextDelay(RepairSkippedLocked); got != 30*time.Second {
		t.Fatalf("delay after a non-failed poll = %v, want the poll interval", got)
	}
	if got := r.nextDelay(RepairFailed); got != time.Minute {
		t.Fatalf("first failure after a reset = %v, want 1m", got)
	}
}

func TestRepairerDefaults(t *testing.T) {
	t.Parallel()

	r := NewRepairer(nil, nil, RepairOptions{})
	if r.options.PollInterval != 30*time.Second || r.options.MaxBackoff != time.Hour {
		t.Fatalf("defaults = %v / %v, want 30s / 1h", r.options.PollInterval, r.options.MaxBackoff)
	}
	if r.options.Logger == nil || r.options.Finalize.Logger == nil {
		t.Fatal("nil loggers were not defaulted")
	}
}

// failingDatabase fails every state read.
type failingDatabase struct{ reads atomic.Int64 }

func (f *failingDatabase) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	f.reads.Add(1)
	return nil, errors.New("database down")
}

func (f *failingDatabase) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("database down")
}

func (f *failingDatabase) Begin(context.Context) (db.Transaction, error) {
	return nil, errors.New("database down")
}

// TestRepairerRunPollsAtOnceAndStopsOnCancel checks the first poll needs no
// interval, a failed state read is retried rather than returned, and Run
// returns nil when its context ends.
func TestRepairerRunPollsAtOnceAndStopsOnCancel(t *testing.T) {
	t.Parallel()

	database := &failingDatabase{}
	r := NewRepairer(database, nil, RepairOptions{PollInterval: 5 * time.Millisecond, MaxBackoff: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for database.reads.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil on cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	if database.reads.Load() < 3 {
		t.Fatalf("state reads = %d, want at least 3 (failed polls retried)", database.reads.Load())
	}
}
