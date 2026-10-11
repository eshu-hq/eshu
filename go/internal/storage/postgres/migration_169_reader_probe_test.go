// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
)

// migration169ProbeInterval is how often the #7961 probe reads content_files
// and samples pg_locks while migration 169 runs.
const migration169ProbeInterval = 100 * time.Millisecond

const migration169ProbeRead = "SELECT count(*) FROM content_files WHERE repo_id = 'repo-1'"

// migration169Read is one timed probe read of content_files.
type migration169Read struct {
	started time.Time
	wait    time.Duration
}

// migration169LockSample is one pg_locks row on content_files held or awaited
// by a session other than the probe.
type migration169LockSample struct {
	at      time.Time
	mode    string
	granted bool
	query   string
}

// migration169Window is the interval between a phase's applying and applied
// log events.
type migration169Window struct {
	start, end time.Time
}

func (w migration169Window) duration() time.Duration { return w.end.Sub(w.start) }

func (w migration169Window) contains(at time.Time) bool {
	return !at.Before(w.start) && !at.After(w.end)
}

// migration169PhaseRecorder is a slog.Handler that records the tracked
// migrator's phase_applying and phase_applied events, the operator signal for
// each 169 phase.
type migration169PhaseRecorder struct {
	mu      sync.Mutex
	started map[string]time.Time
	ended   map[string]time.Time
}

func (r *migration169PhaseRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *migration169PhaseRecorder) Handle(_ context.Context, record slog.Record) error {
	var phase string
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "phase" {
			phase = attr.Value.String()
			return false
		}
		return true
	})
	if phase == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started == nil {
		r.started, r.ended = map[string]time.Time{}, map[string]time.Time{}
	}
	switch record.Message {
	case "postgres schema migration phase applying":
		r.started[phase] = record.Time
	case "postgres schema migration phase applied":
		r.ended[phase] = record.Time
	}
	return nil
}

func (r *migration169PhaseRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *migration169PhaseRecorder) WithGroup(string) slog.Handler { return r }

func (r *migration169PhaseRecorder) window(phase string) (migration169Window, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start, okStart := r.started[phase]
	end, okEnd := r.ended[phase]
	return migration169Window{start: start, end: end}, okStart && okEnd
}

func (r *migration169PhaseRecorder) duration(phase string) time.Duration {
	window, ok := r.window(phase)
	if !ok {
		return -1
	}
	return window.duration()
}

// migration169Probe runs a reader loop and a pg_locks sampler on their own
// pooled connections until stop.
type migration169Probe struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	reads  []migration169Read
	locks  []migration169LockSample
	errs   []error
}

func startMigration169Probe(ctx context.Context, database *sql.DB) *migration169Probe {
	probeCtx, cancel := context.WithCancel(ctx)
	probe := &migration169Probe{cancel: cancel}
	probe.wg.Add(2)
	go probe.loop(probeCtx, func() {
		started := time.Now()
		var count int
		// The read gets the parent context so stop never cancels a read that
		// is still waiting on a lock: its full wait is the measurement.
		err := database.QueryRowContext(ctx, migration169ProbeRead).Scan(&count)
		probe.record(func() {
			probe.reads = append(probe.reads, migration169Read{started: started, wait: time.Since(started)})
		}, err)
	})
	go probe.loop(probeCtx, func() {
		samples, err := sampleContentFilesLocks(probeCtx, database)
		probe.record(func() { probe.locks = append(probe.locks, samples...) }, err)
	})
	return probe
}

func (p *migration169Probe) loop(ctx context.Context, step func()) {
	defer p.wg.Done()
	ticker := time.NewTicker(migration169ProbeInterval)
	defer ticker.Stop()
	for {
		step()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *migration169Probe) record(apply func(), err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.errs = append(p.errs, err)
		return
	}
	apply()
}

// stop ends both loops, waits for an in-flight read to finish, and returns
// what they saw plus every read or sampler error. Sampler errors caused by the
// stop itself are not errors.
func (p *migration169Probe) stop() ([]migration169Read, []migration169LockSample, error) {
	p.cancel()
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads, p.locks, errors.Join(p.errs...)
}

func sampleContentFilesLocks(ctx context.Context, database *sql.DB) ([]migration169LockSample, error) {
	rows, err := database.QueryContext(ctx, `
SELECT clock_timestamp(), l.mode, l.granted, left(regexp_replace(a.query, '\s+', ' ', 'g'), 48)
FROM pg_locks AS l
JOIN pg_stat_activity AS a ON a.pid = l.pid
WHERE l.locktype = 'relation'
  AND l.relation = 'content_files'::regclass
  AND a.pid <> pg_backend_pid()
  AND a.query <> $1`, migration169ProbeRead)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []migration169LockSample
	for rows.Next() {
		var sample migration169LockSample
		if err := rows.Scan(&sample.at, &sample.mode, &sample.granted, &sample.query); err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	if err := rows.Err(); err != nil && ctx.Err() == nil {
		return nil, err
	}
	return out, nil
}

func readsWithin(reads []migration169Read, window migration169Window) []migration169Read {
	var out []migration169Read
	for _, read := range reads {
		if window.contains(read.started) {
			out = append(out, read)
		}
	}
	return out
}

func maxReadWait(reads []migration169Read) time.Duration {
	var worst time.Duration
	for _, read := range reads {
		worst = max(worst, read.wait)
	}
	return worst
}

func lockModesWithin(samples []migration169LockSample, window migration169Window) []string {
	var modes []string
	for _, sample := range samples {
		if window.contains(sample.at) && !slices.Contains(modes, sample.mode) {
			modes = append(modes, sample.mode)
		}
	}
	slices.Sort(modes)
	return modes
}

// assertBackfillLockModes fails if any session held or awaited ACCESS
// EXCLUSIVE on content_files during the backfill, and requires the sampler to
// have seen the UPDATE's granted ROW EXCLUSIVE so the check is not vacuous.
func assertBackfillLockModes(t *testing.T, samples []migration169LockSample, backfill migration169Window) {
	t.Helper()
	sawUpdate := false
	for _, sample := range samples {
		if !backfill.contains(sample.at) {
			continue
		}
		if sample.mode == "AccessExclusiveLock" {
			t.Fatalf("AccessExclusiveLock (granted=%t) on content_files during the backfill: %q", sample.granted, sample.query)
		}
		if sample.mode == "RowExclusiveLock" && sample.granted {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Fatal("pg_locks sampler never saw the backfill UPDATE's RowExclusiveLock on content_files")
	}
}

// assertMigration169Receipts requires exactly want ledger receipts for path.
func assertMigration169Receipts(t *testing.T, ctx context.Context, database *sql.DB, path string, want int) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE path = $1", path).Scan(&got); err != nil {
		t.Fatalf("count 169 receipts: %v", err)
	}
	if got != want {
		t.Fatalf("169 receipts = %d, want %d", got, want)
	}
}

// contentFilesTupleFingerprint hashes every content_files ctid. An UPDATE
// writes a new tuple version for each row it touches, so an unchanged
// fingerprint proves a statement rewrote no row.
func contentFilesTupleFingerprint(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()
	var fingerprint string
	if err := database.QueryRowContext(ctx,
		"SELECT md5(string_agg(ctid::text, ',' ORDER BY ctid)) FROM content_files").Scan(&fingerprint); err != nil {
		t.Fatalf("fingerprint content_files tuples: %v", err)
	}
	return fingerprint
}

// interruptMigration169Backfill clears the tag and receipt, starts the tracked
// apply, terminates the migrator's backend while its backfill UPDATE runs,
// and requires: the committed ALTER kept, no tag written, no receipt; then a
// rerun that completes with exactly one receipt.
func interruptMigration169Backfill(t *testing.T, ctx context.Context, database *sql.DB, definitions []Definition, path string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, "ALTER TABLE content_files DROP COLUMN generation_id"); err != nil {
		t.Fatalf("reset generation tag for interruption: %v", err)
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM eshu_schema_migrations WHERE path = $1", path); err != nil {
		t.Fatalf("clear 169 receipt for interruption: %v", err)
	}
	quiet := slog.New(slog.DiscardHandler)
	result := make(chan error, 1)
	go func() {
		result <- applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, quiet, schemaBootstrapCoordination{})
	}()
	deadline := time.Now().Add(time.Minute)
	terminated := false
	for !terminated && time.Now().Before(deadline) {
		err := database.QueryRowContext(ctx, `
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE state = 'active' AND pid <> pg_backend_pid() AND query LIKE '%UPDATE content_files AS f%'
LIMIT 1`).Scan(&terminated)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("terminate 169 backfill: %v", err)
		}
		if !terminated {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !terminated {
		t.Fatal("169 backfill UPDATE never became active to interrupt")
	}
	if err := <-result; err == nil {
		t.Fatal("interrupted 169 apply returned nil, want the terminated backend's error")
	}
	var columns int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'content_files' AND column_name = 'generation_id'`).Scan(&columns); err != nil {
		t.Fatalf("read generation_id column: %v", err)
	}
	if columns != 1 {
		t.Fatalf("generation_id columns after interruption = %d, want 1 (the ALTER phase committed)", columns)
	}
	if tagged := countTaggedContentFiles(t, ctx, database); tagged != 0 {
		t.Fatalf("tagged rows after interruption = %d, want 0 (the UPDATE rolled back)", tagged)
	}
	assertMigration169Receipts(t, ctx, database, path, 0)
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, quiet, schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("rerun after interrupted 169: %v", err)
	}
	assertMigration169Receipts(t, ctx, database, path, 1)
	t.Log("interrupted 169: backend terminated during backfill; ALTER kept, 0 tags, 0 receipts; rerun completed with 1 receipt")
}
