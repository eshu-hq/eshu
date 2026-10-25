// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// repairFixture is a database with a derived corpus whose side table was made
// stale (half its rows deleted) and whose state was moved to not_built, the
// shape migration 234 leaves (#8833 ruling-3).
type repairFixture struct {
	t      *testing.T
	ctx    context.Context
	config *pgx.ConnConfig
	db     *sql.DB
	sqlDB  postgres.SQLDB
	epoch  int64
	reader *sdkmetric.ManualReader
	inst   *telemetry.Instruments
	logs   *syncBuffer
}

func newRepairFixture(t *testing.T) *repairFixture {
	t.Helper()
	ctx, db, config := openDatabase(t)
	for _, repo := range testRepos[:3] {
		writeRecords(ctx, t, db, repo, corpus(repo, 40))
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM content_file_secret_lines WHERE line_number % 2 = 0`); err != nil {
		t.Fatal(err)
	}
	epoch, err := BeginDeferral(ctx, postgres.SQLDB{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("repair-test"))
	if err != nil {
		t.Fatal(err)
	}
	return &repairFixture{t: t, ctx: ctx, config: config, db: db, sqlDB: postgres.SQLDB{DB: db}, epoch: epoch, reader: reader, inst: inst, logs: &syncBuffer{}}
}

func (f *repairFixture) repairer(options RepairOptions) *Repairer {
	options.Instruments = f.inst
	options.Logger = slog.New(slog.NewJSONHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	options.Finalize.BatchSize = 7
	return NewRepairer(f.sqlDB, openDedicatedPool(f.t, f.config), options)
}

// runs returns eshu_dp_secret_lines_repair_runs_total by outcome.
func (f *repairFixture) runs(t *testing.T) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_secret_lines_repair_runs_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := point.Attributes.Value("outcome")
				out[outcome.AsString()] = point.Value
			}
		}
	}
	return out
}

// events returns the logged repair events by event_name.
func (f *repairFixture) events(t *testing.T) map[string][]map[string]any {
	t.Helper()
	out := map[string][]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(f.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		name, _ := record["event_name"].(string)
		out[name] = append(out[name], record)
	}
	return out
}

// holdBulkLoadLock takes (5318,1) on its own session, as a running
// bootstrap-index does, until the returned release runs.
func holdBulkLoadLock(t *testing.T, ctx context.Context, db *sql.DB) func() {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, BulkLoadLockClass, BulkLoadLockID); err != nil {
		t.Fatal(err)
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1, $2)`, BulkLoadLockClass, BulkLoadLockID)
		_ = conn.Close()
	}
}

// (a) A not_built state with stale rows is rebuilt to parity and published for
// its epoch.
func TestRepairerPublishesStaleStateToParityLive(t *testing.T) {
	f := newRepairFixture(t)
	outcome, err := f.repairer(RepairOptions{}).Tick(f.ctx)
	if err != nil || outcome != RepairPublished {
		t.Fatalf("Tick = %s, %v; want published", outcome, err)
	}
	if state, epoch := stateRow(f.ctx, t, f.db); state != "ready" || epoch != f.epoch {
		t.Fatalf("state = %s epoch %d, want ready epoch %d", state, epoch, f.epoch)
	}
	requireParity(f.ctx, t, f.db, "after repair")
	if got := f.runs(t); got["published"] != 1 || len(got) != 1 {
		t.Fatalf("runs = %v, want published 1", got)
	}
	events := f.events(t)
	if len(events["secret_lines.repair_started"]) != 1 || len(events["secret_lines.repair_complete"]) != 1 {
		t.Fatalf("events = %v, want one repair_started and one repair_complete", events)
	}
}

// (b) Two repairers race on one not_built state: exactly one finalizes, the
// other skips on the lock; nothing collides on the side table key and nothing
// publishes failed.
func TestRepairerIsSingleFlightLive(t *testing.T) {
	f := newRepairFixture(t)
	slow := RepairOptions{}
	slow.Finalize.afterBatch = func(ctx context.Context, _ string) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	first, second := f.repairer(slow), f.repairer(slow)
	var wg sync.WaitGroup
	outcomes := make([]RepairOutcome, 2)
	errs := make([]error, 2)
	for i, r := range []*Repairer{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcomes[i], errs[i] = r.Tick(f.ctx)
		}()
	}
	wg.Wait()
	published, skipped := 0, 0
	for i := range outcomes {
		if errs[i] != nil {
			t.Fatalf("repairer %d: %v", i, errs[i])
		}
		switch outcomes[i] {
		case RepairPublished:
			published++
		case RepairSkippedLocked:
			skipped++
		}
	}
	if published != 1 || skipped != 1 {
		t.Fatalf("outcomes = %v, want one published and one skipped", outcomes)
	}
	if state, _ := stateRow(f.ctx, t, f.db); state != "ready" {
		t.Fatalf("state = %s, want ready", state)
	}
	requireParity(f.ctx, t, f.db, "after racing repairers")
	if got := f.runs(t); got["failed"] != 0 {
		t.Fatalf("runs = %v, want no failed", got)
	}
}

// (c) A bulk load in flight holds the lock: the repairer skips without
// claiming, and finalizes once the lock is released. An epoch bump during the
// rebuild supersedes it; the next poll publishes the new epoch.
func TestRepairerYieldsToBulkLoadAndFollowsEpochLive(t *testing.T) {
	f := newRepairFixture(t)
	release := holdBulkLoadLock(t, f.ctx, f.db)
	r := f.repairer(RepairOptions{})
	if outcome, err := r.Tick(f.ctx); err != nil || outcome != RepairSkippedLocked {
		t.Fatalf("Tick while locked = %s, %v; want skipped_locked", outcome, err)
	}
	if state, _ := stateRow(f.ctx, t, f.db); state != "not_built" {
		t.Fatalf("state = %s while the lock was held, want not_built (no claim)", state)
	}
	if holder, _ := f.events(t)["secret_lines.repair_skipped"][0]["holder"].(string); !strings.Contains(holder, "pid=") {
		t.Fatalf("skipped event holder = %q, want the holder's backend", holder)
	}
	release()

	bumped := false
	superseding := RepairOptions{}
	superseding.Finalize.afterBatch = func(ctx context.Context, _ string) error {
		if bumped {
			return nil
		}
		bumped = true
		_, err := BeginDeferral(ctx, f.sqlDB)
		return err
	}
	if outcome, err := f.repairer(superseding).Tick(f.ctx); err != nil || outcome != RepairSuperseded {
		t.Fatalf("Tick with an epoch bump = %s, %v; want superseded", outcome, err)
	}
	if state, epoch := stateRow(f.ctx, t, f.db); state != "not_built" || epoch != f.epoch+1 {
		t.Fatalf("state = %s epoch %d, want not_built epoch %d", state, epoch, f.epoch+1)
	}
	if outcome, err := r.Tick(f.ctx); err != nil || outcome != RepairPublished {
		t.Fatalf("next Tick = %s, %v; want published", outcome, err)
	}
	if state, epoch := stateRow(f.ctx, t, f.db); state != "ready" || epoch != f.epoch+1 {
		t.Fatalf("state = %s epoch %d, want ready epoch %d", state, epoch, f.epoch+1)
	}
	requireParity(f.ctx, t, f.db, "after superseded and repeated repair")
}

// (d) A failed rebuild leaves failed, logs repair_failed with its failure
// class and next retry, and the next poll heals it.
func TestRepairerRecordsFailureAndHealsLive(t *testing.T) {
	f := newRepairFixture(t)
	failing := RepairOptions{PollInterval: time.Second}
	failing.Finalize.afterBatch = func(context.Context, string) error { return errors.New("injected batch failure") }
	r := f.repairer(failing)
	if outcome, err := r.Tick(f.ctx); err == nil || outcome != RepairFailed {
		t.Fatalf("Tick = %s, %v; want failed with an error", outcome, err)
	}
	if state, _ := stateRow(f.ctx, t, f.db); state != "failed" {
		t.Fatalf("state = %s, want failed", state)
	}
	failed := f.events(t)["secret_lines.repair_failed"]
	if len(failed) != 1 || failed[0]["failure_class"] != repairFailureClass || failed[0]["next_retry_seconds"] != 2.0 {
		t.Fatalf("repair_failed events = %v, want one with failure_class %s and next_retry_seconds 2", failed, repairFailureClass)
	}
	healed := f.repairer(RepairOptions{})
	if outcome, err := healed.Tick(f.ctx); err != nil || outcome != RepairPublished {
		t.Fatalf("healing Tick = %s, %v; want published", outcome, err)
	}
	requireParity(f.ctx, t, f.db, "after healing a failed state")
	if got := f.runs(t); got["failed"] != 1 || got["published"] != 1 {
		t.Fatalf("runs = %v, want failed 1 and published 1", got)
	}
}

// (e) A ready state is a no-op: no lock is taken, even when the lock is free
// or held, and nothing is claimed.
func TestRepairerLeavesReadyStateAloneLive(t *testing.T) {
	f := newRepairFixture(t)
	if outcome, err := f.repairer(RepairOptions{}).Tick(f.ctx); err != nil || outcome != RepairPublished {
		t.Fatalf("setup Tick = %s, %v", outcome, err)
	}
	_, epoch := stateRow(f.ctx, t, f.db)
	release := holdBulkLoadLock(t, f.ctx, f.db)
	defer release()
	if outcome, err := f.repairer(RepairOptions{}).Tick(f.ctx); err != nil || outcome != RepairSkippedReady {
		t.Fatalf("Tick on ready with the lock held = %s, %v; want skipped_ready (no lock attempt)", outcome, err)
	}
	if state, after := stateRow(f.ctx, t, f.db); state != "ready" || after != epoch {
		t.Fatalf("state = %s epoch %d, want ready epoch %d untouched", state, after, epoch)
	}
}

// A clean shutdown during a rebuild is a cancellation, not a failure: no
// repair_failed error, outcome=cancelled, a Warn repair_cancelled event. The
// state stays failed (the next start heals it).
func TestRepairerCountsShutdownAsCancelledLive(t *testing.T) {
	f := newRepairFixture(t)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	options := RepairOptions{}
	options.Finalize.afterBatch = func(context.Context, string) error { cancel(); return nil }
	outcome, err := f.repairer(options).Tick(ctx)
	if outcome != RepairCancelled || err != nil {
		t.Fatalf("Tick on shutdown = %s, %v; want cancelled and no error", outcome, err)
	}
	if state, _ := stateRow(f.ctx, t, f.db); state != "failed" {
		t.Fatalf("state = %s, want failed (the next start heals it)", state)
	}
	events := f.events(t)
	if len(events["secret_lines.repair_failed"]) != 0 {
		t.Fatalf("repair_failed events = %v, want none on a clean shutdown", events["secret_lines.repair_failed"])
	}
	cancelled := events["secret_lines.repair_cancelled"]
	if len(cancelled) != 1 || cancelled[0]["level"] != "WARN" {
		t.Fatalf("repair_cancelled events = %v, want one at WARN", cancelled)
	}
	if got := f.runs(t); got["cancelled"] != 1 || got["failed"] != 0 {
		t.Fatalf("runs = %v, want cancelled 1 and no failed", got)
	}
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
