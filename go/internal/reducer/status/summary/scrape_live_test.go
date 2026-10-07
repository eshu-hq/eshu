// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	"github.com/eshu-hq/eshu/go/internal/runtime"
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

// selectionRecorder is a status reader that records the selection it was asked
// for and answers with an empty snapshot.
type selectionRecorder struct{ selection statuspkg.SnapshotSelection }

func (r *selectionRecorder) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	return r.ReadStatusSnapshotFiltered(ctx, asOf, statuspkg.FullSnapshotSelection())
}

func (r *selectionRecorder) ReadStatusSnapshotFiltered(_ context.Context, asOf time.Time, selection statuspkg.SnapshotSelection) (statuspkg.RawSnapshot, error) {
	r.selection = selection
	return statuspkg.RawSnapshot{AsOf: asOf}, nil
}

// scrapeSelection returns the selection the production /metrics handler
// requests, captured from the handler itself, so these proofs follow the
// production definition instead of a copy of it.
var scrapeSelection = sync.OnceValue(func() statuspkg.SnapshotSelection {
	recorder := &selectionRecorder{}
	handler, err := runtime.NewStatusMetricsHandler("selection-probe", recorder)
	if err != nil {
		panic(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		panic("selection probe scrape failed: " + rec.Body.String())
	}
	return recorder.selection
})

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
	// A process that never served a row (a restart during the outage) serves
	// the stale row itself, with its real age and its own counts: never zeros.
	restart := scrape(short)
	if got := restart.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLastRow || got.Reason != statuspkg.ActiveWorkReasonStale ||
		!got.Stale || got.Age < 10*time.Minute {
		t.Fatalf("a process with no earlier row scraped %+v, want last_row/stale at least 10 minutes old", got)
	}
	if restart.Queue.Outstanding != fresh.Queue.Outstanding || restart.Queue.OldestOutstandingAge < 10*time.Minute {
		t.Fatalf("restart queue = %+v, want the stale row's counts (%d outstanding) with ages past 10 minutes", restart.Queue, fresh.Queue.Outstanding)
	}
	mustExec(ctx, t, database, `UPDATE status_summary_snapshots SET as_of = as_of + interval '10 minutes'`)
	scrape(short) // fresh under the one minute limit: it replaces the older stale row this process held
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

// pinnedReader reads through inner at a fixed instant, and with selection set
// ignores the caller's selection, so the same handler can be driven once with
// the metrics selection and once with the broader one it used to request.
type pinnedReader struct {
	inner     statuspkg.Reader
	asOf      time.Time
	selection *statuspkg.SnapshotSelection
}

func (r pinnedReader) ReadStatusSnapshot(ctx context.Context, _ time.Time) (statuspkg.RawSnapshot, error) {
	return r.ReadStatusSnapshotFiltered(ctx, r.asOf, statuspkg.FullSnapshotSelection())
}

func (r pinnedReader) ReadStatusSnapshotFiltered(ctx context.Context, _ time.Time, selection statuspkg.SnapshotSelection) (statuspkg.RawSnapshot, error) {
	if r.selection != nil {
		selection = *r.selection
	}
	return r.inner.ReadStatusSnapshotFiltered(ctx, r.asOf, selection)
}

// seedCollectorSections populates what the omitted sections read: a collector
// evidence summary row on an active scope, a registry collector instance, and
// registry work items (completed, retryable and terminal failures with
// classes), which the coordinator section also counts.
func seedCollectorSections(ctx context.Context, t *testing.T, database *sql.DB) {
	t.Helper()
	mustExec(ctx, t, database, `
INSERT INTO collector_evidence_summary (scope_id, generation_id, collector_kind, evidence_source, source_system,
  observation_count, last_observed_at, last_ingested_at, materialized_at)
VALUES ('scope-0', 'gen-0', 'git', 'git_fact', 'git', 5, now() - interval '5 minutes', now() - interval '4 minutes', now())`)
	mustExec(ctx, t, database, `
INSERT INTO collector_instances (instance_id, collector_kind, mode, enabled, last_observed_at, created_at, updated_at)
VALUES ('oci-1', 'oci_registry', 'scheduled', true, now(), now(), now()),
       ('pkg-1', 'package_registry', 'scheduled', true, now(), now(), now())`)
	mustExec(ctx, t, database, `
INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at) VALUES ('run-1', 'schedule', 'running', now(), now())`)
	mustExec(ctx, t, database, `
INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, source_system, scope_id,
  acceptance_unit_id, source_run_id, generation_id, fairness_key, status, last_failure_class, created_at, updated_at)
SELECT 'reg-' || i, 'run-1',
       CASE WHEN i % 2 = 0 THEN 'oci_registry' ELSE 'package_registry' END,
       CASE WHEN i % 2 = 0 THEN 'oci-1' ELSE 'pkg-1' END,
       'registry', 'scope-' || i, 'unit-' || i, 'src-' || i, 'gen-' || i, 'a:b:c:npm',
       (ARRAY['completed', 'failed_retryable', 'failed_terminal', 'pending'])[1 + i % 4],
       CASE WHEN i % 4 IN (1, 2) THEN 'registry_rate_limited' END,
       now() - interval '1 hour', now() - interval '10 minutes'
FROM generate_series(0, 11) AS i`)
}

// TestScrapeBytesEqualWithTheOmittedSectionsPopulatedLive renders the scrape
// from a real database whose omitted sections are populated, once with the
// metrics selection and once with the full-minus-Terraform selection the scrape
// used before, at one pinned instant and with the reader off, and requires
// identical bytes. It proves the populated sections exist (the broader read
// returns them), and plants a violation: changing a section the scrape renders
// changes the bytes.
func TestScrapeBytesEqualWithTheOmittedSectionsPopulatedLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 200, 100)
	seedCollectorSections(ctx, t, database)
	asOf := time.Now().UTC()
	store := scrapeStatusStore(&recordingPool{database: database}, false, time.Hour)

	broad := statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence()
	snapshot, err := store.ReadStatusSnapshotFiltered(ctx, asOf, broad)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.CollectorFactEvidence) == 0 {
		t.Fatal("the fixture produced no collector fact evidence; the proof would be vacuous")
	}
	var failures int
	for _, row := range snapshot.RegistryCollectors {
		failures += row.RetryableFailures + row.TerminalFailures
	}
	if failures == 0 || len(snapshot.Coordinator.CollectorInstances) == 0 {
		t.Fatalf("the fixture produced registry failures=%d, coordinator instances=%d; the proof would be vacuous",
			failures, len(snapshot.Coordinator.CollectorInstances))
	}

	scrape := func(selection *statuspkg.SnapshotSelection) string {
		t.Helper()
		handler, err := runtime.NewStatusMetricsHandler("collector-git", pinnedReader{inner: store, asOf: asOf, selection: selection})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	narrowed := scrape(nil)
	if wide := scrape(&broad); wide != narrowed {
		t.Fatalf("the narrowed scrape differs from the broad one with the omitted sections populated:\nbroad=%s\nnarrowed=%s", wide, narrowed)
	}

	// The comparison can differ: change a section the scrape renders.
	mustExec(ctx, t, database, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id IN ('w-0', 'w-1', 'w-2')`)
	if changed := scrape(nil); changed == narrowed {
		t.Fatal("a changed work item did not change the scrape: the byte comparison cannot fail")
	}
}
