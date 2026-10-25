// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	secretlines "github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
)

var errRecordedOpen = errors.New("recorded open")

// recordingPostgresOpener records which opener openBootstrapDB's pools use.
type recordingPostgresOpener struct {
	plainCalls  int
	hookedCalls int
	hooks       []inventory.ConnectHook
	sessionSQL  []string
}

func (r *recordingPostgresOpener) Open(context.Context, func(string) string) (*sql.DB, error) {
	r.plainCalls++
	return nil, errRecordedOpen
}

func (r *recordingPostgresOpener) OpenWithSessionHooks(_ context.Context, _ func(string) string, hooks []inventory.ConnectHook, extraSessionSQL ...string) (*sql.DB, error) {
	r.hookedCalls++
	r.hooks = hooks
	r.sessionSQL = extraSessionSQL
	return nil, errRecordedOpen
}

// openBootstrapDB is the production composition, and CI runs no live test of it
// (bootstrap_db_live_test.go is a scheduled legacy file). This test runs the
// real function against a recording opener and a captured pool opener, so a
// production change that drops the run fence's hook from the writer pool
// (#8833 F1) or hooks the probe pool (F8) fails here, in the CI-run tests.
func TestOpenBootstrapDBComposesAPlainProbeAndAFencedWriterPool(t *testing.T) {
	opener := &recordingPostgresOpener{}
	var captured secretlines.DeferredPoolOptions
	prevOpener, prevOpen := bootstrapPostgresOpener, openDeferredWriterPool
	t.Cleanup(func() { bootstrapPostgresOpener, openDeferredWriterPool = prevOpener, prevOpen })
	bootstrapPostgresOpener = opener
	openDeferredWriterPool = func(_ context.Context, options secretlines.DeferredPoolOptions) (*sql.DB, error) {
		captured = options
		return nil, errRecordedOpen
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	const wait = 11 * time.Second
	if _, err := openBootstrapDB(context.Background(), func(string) string { return "" }, logger, wait); !errors.Is(err, errRecordedOpen) {
		t.Fatalf("openBootstrapDB err = %v, want the captured opener's error", err)
	}
	if captured.Probe == nil || captured.Open == nil {
		t.Fatal("openBootstrapDB did not hand OpenDeferredWriterPool a Probe and an Open")
	}
	if captured.Wait != wait || captured.Logger != logger {
		t.Fatalf("Wait/Logger = %v/%v, want the ownership wait and the logger passed in", captured.Wait, captured.Logger)
	}

	if _, err := captured.Probe(context.Background()); !errors.Is(err, errRecordedOpen) {
		t.Fatalf("Probe err = %v, want the recording opener's error (production must open the probe through the opener)", err)
	}
	if opener.plainCalls != 1 || opener.hookedCalls != 0 {
		t.Fatalf("Probe used plain=%d hooked=%d, want the plain opener only: a hooked probe waits inside the ping (F8)", opener.plainCalls, opener.hookedCalls)
	}

	if _, err := captured.Open(context.Background()); !errors.Is(err, errRecordedOpen) {
		t.Fatalf("Open err = %v, want the recording opener's error (production must open the pool through the opener)", err)
	}
	if opener.hookedCalls != 1 || len(opener.hooks) != 1 {
		t.Fatalf("writer pool hooked=%d hooks=%d, want the hooked opener once with exactly the run fence's hook (F1)", opener.hookedCalls, len(opener.hooks))
	}
	hookName := runtime.FuncForPC(reflect.ValueOf(opener.hooks[0]).Pointer()).Name()
	if !strings.Contains(hookName, "(*RunFence).Hook") {
		t.Fatalf("writer pool hook = %s, want the run fence's Hook", hookName)
	}
	if len(opener.sessionSQL) != 1 || opener.sessionSQL[0] != secretlines.DeferredSessionSQL {
		t.Fatalf("writer pool session SQL = %q, want DeferredSessionSQL", opener.sessionSQL)
	}
}

// stubConnector lets sql.OpenDB return a pool without dialing: openBootstrapDB
// only wraps it, and the test never runs a query.
type stubConnector struct{}

func (stubConnector) Connect(context.Context) (driver.Conn, error) { return nil, errRecordedOpen }
func (stubConnector) Driver() driver.Driver                        { return nil }

// The fence the writer pool's hook is built from (captured.Fence) must be the
// fence the run lock arms (LockOptions.Fence from secretLinesLockInputs). Two
// different fences stay green everywhere else and leave the birth fence
// silently unarmed (#8833 F18).
func TestOpenBootstrapDBHandsTheLockTheFenceTheWriterPoolHookUses(t *testing.T) {
	var captured secretlines.DeferredPoolOptions
	prevOpener, prevOpen := bootstrapPostgresOpener, openDeferredWriterPool
	t.Cleanup(func() { bootstrapPostgresOpener, openDeferredWriterPool = prevOpener, prevOpen })
	bootstrapPostgresOpener = &recordingPostgresOpener{}
	openDeferredWriterPool = func(_ context.Context, options secretlines.DeferredPoolOptions) (*sql.DB, error) {
		captured = options
		return sql.OpenDB(stubConnector{}), nil
	}

	database, err := openBootstrapDB(context.Background(), func(string) string { return "" }, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
	if err != nil {
		t.Fatalf("openBootstrapDB err = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if captured.Fence == nil {
		t.Fatal("openBootstrapDB handed OpenDeferredWriterPool no Fence: the hook's fence cannot be tied to the lock's")
	}
	_, lockOptions, err := secretLinesLockInputs(database, nil, time.Second)
	if err != nil {
		t.Fatalf("secretLinesLockInputs err = %v", err)
	}
	if lockOptions.Fence != captured.Fence {
		t.Fatalf("lock fence = %p, writer pool hook fence = %p: the birth fence would never arm", lockOptions.Fence, captured.Fence)
	}
}
