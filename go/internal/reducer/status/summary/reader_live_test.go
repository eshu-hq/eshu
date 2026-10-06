// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// ageTolerance absorbs the float rounding between the reader adding the row's
// age to a stored decimal and the live statement computing the same age in
// numeric. It is far below the 20 ms the tests wait, so a dropped age add still
// fails the comparison.
const ageTolerance = time.Microsecond

// snapshotPair reads the status snapshot twice inside one REPEATABLE READ
// READ ONLY transaction: once through the stored-summary reader and once live
// at the instant the reader reported (as_of + age), so both describe the same
// data and the same clock. Only the active-work sections are compared.
func snapshotPair(ctx context.Context, t *testing.T, database *sql.DB) (model, live statuspkg.RawSnapshot) {
	t.Helper()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatalf("begin snapshot: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	queryer := txQueryer{tx}
	selection := statuspkg.SnapshotSelection{}.WithoutTerraformStateEvidence()

	reader := postgres.NewStatusStore(queryer).WithSummaryRead(store.ReadConfig{Enabled: true, StaleAfter: time.Hour})
	model, err = reader.ReadStatusSnapshotFiltered(ctx, time.Now(), selection)
	if err != nil {
		t.Fatalf("read through the stored summary: %v", err)
	}
	source := model.ActiveWorkSource
	if source.Source != statuspkg.ActiveWorkSourceModel || source.Reason != statuspkg.ActiveWorkReasonFresh {
		t.Fatalf("active_work_source = %+v, want model/fresh (the row must be served for the comparison to mean anything)", source)
	}
	off := postgres.NewStatusStore(queryer).WithSummaryRead(store.ReadConfig{})
	live, err = off.ReadStatusSnapshotFiltered(ctx, source.AsOf.Add(source.Age), selection)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	return model, live
}

// assertActiveWorkEqual fails unless the active-work sections are equal, with
// ages equal within ageTolerance and every other field exactly equal.
func assertActiveWorkEqual(t *testing.T, got, want statuspkg.RawSnapshot) {
	t.Helper()
	near := func(a, b time.Duration) bool {
		d := a - b
		return d <= ageTolerance && d >= -ageTolerance
	}
	if !near(got.Queue.OldestOutstandingAge, want.Queue.OldestOutstandingAge) {
		t.Fatalf("queue oldest age = %v, live = %v", got.Queue.OldestOutstandingAge, want.Queue.OldestOutstandingAge)
	}
	g, w := got.Queue, want.Queue
	g.OldestOutstandingAge, w.OldestOutstandingAge = 0, 0
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("queue = %+v, live = %+v", g, w)
	}
	if len(got.DomainBacklogs) != len(want.DomainBacklogs) || len(got.QueueBlockages) != len(want.QueueBlockages) {
		t.Fatalf("backlogs/blockages = %d/%d, live = %d/%d",
			len(got.DomainBacklogs), len(got.QueueBlockages), len(want.DomainBacklogs), len(want.QueueBlockages))
	}
	for i := range got.DomainBacklogs {
		a, b := got.DomainBacklogs[i], want.DomainBacklogs[i]
		if !near(a.OldestAge, b.OldestAge) {
			t.Fatalf("backlog %d oldest age = %v, live = %v", i, a.OldestAge, b.OldestAge)
		}
		a.OldestAge, b.OldestAge = 0, 0
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("backlog %d = %+v, live = %+v", i, a, b)
		}
	}
	for i := range got.QueueBlockages {
		a, b := got.QueueBlockages[i], want.QueueBlockages[i]
		if !near(a.OldestAge, b.OldestAge) {
			t.Fatalf("blockage %d oldest age = %v, live = %v", i, a.OldestAge, b.OldestAge)
		}
		a.OldestAge, b.OldestAge = 0, 0
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("blockage %d = %+v, live = %+v", i, a, b)
		}
	}
	if !reflect.DeepEqual(got.StageCounts, want.StageCounts) {
		t.Fatalf("stage counts = %+v, live = %+v", got.StageCounts, want.StageCounts)
	}
	if !reflect.DeepEqual(got.LatestQueueFailure, want.LatestQueueFailure) {
		t.Fatalf("latest failure = %+v, live = %+v", got.LatestQueueFailure, want.LatestQueueFailure)
	}
}

// TestReaderServesWhatTheWriterStoredEqualToLiveLive is D3.2 item 1 on the
// reader side, and it closes the round-trip deviation PR-A recorded: a row
// written by the production writer pass, read back by the production reader in
// a snapshot transaction, equals the live statement's answer at the same data
// and clock, at 0.2 %, 50 %, and 100 % live work and with every input deleted.
// The age advance is load-bearing here: the writer pass ran at least 20 ms
// before the read, the ages are positive, and a reader that dropped the add
// reports the stored ages and fails the comparison. The last case shows the
// comparison can differ.
func TestReaderServesWhatTheWriterStoredEqualToLiveLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	writer := newLiveWriter(database)
	const total = 600
	for _, state := range []struct {
		name string
		live int
	}{
		{"0.2 percent live", 1},
		{"50 percent live", total / 2},
		{"100 percent live", total},
	} {
		t.Run(state.name, func(t *testing.T) {
			seedWork(ctx, t, database, total, state.live)
			if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
				t.Fatalf("RunOnce() = %+v, want ok", pass)
			}
			time.Sleep(30 * time.Millisecond)
			model, live := snapshotPair(ctx, t, database)
			if model.ActiveWorkSource.Age < 30*time.Millisecond {
				t.Fatalf("row age = %v, want at least the 30 ms the test waited", model.ActiveWorkSource.Age)
			}
			if model.Queue.OldestOutstandingAge <= 0 {
				t.Fatalf("queue oldest age = %v; the fixture must have an outstanding item for the age add to matter", model.Queue.OldestOutstandingAge)
			}
			assertActiveWorkEqual(t, model, live)
			if live.ActiveWorkSource.Source != statuspkg.ActiveWorkSourceLive {
				t.Fatalf("the comparison snapshot was not live: %+v", live.ActiveWorkSource)
			}

			// Non-vacuity: change one outstanding row after the pass; the
			// stored row cannot know, so the equality must now fail.
			mustExec(ctx, t, database, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = 'w-0'`)
			model, live = snapshotPair(ctx, t, database)
			if reflect.DeepEqual(model.Queue.Outstanding, live.Queue.Outstanding) && reflect.DeepEqual(model.StageCounts, live.StageCounts) {
				t.Fatal("the comparison could not see a changed work item")
			}
		})
	}
	t.Run("empty", func(t *testing.T) {
		mustExec(ctx, t, database, `DELETE FROM fact_work_items`)
		mustExec(ctx, t, database, `DELETE FROM shared_projection_intents`)
		mustExec(ctx, t, database, `DELETE FROM shared_projection_partition_leases`)
		if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
			t.Fatalf("RunOnce() = %+v, want ok", pass)
		}
		time.Sleep(30 * time.Millisecond)
		model, live := snapshotPair(ctx, t, database)
		assertActiveWorkEqual(t, model, live)
		if model.Queue.OldestOutstandingAge != 0 {
			t.Fatalf("an empty queue reports oldest age %v; a zero age must stay zero", model.Queue.OldestOutstandingAge)
		}
	})
}

// TestReaderFallsBackAndNeverMixesWhenTheRowIsStaleLive proves the reader
// against a real row and the production live statement: a row older than the
// limit is not served, the live statement answers the whole active-work part,
// and the report says why. A row from another statement digest, and a missing
// table, behave the same without aborting the snapshot transaction.
func TestReaderFallsBackAndNeverMixesWhenTheRowIsStaleLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	writer := newLiveWriter(database)
	seedWork(ctx, t, database, 200, 100)
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	selection := statuspkg.SnapshotSelection{}.WithoutTerraformStateEvidence()
	readWith := func(staleAfter time.Duration) statuspkg.RawSnapshot {
		tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		snapshot, err := postgres.NewStatusStore(txQueryer{tx}).
			WithSummaryRead(store.ReadConfig{Enabled: true, StaleAfter: staleAfter}).
			ReadStatusSnapshotFiltered(ctx, time.Now(), selection)
		if err != nil {
			t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
		}
		return snapshot
	}

	// A write after the pass makes the stored row differ from live.
	mustExec(ctx, t, database, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id IN ('w-0', 'w-1', 'w-2')`)
	time.Sleep(50 * time.Millisecond)

	served := readWith(time.Hour)
	if served.ActiveWorkSource.Source != statuspkg.ActiveWorkSourceModel {
		t.Fatalf("source = %+v, want the stored row served under a one hour limit", served.ActiveWorkSource)
	}
	rejected := readWith(10 * time.Millisecond)
	if got := rejected.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != statuspkg.ActiveWorkReasonStale || !got.Stale {
		t.Fatalf("source = %+v, want live_fallback/stale", got)
	}
	// The fallback is the live answer, not the stored one: three items finished.
	if rejected.Queue.Outstanding != served.Queue.Outstanding-3 {
		t.Fatalf("fallback outstanding = %d, stored = %d; the fallback must be the live count, 3 lower", rejected.Queue.Outstanding, served.Queue.Outstanding)
	}

	mustExec(ctx, t, database, `UPDATE status_summary_snapshots SET source_sha256 = 'another-statement'`)
	if got := readWith(time.Hour).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != statuspkg.ActiveWorkReasonVersion {
		t.Fatalf("source = %+v, want live_fallback/version for a row another statement wrote", got)
	}
	mustExec(ctx, t, database, `DROP TABLE status_summary_snapshots`)
	if got := readWith(time.Hour).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLiveFallback || got.Reason != statuspkg.ActiveWorkReasonNotInstalled {
		t.Fatalf("source = %+v, want live_fallback/not_installed with the table dropped", got)
	}
}
