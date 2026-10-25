// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	secretlines "github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
)

// connRecorder is a pinnable bootstrap database that records whether the lock
// tried to take a connection. Conn never succeeds: a test that reaches it has
// already passed the fence check.
type connRecorder struct {
	fakeBootstrapDB
	connCalls int
}

func (c *connRecorder) Conn(context.Context) (*sql.Conn, error) {
	c.connCalls++
	return nil, sql.ErrConnDone
}

// fencedRecorder additionally exposes the deferred-writer fence the production
// bootstrapSQLDB shares with its pool's connect hook.
type fencedRecorder struct {
	connRecorder
	fence *secretlines.RunFence
}

func (f *fencedRecorder) RunFence() *secretlines.RunFence { return f.fence }

// TestProductionSecretLinesLockFailsClosedWithoutTheBirthFence proves the
// production lock refuses to run when the bootstrap database cannot supply the
// run fence (#8833 PR2b review F10). A silent fallback would leave the birth
// fence unarmed: after the pinned lock session dies, pool sessions born later
// would write deferred rows that the Repairer then publishes ready over.
func TestProductionSecretLinesLockFailsClosedWithoutTheBirthFence(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)

	for name, database := range map[string]bootstrapDB{
		"no RunFence method":  &connRecorder{},
		"nil fence from pool": &fencedRecorder{},
	} {
		_, err := productionSecretLines().lock(context.Background(), database, logger, time.Second)
		if err == nil {
			t.Errorf("%s: lock = nil error, want a refusal to run its deferred writers unfenced", name)
			continue
		}
		if !strings.Contains(err.Error(), "fence") {
			t.Errorf("%s: lock error %q does not name the missing fence", name, err)
		}
		var calls int
		switch d := database.(type) {
		case *connRecorder:
			calls = d.connCalls
		case *fencedRecorder:
			calls = d.connCalls
		}
		if calls != 0 {
			t.Errorf("%s: lock pinned %d connection(s) before refusing; it must refuse first", name, calls)
		}
	}
}

// TestProductionSecretLinesLockHandsTheFenceToTheLock proves a database with a
// fence reaches the lock acquisition: the recorder's Conn error surfaces
// instead of the fence refusal.
func TestProductionSecretLinesLockHandsTheFenceToTheLock(t *testing.T) {
	t.Parallel()
	database := &fencedRecorder{fence: secretlines.NewRunFence(nil)}
	_, err := productionSecretLines().lock(context.Background(), database, slog.New(slog.DiscardHandler), time.Second)
	if err == nil {
		t.Fatal("lock = nil error, want the recorder's Conn failure")
	}
	if database.connCalls == 0 {
		t.Fatalf("lock never reached Conn: %v", err)
	}
}

// TestSecretLinesLockOptionsCarryTheFence proves the options handed to
// secretlines.AcquireBulkLoadLock hold the database's own fence, not a copy.
func TestSecretLinesLockOptionsCarryTheFence(t *testing.T) {
	t.Parallel()
	fence := secretlines.NewRunFence(nil)
	database := &fencedRecorder{fence: fence}
	_, options, err := secretLinesLockInputs(database, slog.New(slog.DiscardHandler), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if options.Fence != fence {
		t.Fatal("LockOptions.Fence is not the bootstrap database's fence")
	}
}
