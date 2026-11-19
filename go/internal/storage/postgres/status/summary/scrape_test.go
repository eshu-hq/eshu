// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"errors"
	"reflect"
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
		{"stale and not decodable", &sourceQueryer{installed: true, row: storedRow(sourceTestNow.Add(-34*time.Second), summary.SchemaVersion, sha, 1, `{"x":1}`)}, "stale"},
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
// database clock at each read, so it keeps growing, and an older stale row
// never replaces a newer one the process holds.
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

	// The stored row is replaced by one that is older than the held row, far
	// past the limit, and carries a count a wrong serve would show.
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
			t.Fatalf("scrape at +%v stages = %+v, want the held row's count 2, never the older stale row's 999", advance, snapshot.StageCounts)
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

// TestScrapeWithTheReaderOffServesTheStoredRow is the #10872 no-herd contract:
// the StoredActiveWorkOnly scrape selection serves a stored row instead of
// the live statement, even with the reader flag off. A fresh row is served
// as model/fresh, a row past the staleness bound is served as last_row/stale
// with its counts frozen at as_of and its ages advanced. A missing row on a
// fresh process runs the live statement (reason flag_off) instead of
// presenting zeros — writer-off deployments have no stored row, and a zero
// queue would hide a real backlog.
func TestScrapeWithTheReaderOffServesTheStoredRow(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	storedAt := sourceTestNow.Add(-10 * time.Second)
	inner := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	q := newClockedQueryer(inner, sourceTestNow)
	store := readerStore(q, false)

	fresh := scrapeSnapshot(t, store)
	if inner.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times on a flag-off scrape with a fresh row, want 0", inner.liveRuns.Load())
	}
	got := fresh.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh || got.Stale {
		t.Fatalf("flag-off source = %+v, want model/fresh, not stale", got)
	}
	if !got.AsOf.Equal(storedAt) || got.Age != 10*time.Second {
		t.Fatalf("flag-off as_of/age = %v/%v, want %v/10s", got.AsOf, got.Age, storedAt)
	}
	if fresh.Queue != (statuspkg.QueueSnapshot{Total: 4, Outstanding: 2, Pending: 2, Succeeded: 2, OldestOutstandingAge: 15 * time.Second}) {
		t.Fatalf("flag-off queue = %+v, want the stored counts with the 5s age advanced by 10s", fresh.Queue)
	}

	// The writer stops: the row is past the 33s bound at the next scrape, so
	// the last row is served stale with its counts frozen and its ages grown.
	q.set(sourceTestNow.Add(100 * time.Second))
	stale := scrapeSnapshot(t, store)
	if inner.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times on a stale flag-off scrape, want 0", inner.liveRuns.Load())
	}
	got = stale.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonStale || !got.Stale {
		t.Fatalf("stale flag-off source = %+v, want last_row/stale", got)
	}
	if stale.Queue.Total != 4 || stale.Queue.Outstanding != 2 || stale.Queue.Pending != 2 || stale.Queue.Succeeded != 2 {
		t.Fatalf("stale flag-off queue counts = %+v, want the counts frozen at as_of", stale.Queue)
	}
	if want := 5*time.Second + 110*time.Second; stale.Queue.OldestOutstandingAge != want {
		t.Fatalf("stale flag-off queue oldest age = %v, want %v", stale.Queue.OldestOutstandingAge, want)
	}

	// No row on a process that never served one, with the reader flag off:
	// the scrape runs the live statement (reason flag_off) instead of
	// presenting a zero queue — writer-off deployments have no stored row,
	// and zeros would hide a real backlog (#10872).
	emptyQueryer := &sourceQueryer{installed: true}
	empty := readerStore(emptyQueryer, false)
	live := scrapeSnapshot(t, empty)
	got = live.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceLive || got.Reason != statuspkg.ActiveWorkReasonFlagOff || got.Stale {
		t.Fatalf("missing flag-off source = %+v, want live/flag_off, not stale", got)
	}
	if emptyQueryer.liveRuns.Load() != 1 {
		t.Fatalf("missing flag-off scrape ran the live statement %d times, want exactly 1", emptyQueryer.liveRuns.Load())
	}
	if live.Queue.Pending != 2 || live.Queue.Outstanding != 2 {
		t.Fatalf("missing flag-off queue = %+v, want the live pending/outstanding counts, not zeros", live.Queue)
	}
}

// TestScrapeWithTheReaderOffMatchesTheReaderOnServe pins that the flag changes
// nothing about the served values when a stored row is present: the same
// database state scraped with the reader off and on carries identical
// sections at the same as_of, so every acceptance output (waiting work,
// oldest age, retries, overdue and failed items) reads the same. (With no
// stored row the flag-off scrape runs the live statement instead; see the
// missing-row case above.) The stored
// fixture carries nonzero retrying, overdue, failed, and dead-letter counts,
// a backlog, a blockage, and the latest failure.
func TestScrapeWithTheReaderOffMatchesTheReaderOnServe(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	storedAt := sourceTestNow.Add(-10 * time.Second)
	entries := []summary.Entry{
		{Section: "backlog", Ordinal: 1, JSON: `{"domain":"repo_dependency","outstanding_count":3,"in_flight_count":1,"retrying_count":2,"dead_letter_count":1,"failed_count":1,"oldest_outstanding_age_seconds":42}`},
		{Section: "blockage", Ordinal: 1, JSON: `{"stage":"reducer","domain":"d","conflict_domain":"c","conflict_key":"k","blocked_count":2,"oldest_blocked_age_seconds":9}`},
		{Section: "queue", Ordinal: 1, JSON: `{"total_count":11,"outstanding_count":5,"pending_count":3,"in_flight_count":1,"retrying_count":2,"succeeded_count":4,"dead_letter_count":1,"failed_count":1,"provenance_edge_identity_upgrade_applied":false,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":42,"overdue_claim_count":2}`},
		{Section: "stage", Ordinal: 1, JSON: `{"stage":"reducer","status":"retrying","count":2}`},
		{Section: "failure", Ordinal: 0, JSON: `{"stage":"reducer","domain":"workload","status":"dead_letter","work_item_id":"w-1","scope_id":"s-1","generation_id":"g-1","failure_class":"boom","failure_message":"m","failure_details":"d","updated_at":"2026-10-06T12:00:00Z"}`},
	}
	payload := encodedEntries(t, entries...)
	row := func() []any {
		return storedRow(storedAt, summary.SchemaVersion, sha, len(entries), payload)
	}
	offQ := &sourceQueryer{installed: true, row: row()}
	onQ := &sourceQueryer{installed: true, row: row()}
	off := scrapeSnapshot(t, readerStore(offQ, false))
	on := scrapeSnapshot(t, readerStore(onQ, true))

	if offQ.liveRuns.Load() != 0 || onQ.liveRuns.Load() != 0 {
		t.Fatalf("live runs off=%d on=%d, want 0 on both", offQ.liveRuns.Load(), onQ.liveRuns.Load())
	}
	if off.ActiveWorkSource != on.ActiveWorkSource {
		t.Fatalf("flag-off source = %+v, flag-on source = %+v, want identical", off.ActiveWorkSource, on.ActiveWorkSource)
	}
	if off.Queue != on.Queue || !reflect.DeepEqual(off.StageCounts, on.StageCounts) ||
		!reflect.DeepEqual(off.DomainBacklogs, on.DomainBacklogs) || !reflect.DeepEqual(off.QueueBlockages, on.QueueBlockages) ||
		!reflect.DeepEqual(off.LatestQueueFailure, on.LatestQueueFailure) {
		t.Fatalf("flag-off snapshot differs from flag-on:\noff queue=%+v stages=%+v backlogs=%+v blockages=%+v failure=%+v\non  queue=%+v stages=%+v backlogs=%+v blockages=%+v failure=%+v",
			off.Queue, off.StageCounts, off.DomainBacklogs, off.QueueBlockages, off.LatestQueueFailure,
			on.Queue, on.StageCounts, on.DomainBacklogs, on.QueueBlockages, on.LatestQueueFailure)
	}
	if off.Queue.Outstanding != 5 || off.Queue.Pending != 3 || off.Queue.Retrying != 2 ||
		off.Queue.OverdueClaims != 2 || off.Queue.Failed != 1 || off.Queue.DeadLetter != 1 {
		t.Fatalf("flag-off queue = %+v, want the stored waiting/retry/overdue/failed counts", off.Queue)
	}
	if off.Queue.OldestOutstandingAge != 52*time.Second {
		t.Fatalf("flag-off queue oldest age = %v, want 42s stored + 10s age", off.Queue.OldestOutstandingAge)
	}
	if len(off.DomainBacklogs) != 1 || off.DomainBacklogs[0].Outstanding != 3 || off.DomainBacklogs[0].Retrying != 2 ||
		off.DomainBacklogs[0].OldestAge != 52*time.Second {
		t.Fatalf("flag-off backlogs = %+v, want the stored waiting/retrying counts with the age advanced", off.DomainBacklogs)
	}
	if len(off.QueueBlockages) != 1 || off.QueueBlockages[0].Blocked != 2 || off.QueueBlockages[0].OldestAge != 19*time.Second {
		t.Fatalf("flag-off blockages = %+v, want the stored blockage with its age advanced", off.QueueBlockages)
	}
	if off.LatestQueueFailure == nil || off.LatestQueueFailure.WorkItemID != "w-1" || off.LatestQueueFailure.FailureClass != "boom" {
		t.Fatalf("flag-off latest failure = %+v, want the stored w-1/boom failure", off.LatestQueueFailure)
	}
}

// TestScrapeWithTheReaderOffHonorsTheStalenessBound pins the exact freshness
// bound for a process that never enabled the reader: a row exactly
// stale_after old is still served fresh, one second past it is last_row.
func TestScrapeWithTheReaderOffHonorsTheStalenessBound(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	payload := encodedEntries(t, goodEntries()...)
	scrapeAt := func(age time.Duration) statuspkg.RawSnapshot {
		inner := &sourceQueryer{installed: true, row: storedRow(sourceTestNow.Add(-age), summary.SchemaVersion, sha, 4, payload)}
		return scrapeSnapshot(t, readerStore(newClockedQueryer(inner, sourceTestNow), false))
	}

	atBound := scrapeAt(33 * time.Second)
	if got := atBound.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Stale {
		t.Fatalf("row exactly 33s old source = %+v, want model, not stale", got)
	}
	pastBound := scrapeAt(34 * time.Second)
	if got := pastBound.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLastRow || !got.Stale || got.Age != 34*time.Second {
		t.Fatalf("row 34s old source = %+v, want last_row/stale at age 34s", got)
	}
	if pastBound.Queue.Total != 4 {
		t.Fatalf("row 34s old queue = %+v, want the stored counts served stale", pastBound.Queue)
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

// TestScrapeServesAStaleRowANewProcessNeverServed is the restart case: a
// process that starts while the writer is down finds only a stale row. The row
// is the newest it can decode, so it is served as last_row with stale=true, its
// real age, and its own counts with the ages advanced, never the zero summary.
// A stale row that cannot be trusted (it does not decode) still serves zero, and
// the API path on the same row still runs the live statement.
func TestScrapeServesAStaleRowANewProcessNeverServed(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	storedAt := sourceTestNow.Add(-100 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	snapshot := scrapeSnapshot(t, readerStore(q, true))

	got := snapshot.ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonStale || !got.Stale {
		t.Fatalf("source = %+v, want last_row/stale with stale=true", got)
	}
	if !got.AsOf.Equal(storedAt) || got.Age != 100*time.Second {
		t.Fatalf("as_of/age = %v/%v, want %v/100s", got.AsOf, got.Age, storedAt)
	}
	if want := 105 * time.Second; snapshot.Queue.OldestOutstandingAge != want {
		t.Fatalf("queue oldest age = %v, want 5s stored + 100s age", snapshot.Queue.OldestOutstandingAge)
	}
	if snapshot.Queue.Outstanding != 2 || len(snapshot.StageCounts) != 1 || snapshot.StageCounts[0].Count != 2 ||
		len(snapshot.DomainBacklogs) != 1 || len(snapshot.QueueBlockages) != 1 {
		t.Fatalf("the stale row's counts were not served: queue=%+v stages=%+v", snapshot.Queue, snapshot.StageCounts)
	}
	if q.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times, want 0", q.liveRuns.Load())
	}

	// The status route on the same stale row is unchanged: the live statement.
	api := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	apiSnapshot := readSnapshot(t, readerStore(api, true))
	if a := apiSnapshot.ActiveWorkSource; a.Source != statuspkg.ActiveWorkSourceLiveFallback || a.Reason != statuspkg.ActiveWorkReasonStale || a.Stale || api.liveRuns.Load() != 1 {
		t.Fatalf("status route on a stale row = %+v with %d live runs, want live_fallback/stale and 1", a, api.liveRuns.Load())
	}
}

// TestScrapeKeepsTheNewestStaleRowOverAnOlderHeldRow: a stale row newer than the
// held one replaces it, so the served age is the newest data's.
func TestScrapeKeepsTheNewestStaleRowOverAnOlderHeldRow(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	held := sourceTestNow.Add(-5 * time.Second)
	inner := &sourceQueryer{installed: true, row: storedRow(held, summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))}
	q := newClockedQueryer(inner, sourceTestNow)
	store := readerStore(q, true)
	scrapeSnapshot(t, store)

	newer := sourceTestNow.Add(10 * time.Second)
	inner.row = storedRow(newer, summary.SchemaVersion, sha, 4, encodedEntries(t, goodEntries()...))
	q.set(sourceTestNow.Add(100 * time.Second))
	got := scrapeSnapshot(t, store).ActiveWorkSource
	if got.Source != statuspkg.ActiveWorkSourceLastRow || !got.AsOf.Equal(newer) || got.Age != 90*time.Second {
		t.Fatalf("source = %+v, want last_row at the newer stale row's as_of with age 90s", got)
	}
}

// TestSelectDecodesAStaleRowOnlyWhenAsked pins the API path: without
// DecodeStale a stale row is a plain fallback carrying nothing, and with it the
// same row carries its stored entries; a foreign or undecodable stale row
// carries nothing either way.
func TestSelectDecodesAStaleRowOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	sha := pgstatus.ActiveWorkSummarySourceSHA256()
	good := encodedEntries(t, goodEntries()...)
	staleAt := sourceTestNow.Add(-100 * time.Second)
	cfg := summary.SelectConfig{ModelKey: summary.ModelActiveWorkSummary, SourceSHA256: sha, StaleAfter: 33 * time.Second}
	for _, tc := range []struct {
		name        string
		row         []any
		decodeStale bool
		wantStored  int
		wantReason  summary.Reason
	}{
		{"off", storedRow(staleAt, summary.SchemaVersion, sha, 4, good), false, 0, summary.ReasonStale},
		{"on", storedRow(staleAt, summary.SchemaVersion, sha, 4, good), true, 4, summary.ReasonStale},
		{"on but foreign", storedRow(staleAt, summary.SchemaVersion, "other", 4, good), true, 0, summary.ReasonVersion},
		{"on but not decodable", storedRow(staleAt, summary.SchemaVersion, sha, 1, `{"x":1}`), true, 0, summary.ReasonStale},
		{"on but an age key is not a number", storedRow(staleAt, summary.SchemaVersion, sha, 1,
			encodedEntries(t, summary.Entry{Section: "queue", Ordinal: 1, JSON: `{"oldest_outstanding_age_seconds":"soon"}`})), true, 0, summary.ReasonStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := cfg
			c.DecodeStale = tc.decodeStale
			got, err := summary.Select(context.Background(), &sourceQueryer{installed: true, row: tc.row}, c)
			if err != nil {
				t.Fatal(err)
			}
			if got.Source != summary.SourceLiveFallback || got.Reason != tc.wantReason || len(got.Stored) != tc.wantStored || got.Entries != nil {
				t.Fatalf("selection = %+v, want live_fallback/%s with %d stored entries and no served entries", got, tc.wantReason, tc.wantStored)
			}
		})
	}
}
