// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Sleeps the phase fake injects. Every phase gets its own so a phase that
// is measured around the wrong call, or not at all, cannot pass by luck.
const (
	sleepEnsureCursor = 30 * time.Millisecond
	sleepBegin        = 12 * time.Millisecond
	sleepLocks        = 14 * time.Millisecond
	sleepStatement    = 20 * time.Millisecond
	sleepAdvance      = 16 * time.Millisecond
	sleepCommit       = 50 * time.Millisecond
)

// sleepingDB is a scriptedDB whose calls sleep per phase.
type sleepingDB struct {
	scriptedDB
	// sameGeneration makes the cursor already describe the activating
	// generation, so LinkNext advances without a link statement.
	sameGeneration bool
}

func (d *sleepingDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	switch query {
	case ensureCursorQuery:
		time.Sleep(sleepEnsureCursor)
	case advanceCursorQuery:
		time.Sleep(sleepAdvance)
	}
	return d.scriptedDB.ExecContext(ctx, query, args...)
}

func (d *sleepingDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	switch query {
	case lockCursorQuery:
		if d.sameGeneration {
			return &scriptedRows{values: []any{"g1", int64(1), int16(DigestVersion), int64(0), 0, nil}}, nil
		}
	case lockGenerationQuery:
		time.Sleep(sleepLocks)
	case IncrementalLinkSQL:
		time.Sleep(sleepStatement)
	}
	return d.scriptedDB.QueryContext(ctx, query, args...)
}

func (d *sleepingDB) Begin(context.Context) (db.Transaction, error) {
	time.Sleep(sleepBegin)
	return &sleepingTx{db: d}, nil
}

// sleepingTx routes statements through the sleeping database and sleeps in
// Commit.
type sleepingTx struct{ db *sleepingDB }

func (t *sleepingTx) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return t.db.ExecContext(ctx, q, a...)
}

func (t *sleepingTx) QueryContext(ctx context.Context, q string, a ...any) (db.Rows, error) {
	return t.db.QueryContext(ctx, q, a...)
}

func (t *sleepingTx) Commit() error {
	time.Sleep(sleepCommit)
	t.db.committed = true
	return nil
}
func (t *sleepingTx) Rollback() error { return nil }

// maxUnattributed bounds the time a link may spend outside every named
// phase: context setup and function-call overhead, never I/O.
const maxUnattributed = 20 * time.Millisecond

// TestLinkNextRecordsPhaseTimings is the unit proof of #9275: LinkNext splits
// its Duration into the phases it spends it in, so a slow link on a hosted
// runner names its phase instead of a single total. Each phase is at least
// the sleep injected into its own call, the phases never sum past Duration,
// and the unattributed remainder is small.
func TestLinkNextRecordsPhaseTimings(t *testing.T) {
	d := &sleepingDB{scriptedDB: scriptedDB{linkRow: linkRow(0, 0, 0, 0, 0)}}
	result, err := NewLinkWriter(d).LinkNext(context.Background(), "scope")
	if err != nil {
		t.Fatalf("LinkNext: %v", err)
	}
	if result.Kind != LinkKindIncremental {
		t.Fatalf("LinkNext = %+v, want an incremental link", result)
	}
	for _, phase := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"EnsureCursor", result.Phases.EnsureCursor, sleepEnsureCursor},
		{"Begin", result.Phases.Begin, sleepBegin},
		{"Locks", result.Phases.Locks, sleepLocks},
		{"Statement", result.Phases.Statement, sleepStatement},
		{"Advance", result.Phases.Advance, sleepAdvance},
		{"Commit", result.Phases.Commit, sleepCommit},
	} {
		if phase.got < phase.want {
			t.Errorf("Phases.%s = %s, want at least the %s the call slept", phase.name, phase.got, phase.want)
		}
	}
	sum := result.Phases.Sum()
	if sum > result.Duration {
		t.Errorf("phases sum to %s, more than Duration %s", sum, result.Duration)
	}
	if rest := result.Duration - sum; rest >= maxUnattributed {
		t.Errorf("Duration %s leaves %s outside every phase, want under %s; phases %+v",
			result.Duration, rest, maxUnattributed, result.Phases)
	}
}

// TestLinkNextPhasesOnEarlyExits covers the paths that advance the cursor
// without a link statement: the phases still name where the time went, and
// Statement stays zero because none ran.
func TestLinkNextPhasesOnEarlyExits(t *testing.T) {
	d := &sleepingDB{scriptedDB: scriptedDB{linkRow: linkRow(0, 0, 0, 0, 0)}, sameGeneration: true}
	result, err := NewLinkWriter(d).LinkNext(context.Background(), "scope")
	if err != nil {
		t.Fatalf("LinkNext: %v", err)
	}
	if result.Kind != LinkKindNone {
		t.Fatalf("LinkNext = %+v, want no link for the activation of the state generation", result)
	}
	if result.Phases.Statement != 0 {
		t.Errorf("Phases.Statement = %s, want 0: no link statement ran", result.Phases.Statement)
	}
	if result.Phases.Advance < sleepAdvance || result.Phases.Commit < sleepCommit {
		t.Errorf("early exit phases %+v, want Advance and Commit to carry their sleeps", result.Phases)
	}
	if sum := result.Phases.Sum(); sum > result.Duration {
		t.Errorf("phases sum to %s, more than Duration %s", sum, result.Duration)
	}
}
