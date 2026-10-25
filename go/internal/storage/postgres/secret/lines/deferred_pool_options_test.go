// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// recordingOpener records which opener each pool of the deferred-writer
// composition used and with which hooks and session SQL.
type recordingOpener struct {
	plainCalls  int
	hookedCalls int
	hooks       []inventory.ConnectHook
	sessionSQL  []string
}

var errRecorded = errors.New("recorded")

func (r *recordingOpener) Open(_ context.Context, _ func(string) string) (*sql.DB, error) {
	r.plainCalls++
	return nil, errRecorded
}

func (r *recordingOpener) OpenWithSessionHooks(_ context.Context, _ func(string) string, hooks []inventory.ConnectHook, extraSessionSQL ...string) (*sql.DB, error) {
	r.hookedCalls++
	r.hooks = hooks
	r.sessionSQL = extraSessionSQL
	return nil, errRecorded
}

// BootstrapDeferredPoolOptions is the one composition production
// (bootstrap-index openBootstrapDB) and the enrolled live tests share. Its
// probe pool must be plain (a hooked probe reopens #8833 F8: the probe would
// wait inside the ping) and its writer pool must carry the run fence's hook and
// DeferredSessionSQL (dropping the hook reopens F1: sessions take no (5318,2)
// shared lock and get no birth fence).
func TestBootstrapDeferredPoolOptionsWiresPlainProbeAndFencedWriterPool(t *testing.T) {
	t.Parallel()
	opener := &recordingOpener{}
	getenv := func(string) string { return "" }
	fence := NewRunFence(nil)
	options := BootstrapDeferredPoolOptions(opener, getenv, fence, 7*time.Second, nil)

	if _, err := options.Probe(context.Background()); !errors.Is(err, errRecorded) {
		t.Fatalf("Probe err = %v, want the recording opener's error", err)
	}
	if opener.plainCalls != 1 || opener.hookedCalls != 0 {
		t.Fatalf("Probe used plain=%d hooked=%d, want the plain opener only", opener.plainCalls, opener.hookedCalls)
	}

	if _, err := options.Open(context.Background()); !errors.Is(err, errRecorded) {
		t.Fatalf("Open err = %v, want the recording opener's error", err)
	}
	if opener.plainCalls != 1 || opener.hookedCalls != 1 {
		t.Fatalf("Open used plain=%d hooked=%d, want the hooked opener once", opener.plainCalls, opener.hookedCalls)
	}
	if len(opener.hooks) != 1 {
		t.Fatalf("writer pool hooks = %d, want exactly the run fence's hook", len(opener.hooks))
	}
	// Inlining can give the hook another closure address, so identify it by its
	// symbol: RunFence.Hook's literal.
	hookName := runtime.FuncForPC(reflect.ValueOf(opener.hooks[0]).Pointer()).Name()
	if !strings.Contains(hookName, "(*RunFence).Hook") {
		t.Fatalf("writer pool hook = %s, want the run fence's Hook", hookName)
	}
	if len(opener.sessionSQL) != 1 || opener.sessionSQL[0] != DeferredSessionSQL {
		t.Fatalf("writer pool session SQL = %q, want DeferredSessionSQL", opener.sessionSQL)
	}
	// The hook above is fence.Hook(), so the fence the options expose must be
	// that same object. A different or nil-armed fence here would leave the
	// birth fence unarmed by the lock the caller builds from it (#8833 F18).
	if options.Fence != fence {
		t.Fatalf("options.Fence = %p, want the fence the builder was given (%p)", options.Fence, fence)
	}
	if options.Wait != 7*time.Second {
		t.Fatalf("Wait = %v, want the ownership wait passed in", options.Wait)
	}
}

// TestBootstrapDeferredPoolBirthHookFollowsOptionsFence pins, by behavior and
// without a DSN, that the hook the writer pool runs is built from the very
// fence the options expose (#8833 F21). The caller arms options.Fence through
// the run lock; a hook built from any other fence stays unarmed and would admit
// a session born after the lock session died.
func TestBootstrapDeferredPoolBirthHookFollowsOptionsFence(t *testing.T) {
	t.Parallel()
	opener := &recordingOpener{}
	options := BootstrapDeferredPoolOptions(opener, func(string) string { return "" }, NewRunFence(nil), time.Second, nil)
	if _, err := options.Open(context.Background()); !errors.Is(err, errRecorded) {
		t.Fatalf("Open err = %v, want the recording opener's error", err)
	}
	if len(opener.hooks) != 1 {
		t.Fatalf("writer pool hooks = %d, want exactly the run fence's hook", len(opener.hooks))
	}
	hook := opener.hooks[0]

	shim := &hookShim{}
	conn := shim.connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Before the run lock is published the hook admits the session but still
	// takes the (5318,2) shared lock.
	if err := hook(ctx, conn); err != nil {
		t.Fatalf("hook before the fence is armed = %v, want nil", err)
	}
	if got := shim.sharedLocks.Load(); got != 1 {
		t.Fatalf("shared lock statements = %d, want 1 (the hook takes (5318,2) at birth)", got)
	}

	// The lock session is published through options.Fence, as the run lock does.
	options.Fence.publish(4242, slog.New(slog.DiscardHandler))
	shim.held.Store(true)
	if err := hook(ctx, conn); err != nil {
		t.Fatalf("hook while the lock session holds the run lock = %v, want nil", err)
	}

	// The lock session dies: the next birth must be refused.
	shim.held.Store(false)
	if err := hook(ctx, conn); !errors.Is(err, ErrDeferredSessionRefused) {
		t.Fatalf("hook after the lock session is gone = %v, want ErrDeferredSessionRefused (the hook is not built from options.Fence)", err)
	}

	// Release clears the pid; the hook admits again, as it does for a run that
	// no longer owns the lock.
	options.Fence.clear()
	if err := hook(ctx, conn); err != nil {
		t.Fatalf("hook after the fence is cleared = %v, want nil", err)
	}
}
