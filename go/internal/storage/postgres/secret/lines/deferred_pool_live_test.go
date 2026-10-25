// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

// fixtureGetenv points the runtime openers at the fixture's disposable database.
func fixtureGetenv(t *testing.T, f *repairFixture, extra map[string]string) func(string) string {
	t.Helper()
	u, err := url.Parse(os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + f.config.Database
	dsn := u.String()
	return func(key string) string {
		if key == "ESHU_POSTGRES_DSN" {
			return dsn
		}
		return extra[key]
	}
}

// productionDeferredOptions is the production composition
// (BootstrapDeferredPoolOptions, the same call openBootstrapDB makes) with a
// faster probe cadence so the tests do not wait a second per try.
func productionDeferredOptions(getenv func(string) string, fence *RunFence, wait time.Duration, logger *slog.Logger) DeferredPoolOptions {
	options := BootstrapDeferredPoolOptions(RuntimePostgresOpener{}, getenv, fence, wait, logger)
	options.Poll = 100 * time.Millisecond
	options.LogEvery = time.Second
	return options
}

// holdBothLocksExclusive takes (5318,1) and (5318,2) exclusive on one session,
// as a Repairer mid-rebuild does, until the returned release runs.
func holdBothLocksExclusive(t *testing.T, ctx context.Context, db *sql.DB) func() {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{BulkLoadLockID, DeferredWritersLockID} {
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, BulkLoadLockClass, id); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock_all()`)
		_ = conn.Close()
	}
}

func jsonLogger(buf *syncBuffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func logEvents(t *testing.T, buf *syncBuffer) map[string][]map[string]any {
	t.Helper()
	out := map[string][]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
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

// sharedWritersHeldBy counts the sessions holding (5318,2) shared.
func sharedWritersHeldBy(ctx context.Context, t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks
WHERE locktype = 'advisory' AND classid = $1 AND objid = $2 AND mode = 'ShareLock' AND granted
  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`,
		BulkLoadLockClass, DeferredWritersLockID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestDeferredPoolWaitsForARebuildLongerThanThePingLive (#8833 ruling-6 proof
// A): a rebuild holds (5318,1) and (5318,2) for 5 s while the startup ping
// budget is 2 s. The pool still opens once the rebuild ends, because the probe
// waits with the ownership wait instead of the connect hook waiting inside the
// ping, and the wait is logged with lock=deferred_writers.
func TestDeferredPoolWaitsForARebuildLongerThanThePingLive(t *testing.T) {
	f := newRepairFixture(t)
	release := holdBothLocksExclusive(t, f.ctx, f.db)
	defer release()
	time.AfterFunc(5*time.Second, release)

	logs := &syncBuffer{}
	getenv := fixtureGetenv(t, f, map[string]string{"ESHU_POSTGRES_PING_TIMEOUT": "2s"})
	started := time.Now()
	pool, err := OpenDeferredWriterPool(f.ctx, productionDeferredOptions(getenv, NewRunFence(nil), time.Minute, jsonLogger(logs)))
	if err != nil {
		t.Fatalf("OpenDeferredWriterPool behind a 5 s rebuild with a 2 s ping budget: %v", err)
	}
	defer func() { _ = pool.Close() }()
	if waited := time.Since(started); waited < 4500*time.Millisecond {
		t.Fatalf("pool opened after %s, before the rebuild released", waited)
	}
	var setting string
	if err := pool.QueryRowContext(f.ctx, `SELECT current_setting('eshu.secret_lines_derive', true)`).Scan(&setting); err != nil || setting != "deferred" {
		t.Fatalf("pool session eshu.secret_lines_derive = %q, %v; want deferred", setting, err)
	}
	if got := sharedWritersHeldBy(f.ctx, t, f.db); got != 1 {
		t.Fatalf("shared (5318,2) holders after open = %d, want only the pool's session (the probe must be released)", got)
	}
	events := logEvents(t, logs)
	waiting := events["bootstrap.postgres.ownership.waiting"]
	if len(waiting) == 0 || waiting[0]["lock"] != "deferred_writers" {
		t.Fatalf("ownership.waiting events = %v, want one with lock=deferred_writers", waiting)
	}
	if holder, _ := waiting[0]["holder"].(string); !strings.Contains(holder, "ExclusiveLock") {
		t.Fatalf("waiting holder = %q, want the rebuild's ExclusiveLock session", holder)
	}
	if len(events["bootstrap.postgres.ownership.acquired"]) != 1 {
		t.Fatalf("ownership.acquired events = %v, want one", events["bootstrap.postgres.ownership.acquired"])
	}
}

// TestDeferredPoolFailsNamingTheHolderLive (proof B): a holder that outlasts
// the ownership wait fails the open with an error that names the deferred
// writers and the holder, never a ping error, and leaves no probe behind.
func TestDeferredPoolFailsNamingTheHolderLive(t *testing.T) {
	f := newRepairFixture(t)
	release := holdBothLocksExclusive(t, f.ctx, f.db)
	defer release()

	getenv := fixtureGetenv(t, f, nil)
	pool, err := OpenDeferredWriterPool(f.ctx, productionDeferredOptions(getenv, NewRunFence(nil), 2*time.Second, nil))
	if err == nil {
		_ = pool.Close()
		t.Fatal("OpenDeferredWriterPool behind a holder past the ownership wait = nil error")
	}
	for _, want := range []string{"secret lines deferred writers", "ExclusiveLock", "pid="} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "ping postgres") {
		t.Errorf("error %q is a ping error, want the ownership failure", err)
	}
	release()
	if got := sharedWritersHeldBy(f.ctx, t, f.db); got != 0 {
		t.Fatalf("shared (5318,2) holders after the refused open = %d, want 0 (no probe left behind)", got)
	}
}

// TestDeferredPoolUnreachableDatabaseFailsWithinThePingTimeoutLive (proof C):
// the ownership wait must not slow the report of a dead database. The probe
// pool's own ping fails within ESHU_POSTGRES_PING_TIMEOUT, before any wait.
func TestDeferredPoolUnreachableDatabaseFailsWithinThePingTimeoutLive(t *testing.T) {
	if os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN") == "" {
		t.Skip("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN not set; skipping the live secret lines proofs")
	}
	getenv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return "postgres://nobody:nothing@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"
		case "ESHU_POSTGRES_PING_TIMEOUT":
			return "1s"
		}
		return ""
	}
	started := time.Now()
	pool, err := OpenDeferredWriterPool(context.Background(), productionDeferredOptions(getenv, NewRunFence(nil), 3*time.Minute, nil))
	if err == nil {
		_ = pool.Close()
		t.Fatal("OpenDeferredWriterPool on an unreachable database = nil error")
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("unreachable database reported after %s, want under 3 s (ping timeout 1 s)", elapsed)
	}
	if !strings.Contains(err.Error(), "ping postgres") {
		t.Fatalf("error %q does not name the ping", err)
	}
}

// TestRepairerSkipsWhileTheProbeHoldsThenBootstrapProceedsLive (proof D): a
// Repairer that ticks while the probe holds (5318,2) shared skips with
// reason=deferred_writers; after the open, the run lock is acquired.
func TestRepairerSkipsWhileTheProbeHoldsThenBootstrapProceedsLive(t *testing.T) {
	f := newRepairFixture(t)
	probePool := stdlib.OpenDB(*f.config)
	probePool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = probePool.Close() })
	probe, err := AcquireDeferredWriterProbe(f.ctx, probePool, ProbeOptions{Wait: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := f.repairer(RepairOptions{}).Tick(f.ctx)
	if err != nil || outcome != RepairSkippedLocked {
		t.Fatalf("Tick while the probe holds (5318,2) = %s, %v; want skipped_locked", outcome, err)
	}
	skipped := f.events(t)["secret_lines.repair_skipped"]
	if len(skipped) != 1 || skipped[0]["reason"] != "deferred_writers" {
		t.Fatalf("repair_skipped events = %v, want one with reason=deferred_writers", skipped)
	}
	if state, _ := stateRow(f.ctx, t, f.db); state == "ready" {
		t.Fatalf("state = %s, the Repairer must not have published", state)
	}
	if err := probe.Release(f.ctx); err != nil {
		t.Fatal(err)
	}

	fence := NewRunFence(nil)
	pool := openFencedPool(t, f.config, fence)
	lock, err := AcquireBulkLoadLock(f.ctx, pool, LockOptions{Wait: 5 * time.Second, Poll: 100 * time.Millisecond, Fence: fence})
	if err != nil {
		t.Fatalf("AcquireBulkLoadLock after the probe released: %v", err)
	}
	if err := lock.Release(f.ctx); err != nil {
		t.Fatal(err)
	}
}

// TestDeferredProbeUncertainUnlockIsDiscardedLive (proof E): a probe whose
// unlock reports the lock was not held is discarded, so the next connection
// from a one-connection pool that keeps one idle is a different backend.
func TestDeferredProbeUncertainUnlockIsDiscardedLive(t *testing.T) {
	f := newRepairFixture(t)
	pool := stdlib.OpenDB(*f.config)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	probe, err := AcquireDeferredWriterProbe(f.ctx, pool, ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.conn.ExecContext(f.ctx, `SELECT pg_advisory_unlock_all()`); err != nil {
		t.Fatal(err)
	}
	before := probe.pid
	if err := probe.Release(f.ctx); err == nil || !strings.Contains(err.Error(), "no longer held") {
		t.Fatalf("Release after the lock vanished = %v, want the not-held error", err)
	}
	conn, err := pool.Conn(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	var after int
	if err := conn.QueryRowContext(f.ctx, `SELECT pg_backend_pid()`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatalf("the next connection is backend %d, the uncertain probe session; it must be discarded", after)
	}
}

// TestDeferredPoolReleasesTheProbeWhenOpeningFailsLive (proof E): the probe is
// released on the error path of the pool open too, and the open error is the
// one returned.
func TestDeferredPoolReleasesTheProbeWhenOpeningFailsLive(t *testing.T) {
	f := newRepairFixture(t)
	opened := errors.New("open deferred writer pool: refused")
	options := productionDeferredOptions(fixtureGetenv(t, f, nil), NewRunFence(nil), 5*time.Second, nil)
	options.Open = func(ctx context.Context) (*sql.DB, error) {
		if got := sharedWritersHeldBy(ctx, t, f.db); got != 1 {
			t.Errorf("shared (5318,2) holders while opening = %d, want the probe", got)
		}
		return nil, opened
	}
	pool, err := OpenDeferredWriterPool(f.ctx, options)
	if pool != nil {
		_ = pool.Close()
	}
	if !errors.Is(err, opened) {
		t.Fatalf("OpenDeferredWriterPool = %v, want the open error", err)
	}
	if got := sharedWritersHeldBy(f.ctx, t, f.db); got != 0 {
		t.Fatalf("shared (5318,2) holders after a failed open = %d, want 0", got)
	}
}
