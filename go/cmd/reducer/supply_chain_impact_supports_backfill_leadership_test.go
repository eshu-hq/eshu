// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// supportsLockProbe is a driver.Connector whose connections answer only the
// backfill's advisory-lock statements. lock and unlock decide each answer; a
// non-nil error is returned instead of a row. Every statement is recorded so a
// test can prove which ones ran.
type supportsLockProbe struct {
	mu        sync.Mutex
	statement []string
	lock      func() (bool, error)
	unlock    func() (bool, error)
	attempted chan struct{}
}

func (p *supportsLockProbe) Connect(context.Context) (driver.Conn, error) {
	return &supportsLockProbeConn{probe: p}, nil
}

func (p *supportsLockProbe) Driver() driver.Driver { return supportsLockProbeDriver{} }

func (p *supportsLockProbe) statements() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.statement...)
}

type supportsLockProbeDriver struct{}

func (supportsLockProbeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("open through the connector")
}

type supportsLockProbeConn struct{ probe *supportsLockProbe }

func (c *supportsLockProbeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *supportsLockProbeConn) Close() error { return nil }

func (c *supportsLockProbeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transaction not supported")
}

func (c *supportsLockProbeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	p := c.probe
	p.mu.Lock()
	p.statement = append(p.statement, query)
	p.mu.Unlock()
	var reply func() (bool, error)
	switch {
	case strings.Contains(query, "pg_try_advisory_lock"):
		reply = p.lock
		if p.attempted != nil {
			defer func() {
				select {
				case p.attempted <- struct{}{}:
				default:
				}
			}()
		}
	case strings.Contains(query, "pg_advisory_unlock"):
		reply = p.unlock
	default:
		return nil, errors.New("unexpected statement: " + query)
	}
	granted, err := reply()
	if err != nil {
		return nil, err
	}
	return &supportsLockProbeRows{value: granted}, nil
}

var _ driver.QueryerContext = (*supportsLockProbeConn)(nil)

type supportsLockProbeRows struct {
	value bool
	done  bool
}

func (r *supportsLockProbeRows) Columns() []string { return []string{"granted"} }
func (r *supportsLockProbeRows) Close() error      { return nil }
func (r *supportsLockProbeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	dest[0] = r.value
	r.done = true
	return nil
}

func lockReply(granted bool) func() (bool, error) {
	return func() (bool, error) { return granted, nil }
}

// TestWithSupportsBackfillLeadershipElectsOneRunner proves the election: the
// pass runs only for the session that gets the advisory lock, a contended
// session skips without running or unlocking, and a lock that cannot be
// released is reported with the pass result instead of being dropped.
func TestWithSupportsBackfillLeadershipElectsOneRunner(t *testing.T) {
	t.Parallel()
	passErr := errors.New("pass failed")
	for name, tc := range map[string]struct {
		probe      *supportsLockProbe
		fn         func(context.Context) error
		wantRan    bool
		wantErr    string
		wantIsErr  error
		wantUnlock bool
	}{
		"elected runs the pass and releases the lock": {
			probe: &supportsLockProbe{lock: lockReply(true), unlock: lockReply(true)}, wantRan: true, wantUnlock: true,
		},
		"contended skips the pass and never unlocks": {
			probe: &supportsLockProbe{lock: lockReply(false), unlock: lockReply(true)},
		},
		"a failed pass is returned and the lock is still released": {
			probe: &supportsLockProbe{lock: lockReply(true), unlock: lockReply(true)},
			fn:    func(context.Context) error { return passErr }, wantRan: true, wantIsErr: passErr, wantUnlock: true,
		},
		"an unconfirmed acquire is an error and runs nothing": {
			probe:   &supportsLockProbe{lock: func() (bool, error) { return false, errors.New("connection reset") }, unlock: lockReply(true)},
			wantErr: "acquire supports backfill lock",
		},
		"a lock the session no longer holds is reported": {
			probe: &supportsLockProbe{lock: lockReply(true), unlock: lockReply(false)}, wantRan: true,
			wantErr: "session did not hold lock", wantUnlock: true,
		},
		"an unlock failure is reported next to a pass failure": {
			probe: &supportsLockProbe{lock: lockReply(true), unlock: func() (bool, error) { return false, errors.New("unlock reset") }},
			fn:    func(context.Context) error { return passErr }, wantRan: true,
			wantIsErr: passErr, wantErr: "release supports backfill lock", wantUnlock: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			database := sql.OpenDB(tc.probe)
			defer database.Close()
			fnRan := false
			ran, err := withSupportsBackfillLeadership(context.Background(), database, func(ctx context.Context) error {
				fnRan = true
				if tc.fn != nil {
					return tc.fn(ctx)
				}
				return nil
			})
			if ran != tc.wantRan || fnRan != tc.wantRan {
				t.Fatalf("ran = %v, fn ran = %v, want both %v", ran, fnRan, tc.wantRan)
			}
			if tc.wantErr == "" && tc.wantIsErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
			if tc.wantIsErr != nil && !errors.Is(err, tc.wantIsErr) {
				t.Fatalf("error = %v, want it to wrap %v", err, tc.wantIsErr)
			}
			unlocked := false
			for _, statement := range tc.probe.statements() {
				if strings.Contains(statement, "pg_advisory_unlock") {
					unlocked = true
				}
			}
			if unlocked != tc.wantUnlock {
				t.Fatalf("unlock ran = %v, want %v (statements %q)", unlocked, tc.wantUnlock, tc.probe.statements())
			}
		})
	}
}

// TestRunSupplyChainImpactSupportsBackfillStaysAliveAndStopsOnCancel proves the
// loop survives a contended election and a failed pass (it logs the failure and
// polls again) and returns the context error, not nil, when it is stopped.
func TestRunSupplyChainImpactSupportsBackfillStaysAliveAndStopsOnCancel(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		lock    func() (bool, error)
		wantLog string
	}{
		"contended": {lock: lockReply(false)},
		"failed acquire": {
			lock:    func() (bool, error) { return false, errors.New("connection reset") },
			wantLog: "supply chain impact supports backfill pass failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe := &supportsLockProbe{lock: tc.lock, unlock: lockReply(true), attempted: make(chan struct{}, 1)}
			database := sql.OpenDB(probe)
			defer database.Close()
			var logs syncBuffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runSupplyChainImpactSupportsBackfill(ctx, database, nil, logger) }()
			select {
			case <-probe.attempted:
			case err := <-done:
				t.Fatalf("backfill exited before its first election: %v", err)
			case <-ctx.Done():
				t.Fatal("backfill never tried the election")
			}
			// The probe signals from inside the lock statement, before the loop has
			// seen the failure, so a test that cancels at once can stop the loop
			// before it logs. Wait for the effect itself, bounded, before stopping.
			if tc.wantLog != "" {
				waitForSupportsBackfillLog(t, &logs, tc.wantLog, 5*time.Second)
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("backfill exit = %v, want context canceled", err)
			}
			if tc.wantLog == "" && strings.Contains(logs.String(), "level=ERROR") {
				t.Fatalf("a contended election must not log an error: %q", logs.String())
			}
		})
	}
}

// waitForSupportsBackfillLog polls logs until it contains want or the deadline
// passes, so a test asserts on an effect that has happened and not on the
// moment something upstream of it was signalled.
func waitForSupportsBackfillLog(t *testing.T, logs *syncBuffer, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !strings.Contains(logs.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("log = %q, want it to contain %q within %s", logs.String(), want, within)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestStartSupplyChainImpactSupportsBackfillWaitsAndLogsOnlyRealFailures proves
// the wait function blocks until the run returns (so shutdown closes the
// database after the backfill), and that an unexpected stop is logged while a
// stop caused by cancellation is not.
func TestStartSupplyChainImpactSupportsBackfillWaitsAndLogsOnlyRealFailures(t *testing.T) {
	t.Parallel()
	t.Run("waits for the run to return", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		wait := startSupplyChainImpactSupportsBackfill(context.Background(), func(context.Context) error {
			<-release
			return nil
		}, nil)
		waited := make(chan struct{})
		go func() { wait(); close(waited) }()
		select {
		case <-waited:
			t.Fatal("wait returned while the run was still going")
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		select {
		case <-waited:
		case <-time.After(5 * time.Second):
			t.Fatal("wait did not return after the run ended")
		}
	})
	t.Run("logs an unexpected stop", func(t *testing.T) {
		t.Parallel()
		var logs syncBuffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		boom := errors.New("backfill exploded")
		startSupplyChainImpactSupportsBackfill(context.Background(), func(context.Context) error { return boom }, logger)()
		if !strings.Contains(logs.String(), "supply chain impact supports backfill stopped") || !strings.Contains(logs.String(), "backfill exploded") {
			t.Fatalf("log = %q, want the stop and its cause", logs.String())
		}
	})
	t.Run("does not log a stop caused by cancellation", func(t *testing.T) {
		t.Parallel()
		var logs syncBuffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		startSupplyChainImpactSupportsBackfill(ctx, func(runCtx context.Context) error { return runCtx.Err() }, logger)()
		if logs.Len() != 0 {
			t.Fatalf("log = %q, want nothing for a cancelled run", logs.String())
		}
	})
}

// syncBuffer is a bytes.Buffer safe for a logger written from the backfill
// goroutine while the test reads it.
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

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}
