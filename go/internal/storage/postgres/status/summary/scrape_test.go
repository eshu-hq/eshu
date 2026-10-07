// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// scrapeSelection is the selection the runtime /metrics scrape requests.
func scrapeSelection() statuspkg.SnapshotSelection {
	return statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence().WithStoredActiveWorkOnly()
}

// clockedQueryer answers the summary clock statement with a mutable database
// clock and passes every other statement to the scripted queryer, so one test
// can scrape the same process at several database times.
type clockedQueryer struct {
	*sourceQueryer
	clock atomic.Pointer[time.Time]
}

func newClockedQueryer(q *sourceQueryer, now time.Time) *clockedQueryer {
	c := &clockedQueryer{sourceQueryer: q}
	c.set(now)
	return c
}

func (c *clockedQueryer) set(now time.Time) { c.clock.Store(&now) }

func (c *clockedQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "to_regclass('status_summary_snapshots')") {
		c.clockReads.Add(1)
		return &readerRows{rows: [][]any{{*c.clock.Load(), c.installed}}}, nil
	}
	return c.sourceQueryer.QueryContext(ctx, query, args...)
}

func scrapeSnapshot(t *testing.T, store pgstatus.StatusStore) statuspkg.RawSnapshot {
	t.Helper()
	snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, scrapeSelection())
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	return snapshot
}

func TestScrapeServesAFreshRowWithoutTheLiveStatement(t *testing.T) {
	t.Parallel()

	storedAt := sourceTestNow.Add(-10 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	snapshot := scrapeSnapshot(t, readerStore(q, true))

	if q.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times on a scrape with a fresh row", q.liveRuns.Load())
	}
	got := snapshot.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh || got.Stale {
		t.Fatalf("source = %+v, want model/fresh, not stale", got)
	}
	if !got.AsOf.Equal(storedAt) || got.Age != 10*time.Second {
		t.Fatalf("as_of/age = %v/%v, want %v/10s", got.AsOf, got.Age, storedAt)
	}
	if snapshot.Queue.OldestOutstandingAge != 15*time.Second {
		t.Fatalf("queue oldest age = %v, want 5s stored + 10s age", snapshot.Queue.OldestOutstandingAge)
	}
}

// TestScrapeNeverRunsTheLiveStatementWhenTheRowCannotBeServed is the no-herd
// contract: every reason that makes an API read run the live statement must
// leave a scrape with zero live statements and a stale marker.
func TestScrapeNeverRunsTheLiveStatementWhenTheRowCannotBeServed(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	good := encodedEntries(t, goodEntries()...)
	for _, tc := range []struct {
		name   string
		q      *sourceQueryer
		reason string
	}{
		{"table not installed", &sourceQueryer{installed: false}, "not_installed"},
		{"row missing", &sourceQueryer{installed: true}, "missing"},
		{"schema version", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion+1, sha, 4, good)}, "version"},
		{"source digest", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, "other", 4, good)}, "version"},
		{"row count", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 9, good)}, "row_count"},
		{"stale", &sourceQueryer{installed: true, row: storedRow(sourceTestNow.Add(-34*time.Second), summary.SchemaVersion, sha, 4, good)}, "stale"},
		{"payload not decodable", &sourceQueryer{installed: true, row: storedRow(sourceTestAsOf.Add(-time.Second), summary.SchemaVersion, sha, 1, `{"x":1}`)}, "decode"},
	} {
		t.Run(tc.name+" with no earlier row serves the zero summary", func(t *testing.T) {
			t.Parallel()
			snapshot := scrapeSnapshot(t, readerStore(tc.q, true))
			if tc.q.liveRuns.Load() != 0 {
				t.Fatalf("live statement ran %d times on a scrape, want 0", tc.q.liveRuns.Load())
			}
			got := snapshot.ActiveWorkSource
			if got.Source != statuspkg.ActiveWorkSourceZero || got.Reason != tc.reason || !got.Stale {
				t.Fatalf("source = %+v, want zero/%s with stale=true", got, tc.reason)
			}
			if !got.AsOf.IsZero() || got.Age != 0 {
				t.Fatalf("as_of/age = %v/%v, want none: the zero summary has no row", got.AsOf, got.Age)
			}
			if snapshot.Queue != (statuspkg.QueueSnapshot{}) || len(snapshot.StageCounts) != 0 ||
				len(snapshot.DomainBacklogs) != 0 || len(snapshot.QueueBlockages) != 0 || snapshot.LatestQueueFailure != nil {
				t.Fatalf("the zero summary carried data: %+v", snapshot)
			}
		})
	}
}

// TestScrapeServesTheLastDecodedRowWithAnAdvancingAge proves the last row is
// served as stale, never as fresh: its age comes from the row's as_of and the
// database clock at each read, so it keeps growing, and no byte of the
// rejected row reaches the answer.
func TestScrapeServesTheLastDecodedRowWithAnAdvancingAge(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	storedAt := sourceTestNow.Add(-10 * time.Second)
	inner := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	q := newClockedQueryer(inner, sourceTestNow)
	store := readerStore(q, true)

	fresh := scrapeSnapshot(t, store)
	if fresh.ActiveWorkSource.Source != statuspkg.ActiveWorkSourceModel {
		t.Fatalf("first scrape = %+v, want model", fresh.ActiveWorkSource)
	}

	// The writer stops: the stored row is now far older than the limit and
	// carries a different count that a stale serve would show.
	other := []summary.Entry{
		{Section: "stage", Ordinal: 1, JSON: `{"stage":"reducer","status":"pending","count":999}`},
		{Section: "queue", Ordinal: 1, JSON: queueEntryJSON},
	}
	inner.row = storedRow(sourceTestNow.Add(-300*time.Second), summary.SchemaVersion, sha, 2, encodedEntries(t, other...))

	for _, advance := range []time.Duration{40 * time.Second, 100 * time.Second} {
		now := sourceTestNow.Add(advance)
		q.set(now)
		snapshot := scrapeSnapshot(t, store)
		got := snapshot.ActiveWorkSource
		wantAge := now.Sub(storedAt)
		if got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonStale || !got.Stale {
			t.Fatalf("scrape at +%v = %+v, want last_row/stale with stale=true", advance, got)
		}
		if !got.AsOf.Equal(storedAt) || got.Age != wantAge {
			t.Fatalf("scrape at +%v as_of/age = %v/%v, want %v/%v", advance, got.AsOf, got.Age, storedAt, wantAge)
		}
		// The served ages are the stored 5 s plus the age at this read.
		if want := 5*time.Second + wantAge; snapshot.Queue.OldestOutstandingAge != want {
			t.Fatalf("scrape at +%v queue oldest age = %v, want %v", advance, snapshot.Queue.OldestOutstandingAge, want)
		}
		if len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != 2 {
			t.Fatalf("scrape at +%v stages = %+v, want the last decoded row's count 2, never the rejected row's 999", advance, snapshot.StageCounts)
		}
	}
	if inner.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times, want 0", inner.liveRuns.Load())
	}
}

func TestScrapeServesTheLastRowWhenTheRowGoesMissing(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	storedAt := sourceTestNow.Add(-2 * time.Second)
	inner := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	store := readerStore(inner, true)
	scrapeSnapshot(t, store)
	inner.row = nil

	got := scrapeSnapshot(t, store).ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonMissing || !got.Stale {
		t.Fatalf("source = %+v, want last_row/missing, stale", got)
	}
	if got.Age != 2*time.Second {
		t.Fatalf("age = %v, want 2s from the held row's as_of and the database clock", got.Age)
	}
}

// TestScrapeDatabaseErrorFailsTheRead states the error contract: a failed
// clock or row read is an error, not a stale serve and not a live run.
func TestScrapeDatabaseErrorFailsTheRead(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	inner := &sourceQueryer{installed: true, row: storedRow(sourceTestNow.Add(-time.Second), summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	store := readerStore(inner, true)
	scrapeSnapshot(t, store)
	inner.clockErr = errors.New("clock read failed")

	_, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, scrapeSelection())
	if err == nil {
		t.Fatal("ReadStatusSnapshotFiltered() error = nil, want the clock read failure")
	}
	if inner.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times after a database error", inner.liveRuns.Load())
	}
}

// TestScrapeWithTheReaderOffIsTheLiveRead is the flag-off contract: the scrape
// selection changes nothing while the reader is off.
func TestScrapeWithTheReaderOffIsTheLiveRead(t *testing.T) {
	t.Parallel()

	q := &sourceQueryer{installed: true}
	snapshot := scrapeSnapshot(t, readerStore(q, false))

	if q.clockReads.Load() != 0 || q.modelReads.Load() != 0 {
		t.Fatalf("flag off issued %d clock and %d model reads, want none", q.clockReads.Load(), q.modelReads.Load())
	}
	if q.liveRuns.Load() != 1 {
		t.Fatalf("live statement ran %d times, want 1", q.liveRuns.Load())
	}
	got := snapshot.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceLive || got.Reason != statuspkg.ActiveWorkReasonFlagOff || got.Stale {
		t.Fatalf("source = %+v, want live/flag_off", got)
	}
}

// TestScrapeHoldsOneBoundedLastRowUnderConcurrentScrapes runs scrapes together
// under -race: nothing runs the live statement, every answer is one whole row,
// and a stale serve never reports itself fresh.
func TestScrapeHoldsOneBoundedLastRowUnderConcurrentScrapes(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	inner := &sourceQueryer{installed: true, row: storedRow(sourceTestNow.Add(-time.Second), summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	store := readerStore(inner, true)
	scrapeSnapshot(t, store)

	var wg sync.WaitGroup
	failures := make(chan string, 64)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				snapshot, err := store.ReadStatusSnapshotFiltered(context.Background(), sourceTestNow, scrapeSelection())
				if err != nil {
					failures <- err.Error()
					return
				}
				got := snapshot.ActiveWorkSource
				if got.Source == statuspkg.ActiveWorkSourceModel && got.Stale {
					failures <- "a fresh serve reported stale"
				}
				if got.Source != statuspkg.ActiveWorkSourceModel && !got.Stale {
					failures <- "a stale serve reported fresh"
				}
				if len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != 2 {
					failures <- "answer was not one whole row"
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
	if inner.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times, want 0", inner.liveRuns.Load())
	}
}
