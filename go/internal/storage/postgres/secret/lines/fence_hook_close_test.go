// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestRefusedBirthClosesTheSessionAndFreesItsSharedLock proves a connection the
// fence refuses is closed on the server. pgx's stdlib connector returns the
// AfterConnect error without closing the session, so a refusal that relied on
// the driver left (5318,2) held until the process exited, and the Repairer kept
// yielding to a writer that no longer existed.
func TestRefusedBirthClosesTheSessionAndFreesItsSharedLock(t *testing.T) {
	shim := &hookShim{}
	fence := NewRunFence(nil)
	fence.publish(4242, slog.New(slog.DiscardHandler))
	option := inventory.WriterConnectOptionWithHooks([]inventory.ConnectHook{fence.Hook()})
	db := sql.OpenDB(stdlib.GetConnector(*shim.connConfig(t), option))
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := db.PingContext(ctx)
	if !errors.Is(err, ErrDeferredSessionRefused) {
		t.Fatalf("Ping error = %v, want ErrDeferredSessionRefused", err)
	}
	if got := shim.sharedLocks.Load(); got != 1 {
		t.Fatalf("shared lock requests = %d, want 1", got)
	}
	requireClosedSessions(t, shim, 1)
}

// TestRejectedSessionSQLClosesTheSession covers the other failure site of the
// connect option: a refused session statement happens before any hook ran, and
// the session must still be closed.
func TestRejectedSessionSQLClosesTheSession(t *testing.T) {
	shim := &hookShim{}
	shim.failSessionSQL.Store(true)
	option := inventory.WriterConnectOptionWithHooks(nil)
	db := sql.OpenDB(stdlib.GetConnector(*shim.connConfig(t), option))
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err == nil {
		t.Fatal("Ping succeeded, want the rejected session statement to fail the connection")
	}
	requireClosedSessions(t, shim, 1)
}

// TestRefusedBirthWithACancelledContextStillClosesTheSession pins the outcome
// when the connect context has ended before the hook refuses: the session still
// ends, with a graceful Terminate. It does not isolate the detached close
// context in the connect option; pgx ends the session either way, and the
// detached context only keeps the close independent of the connect deadline.
func TestRefusedBirthWithACancelledContextStillClosesTheSession(t *testing.T) {
	shim := &hookShim{}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	waitForContextThenRefuse := func(hookCtx context.Context, _ *pgx.Conn) error {
		<-hookCtx.Done()
		return fmt.Errorf("refused after the connect context ended: %w", hookCtx.Err())
	}
	option := inventory.WriterConnectOptionWithHooks([]inventory.ConnectHook{waitForContextThenRefuse})
	db := sql.OpenDB(stdlib.GetConnector(*shim.connConfig(t), option))
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(ctx); err == nil {
		t.Fatal("Ping succeeded, want the refused session to fail the connection")
	}
	requireClosedSessions(t, shim, 1)
	deadline := time.Now().Add(2 * time.Second)
	for shim.terminated.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := shim.terminated.Load(); got != 1 {
		t.Fatalf("graceful Terminate messages = %d, want 1: the close used the ended connect context", got)
	}
}

// requireClosedSessions waits for the shim to see want closed sessions.
func requireClosedSessions(t *testing.T, shim *hookShim, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for shim.closed.Load() < want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := shim.closed.Load(); got != want {
		t.Fatalf("closed sessions = %d, want %d: a refused session stayed open", got, want)
	}
}
