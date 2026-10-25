// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"context"
	"database/sql"
	"errors"
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
	if options.Wait != 7*time.Second {
		t.Fatalf("Wait = %v, want the ownership wait passed in", options.Wait)
	}
}
