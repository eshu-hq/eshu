// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// snapshotRows is a tiny scripted db.Rows: each row is a list of values that
// Scan assigns by destination type.
type snapshotRows struct {
	rows   [][]any
	cursor int
}

func (r *snapshotRows) Next() bool { r.cursor++; return r.cursor <= len(r.rows) }
func (r *snapshotRows) Err() error { return nil }
func (r *snapshotRows) Close() error {
	return nil
}

func (r *snapshotRows) Scan(dest ...any) error {
	row := r.rows[r.cursor-1]
	for i, target := range dest {
		switch d := target.(type) {
		case *time.Time:
			*d = row[i].(time.Time)
		case *bool:
			*d = row[i].(bool)
		case *string:
			*d = row[i].(string)
		case *int64:
			*d = row[i].(int64)
		default:
			return errors.New("snapshotRows: unsupported scan destination")
		}
	}
	return nil
}

// snapshotStore is a db.ReadStore whose snapshot transactions answer the
// stored-summary clock read as "table not installed" (a typed fallback), run
// the live active-work statement behind a gate, and return no rows for any
// other status statement. It counts live statements and snapshot transactions.
type snapshotStore struct {
	db.ReadStore // never called: the wiring only opens snapshots
	gate         chan struct{}
	started      chan struct{}
	liveRuns     atomic.Int32
	begun        atomic.Int32
}

func (s *snapshotStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	s.begun.Add(1)
	return &snapshotTx{store: s}, nil
}

type snapshotTx struct {
	db.ReadTransaction // QueryRowContext is never called
	store              *snapshotStore
}

func (t *snapshotTx) Commit() error   { return nil }
func (t *snapshotTx) Rollback() error { return nil }

func (t *snapshotTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
		return &snapshotRows{rows: [][]any{{time.Now(), false}}}, nil
	case strings.Contains(query, "active_work_stage"):
		t.store.liveRuns.Add(1)
		select {
		case t.store.started <- struct{}{}:
		default:
		}
		<-t.store.gate
		return &snapshotRows{rows: [][]any{
			{"stage", int64(1), `{"stage":"reducer","status":"pending","count":7}`},
		}}, nil
	default:
		return &snapshotRows{}, nil
	}
}

// TestSnapshotStatusReaderSharesOneLiveStatementAcrossTransactions drives the
// production wiring: two concurrent status reads open two snapshot
// transactions, each builds its own StatusStore through the factory, and the
// shared process-wide summary reader runs one live statement for both. A
// reader owned by the per-transaction store would run two.
func TestSnapshotStatusReaderSharesOneLiveStatementAcrossTransactions(t *testing.T) {
	t.Parallel()

	store := &snapshotStore{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	summaryReader, err := newStatusSummaryReader(func(key string) string {
		if key == summary.ReadEnabledEnv {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("newStatusSummaryReader() error = %v", err)
	}
	reader := newSnapshotStatusReader(store, nil, summaryReader)

	results := make([]status.RawSnapshot, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = reader.ReadStatusSnapshot(context.Background(), time.Now())
		}()
		if i == 0 {
			<-store.started // the first read holds the live statement
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for summaryReader.Waiting() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("the second transaction never joined the first one's live statement")
		}
		time.Sleep(time.Millisecond)
	}
	close(store.gate)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("read %d error = %v", i, err)
		}
	}
	if got := store.begun.Load(); got != 2 {
		t.Fatalf("snapshot transactions = %d, want 2", got)
	}
	if got := store.liveRuns.Load(); got != 1 {
		t.Fatalf("live statements = %d for two concurrent fallbacks across two transactions, want 1 through the production wiring", got)
	}
	for i, snapshot := range results {
		source := snapshot.ActiveWorkSource
		if source.Source != status.ActiveWorkSourceLiveFallback || source.Reason != status.ActiveWorkReasonNotInstalled {
			t.Fatalf("read %d source = %+v, want live_fallback/not_installed", i, source)
		}
		if len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != 7 {
			t.Fatalf("read %d stage counts = %+v, want the shared live answer", i, snapshot.StageCounts)
		}
	}
}

func TestNewStatusSummaryReaderFailsStartupOnAnInvalidStaleAfter(t *testing.T) {
	t.Parallel()

	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	if _, err := newStatusSummaryReader(env(map[string]string{summary.ReadEnabledEnv: "true", summary.StaleAfterEnv: "soon"})); err == nil ||
		!strings.Contains(err.Error(), summary.StaleAfterEnv) {
		t.Fatalf("error = %v, want a startup error naming %s", err, summary.StaleAfterEnv)
	}
	if _, err := newStatusSummaryReader(env(nil)); err != nil {
		t.Fatalf("an unset environment error = %v, want none (the reader is off)", err)
	}
}

// TestStatusStoreFactoryReadsNoEnvironment proves the per-transaction factory
// does not re-read the environment: the process-wide reader decides, and a
// later environment change does not change a running API.
func TestStatusStoreFactoryReadsNoEnvironment(t *testing.T) {
	t.Setenv(summary.ReadEnabledEnv, "true")
	store := &snapshotStore{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	close(store.gate)
	summaryReader := pgstatus.NewStatusSummaryReaderWithConfig(summary.ReadConfig{}) // reader off
	snapshot, err := newSnapshotStatusReader(store, nil, summaryReader).ReadStatusSnapshot(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("ReadStatusSnapshot() error = %v", err)
	}
	if got := snapshot.ActiveWorkSource; got.Source != status.ActiveWorkSourceLive || got.Reason != status.ActiveWorkReasonFlagOff {
		t.Fatalf("source = %+v, want live/flag_off: the environment must not override the process-wide reader", got)
	}
}
