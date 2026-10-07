// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// recordingPool runs each statement in autocommit on the pool, the way a hosted
// runtime's status store does, and records the statement text.
type recordingPool struct {
	database *sql.DB
	mu       sync.Mutex
	queries  []string
}

func (p *recordingPool) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	p.mu.Lock()
	p.queries = append(p.queries, query)
	p.mu.Unlock()
	return p.database.QueryContext(ctx, query, args...)
}

// count returns the statements recorded so far and how many of them are the
// live active-work statement.
func (p *recordingPool) count() (total, activeWork int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, query := range p.queries {
		if strings.Contains(query, "active_work_stage") {
			activeWork++
		}
	}
	return len(p.queries), activeWork
}

func scrapeStatusStore(pool db.Queryer, enabled bool, staleAfter time.Duration) postgres.StatusStore {
	return postgres.NewStatusStore(pool).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(store.ReadConfig{Enabled: enabled, StaleAfter: staleAfter}))
}

func scrapeSelection() statuspkg.SnapshotSelection {
	return statuspkg.SnapshotSelection{}.WithoutTerraformStateEvidence().WithStoredActiveWorkOnly()
}

// TestScrapeServesTheStoredRowAndNeverTheLiveStatementLive runs the scrape
// path on a real database in autocommit, the shape of every hosted runtime: a
// row the production writer stored is served equal to the live statement at
// the same data and clock; once the row is stale, missing, or its table is
// dropped, the process keeps serving that last row as stale with a growing
// age; a process that never served a row serves the zero summary; and the live
// active-work statement runs zero times throughout.
func TestScrapeServesTheStoredRowAndNeverTheLiveStatementLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	writer := newLiveWriter(database)
	seedWork(ctx, t, database, 200, 100)
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	time.Sleep(30 * time.Millisecond)

	pool := &recordingPool{database: database}
	process := scrapeStatusStore(pool, true, time.Hour)
	scrape := func(s postgres.StatusStore) statuspkg.RawSnapshot {
		t.Helper()
		snapshot, err := s.ReadStatusSnapshotFiltered(ctx, time.Now(), scrapeSelection())
		if err != nil {
			t.Fatalf("scrape: %v", err)
		}
		return snapshot
	}

	fresh := scrape(process)
	if got := fresh.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh || got.Stale {
		t.Fatalf("first scrape source = %+v, want model/fresh", got)
	}
	live, err := scrapeStatusStore(&recordingPool{database: database}, false, time.Hour).ReadStatusSnapshotFiltered(
		ctx, fresh.ActiveWorkSource.AsOf.Add(fresh.ActiveWorkSource.Age), statuspkg.SnapshotSelection{}.WithoutTerraformStateEvidence())
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	assertActiveWorkEqual(t, fresh, live)

	// The writer stops: the row ages past the limit. A reader with a short
	// limit sees it as stale and the process serves its last row instead.
	mustExec(ctx, t, database, `UPDATE status_summary_snapshots SET as_of = as_of - interval '10 minutes'`)
	short := scrapeStatusStore(pool, true, time.Minute)
	if got := scrape(short).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceZero || got.Reason != statuspkg.ActiveWorkReasonStale || !got.Stale {
		t.Fatalf("a process with no earlier row scraped %+v, want zero/stale", got)
	}
	mustExec(ctx, t, database, `UPDATE status_summary_snapshots SET as_of = as_of + interval '10 minutes'`)
	scrape(short) // fresh under the one minute limit: this process now holds a row
	mustExec(ctx, t, database, `UPDATE status_summary_snapshots SET as_of = as_of - interval '10 minutes'`)
	time.Sleep(30 * time.Millisecond)
	stale := scrape(short)
	got := stale.ActiveWorkSource
	// The age is the held row's, not the rejected row's 10 minutes: it is the
	// time since the row this process served, and it keeps growing.
	if got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonStale || !got.Stale ||
		got.Age < 30*time.Millisecond || got.Age > time.Minute {
		t.Fatalf("scrape after the writer stopped = %+v, want last_row/stale with the held row's small age", got)
	}
	if stale.Queue.Outstanding != fresh.Queue.Outstanding || stale.Queue.OldestOutstandingAge <= fresh.Queue.OldestOutstandingAge {
		t.Fatalf("last row queue = %+v, want the first row's counts with ages advanced past %v", stale.Queue, fresh.Queue.OldestOutstandingAge)
	}
	later := scrape(short).ActiveWorkSource
	if later.Age <= got.Age {
		t.Fatalf("the last row's age did not advance: %v then %v", got.Age, later.Age)
	}

	mustExec(ctx, t, database, `DELETE FROM status_summary_snapshots`)
	if got := scrape(short).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonMissing {
		t.Fatalf("scrape with the row deleted = %+v, want last_row/missing", got)
	}
	mustExec(ctx, t, database, `DROP TABLE status_summary_snapshots`)
	if got := scrape(short).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonNotInstalled {
		t.Fatalf("scrape with the table dropped = %+v, want last_row/not_installed", got)
	}
	if got := scrape(scrapeStatusStore(pool, true, time.Minute)).ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceZero || got.Reason != statuspkg.ActiveWorkReasonNotInstalled {
		t.Fatalf("a new process scraping with the table dropped = %+v, want zero/not_installed", got)
	}
	if _, activeWork := pool.count(); activeWork != 0 {
		t.Fatalf("the scrape ran the live active-work statement %d times, want 0", activeWork)
	}
}

// TestScrapeStatementInventoryOnAnEmptyStoreLive counts the statements one
// scrape sends to a real, empty database through the production status store:
// before this change, with the reader off, and with the reader on. Every
// statement runs without error, so the narrowed selection is valid SQL end to
// end, and the reader on sends no live active-work statement.
func TestScrapeStatementInventoryOnAnEmptyStoreLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	mustExec(ctx, t, database, `DELETE FROM fact_work_items`)
	if pass := newLiveWriter(database).RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	inventory := func(selection statuspkg.SnapshotSelection, enabled bool) (total, activeWork int) {
		t.Helper()
		pool := &recordingPool{database: database}
		if _, err := scrapeStatusStore(pool, enabled, time.Hour).ReadStatusSnapshotFiltered(ctx, time.Now(), selection); err != nil {
			t.Fatalf("scrape: %v", err)
		}
		return pool.count()
	}
	beforeTotal, beforeActive := inventory(statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence(), false)
	offTotal, offActive := inventory(scrapeSelection(), false)
	onTotal, onActive := inventory(scrapeSelection(), true)
	t.Logf("statements per scrape on an empty store: before=%d (active-work %d), reader off=%d (%d), reader on=%d (%d)",
		beforeTotal, beforeActive, offTotal, offActive, onTotal, onActive)
	if beforeTotal != 24 || beforeActive != 1 {
		t.Fatalf("before = %d statements, %d active-work; want 24 and 1", beforeTotal, beforeActive)
	}
	if offTotal != 20 || offActive != 1 {
		t.Fatalf("reader off = %d statements, %d active-work; want 20 and 1", offTotal, offActive)
	}
	if onTotal != 21 || onActive != 0 {
		t.Fatalf("reader on = %d statements, %d active-work; want 21 (the clock and the row read replace it) and 0", onTotal, onActive)
	}
}
