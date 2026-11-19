// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7009 disposable PostgreSQL 18 proofs of the runtime /metrics scrape path in autocommit, the shape of every hosted runtime: a row written by the production writer is served equal to the live active-work statement at the same data and clock; once the row is stale, missing, or its table is dropped, the process keeps serving its last row as stale with an age that keeps growing; a process that restarts into a stale but decodable row serves that row as stale, and a process that restarts with the table dropped serves the zero summary (the empty-table and foreign or undecodable row cases are proven hermetically, not by this live file); with the reader on the live active-work statement runs zero times, while with the reader off and no servable row each model falls back to its live statement (reason flag_off) instead of zeros (#10872); the statement inventory on an empty store through the production status store is 26 statements before, 17 with the reader on (no active-work and no coordinator statement on the stored serve, #8828, #10872), and 21 with the reader off while the writer's fresh rows are servable (the 17 stored-serve statements plus the four probe reads); and the narrowed scrape with the reader on renders deterministic bytes with no stored row (zero summary marked stale, no live statement) while the broader selection still reads live over a database whose collector fact-evidence, registry collector, and coordinator sections are populated (with a planted violation on the live read); the blocking reducer-contention-gate step also runs it with ESHU_REQUIRE_STATUS_SUMMARY_WRITER_PROOF=1, so an unset DSN fails there
//eshu:readiness:env ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE ESHU_STATUS_SUMMARY_PROOF_DSN

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

// coordinatorCount returns how many recorded statements are the live
// coordinator read (#8828): the eight statements readCoordinatorSnapshot runs on
// a store with no collector instance. The matched text is unique to them.
func (p *recordingPool) coordinatorCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, query := range p.queries {
		for _, marker := range []string{
			"configuration->'scan_interval'",
			"FROM workflow_runs\nGROUP BY status",
			"FROM workflow_work_items\nGROUP BY status",
			"FROM workflow_run_completeness\nGROUP BY status",
			"AS active_claim_count,\n    (SELECT COUNT(*)",
			"recent_failed_runs",
			"WITH workflow_collector_backpressure",
			"last_failure_class,\n    COUNT(*) AS count\nFROM workflow_work_items",
		} {
			if strings.Contains(query, marker) {
				n++
				break
			}
		}
	}
	return n
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
// path on a real database in autocommit with the reader on, the shape of a
// hosted runtime with the reader enabled: a row the production writer stored
// is served equal to the live statement at the same data and clock; once the
// row is stale, missing, or its table is dropped, the process keeps serving
// its newest decodable row as stale with a growing age; a process restarted
// into a stale row serves that row, and one that can decode no row serves the
// zero summary; and the live active-work statement runs zero times
// throughout. (With the reader off and no servable row the scrape runs the
// live statement instead; see
// TestScrapeBytesEqualWithTheOmittedSectionsPopulatedLive.)
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
	// A process that has not served any row (a restart during the outage) serves
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
// scrape sends to a real database through the production status store: before
// this change, with the reader off, and with the reader on. Every statement
// runs without error, so the narrowed selection is valid SQL end to end. The
// writer stored fresh rows for both models, so the reader off serves them:
// no live active-work and no live coordinator statement, plus the four probe
// reads (a clock and a keyed row read per model) that decide servability
// (#10872).
func TestScrapeStatementInventoryOnAnEmptyStoreLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	mustExec(ctx, t, database, `DELETE FROM fact_work_items`)
	if pass := newLiveWriterWithCoordinator(database).RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	inventory := func(selection statuspkg.SnapshotSelection, enabled bool) (total, activeWork, coordinator int) {
		t.Helper()
		pool := &recordingPool{database: database}
		if _, err := scrapeStatusStore(pool, enabled, time.Hour).ReadStatusSnapshotFiltered(ctx, time.Now(), selection); err != nil {
			t.Fatalf("scrape: %v", err)
		}
		total, activeWork = pool.count()
		return total, activeWork, pool.coordinatorCount()
	}
	beforeTotal, beforeActive, beforeCoordinator := inventory(statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence(), false)
	offTotal, offActive, offCoordinator := inventory(scrapeSelection(), false)
	onTotal, onActive, onCoordinator := inventory(scrapeSelection(), true)
	t.Logf("statements per scrape on an empty store: before=%d (active-work %d, coordinator %d), reader off=%d (%d, %d), reader on=%d (%d, %d)",
		beforeTotal, beforeActive, beforeCoordinator, offTotal, offActive, offCoordinator, onTotal, onActive, onCoordinator)
	if beforeTotal != 26 || beforeActive != 1 || beforeCoordinator != 8 {
		t.Fatalf("before = %d statements, %d active-work, %d coordinator; want 26, 1 and 8", beforeTotal, beforeActive, beforeCoordinator)
	}
	if offTotal != 21 || offActive != 0 || offCoordinator != 0 {
		t.Fatalf("reader off = %d statements, %d active-work, %d coordinator; want 21 (the 17 stored-serve statements plus the four probe reads), 0 and 0",
			offTotal, offActive, offCoordinator)
	}
	if onTotal != 17 || onActive != 0 || onCoordinator != 0 {
		t.Fatalf("reader on = %d statements, %d active-work, %d coordinator; want 17 (the active-work and the eight coordinator statements are replaced by a clock and a row read for each of the two models), 0 and 0",
			onTotal, onActive, onCoordinator)
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
// from a real database whose omitted sections are populated, with the reader
// off and no writer running. The narrowed scrape falls back to the live
// statement per model (reason flag_off) and reports the live counts, never
// zeros; the same scrape with the reader on serves the zero summary marked
// stale, deterministically, with no live statement. The broad read still runs
// live (#10872). It proves the populated sections exist (the broader read
// returns them), that the flag-off narrow scrape reports live counts rather
// than zeros, that the stored path stays deterministic with no live
// statement, that stored and live serves differ by path (not by section
// content), and plants a violation: changing a section the live read renders
// changes its bytes.
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

	pool := &recordingPool{database: database}
	narrowStore := scrapeStatusStore(pool, false, time.Hour)
	scrape := func(store postgres.StatusStore, selection *statuspkg.SnapshotSelection) string {
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
	// No writer and the reader off: each model falls back to its live
	// statement (reason flag_off), so the narrowed scrape reports the live
	// counts instead of zeros.
	narrowSnap, err := narrowStore.ReadStatusSnapshotFiltered(ctx, asOf, scrapeSelection())
	if err != nil {
		t.Fatalf("flag-off narrow scrape: %v", err)
	}
	if got := narrowSnap.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceLive || got.Reason != statuspkg.ActiveWorkReasonFlagOff || got.Stale {
		t.Fatalf("flag-off narrow scrape source = %+v, want live/flag_off, not stale", got)
	}
	if narrowSnap.Queue == (statuspkg.QueueSnapshot{}) {
		t.Fatalf("flag-off narrow scrape with no stored row served the zero queue; want the live counts")
	}
	narrowed := scrape(narrowStore, nil)
	if total, activeWork := pool.count(); activeWork == 0 || pool.coordinatorCount() == 0 {
		t.Fatalf("flag-off narrowed scrapes ran %d statements without both live fallbacks; want the live active-work and coordinator statements", total)
	}
	if strings.Contains(narrowed, `eshu_runtime_status_summary_stale{model_key="active_work_summary"`) {
		t.Fatalf("flag-off live scrape renders a stored marker:\n%s", narrowed)
	}

	// The same database with the reader on: the narrowed scrape serves the
	// zero summary marked stale, deterministically, with no live statement.
	onPool := &recordingPool{database: database}
	onStore := scrapeStatusStore(onPool, true, time.Hour)
	zero := scrape(onStore, nil)
	if second := scrape(onStore, nil); second != zero {
		t.Fatalf("two identical stored narrow scrapes differ:\nfirst=%s\nsecond=%s", zero, second)
	}
	if !strings.Contains(zero, `eshu_runtime_status_summary_stale{model_key="active_work_summary",service_name="collector-git"} 1`) {
		t.Fatalf("reader-on narrowed scrape with no stored row is not marked stale:\n%s", zero)
	}
	if total, activeWork := onPool.count(); activeWork != 0 || onPool.coordinatorCount() != 0 {
		t.Fatalf("reader-on narrowed scrapes ran %d statements with live reads; want zero live active-work and coordinator statements (stored serve only)", total)
	}

	// A database with fresh stored rows for both models: the flag-off narrow
	// scrape serves them frozen, while the broad read stays live.
	ctxB, databaseB := openWriterDatabase(t)
	seedWork(ctxB, t, databaseB, 200, 100)
	if pass := newLiveWriterWithCoordinator(databaseB).RunOnce(ctxB); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	storeB := scrapeStatusStore(&recordingPool{database: databaseB}, false, time.Hour)
	stored, err := storeB.ReadStatusSnapshotFiltered(ctxB, time.Now().UTC(), scrapeSelection())
	if err != nil {
		t.Fatalf("flag-off stored scrape: %v", err)
	}
	if got := stored.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Reason != statuspkg.ActiveWorkReasonFresh || got.Stale {
		t.Fatalf("flag-off narrow scrape with fresh rows source = %+v, want model/fresh", got)
	}
	if narrowedB, wideB := scrape(storeB, nil), scrape(storeB, &broad); wideB == narrowedB {
		t.Fatal("the stored narrow scrape equals the live broad read: the stored-vs-live split is not pinned")
	}

	// Change the live queue: the broad read follows, the stored narrow
	// scrape stays frozen at the writer's row.
	var pending int
	if err := databaseB.QueryRowContext(ctxB, `SELECT count(*) FROM fact_work_items WHERE status = 'pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending == 0 {
		t.Fatal("the fixture seeded no pending items; the frozen-vs-live comparison would be vacuous")
	}
	before := stored.Queue.Outstanding
	mustExec(ctxB, t, databaseB, `UPDATE fact_work_items SET status = 'succeeded' WHERE status = 'pending'`)
	if live, err := storeB.ReadStatusSnapshotFiltered(ctxB, time.Now().UTC(), broad); err != nil {
		t.Fatal(err)
	} else if live.Queue.Outstanding != before-pending {
		t.Fatalf("broad read outstanding = %d after succeeding %d pending items, want %d", live.Queue.Outstanding, pending, before-pending)
	}
	if again, err := storeB.ReadStatusSnapshotFiltered(ctxB, time.Now().UTC(), scrapeSelection()); err != nil {
		t.Fatal(err)
	} else if again.Queue.Outstanding != before {
		t.Fatalf("stored narrow scrape outstanding = %d after the change, want the frozen %d", again.Queue.Outstanding, before)
	} else if got := again.ActiveWorkSource; got.Source != statuspkg.ActiveWorkSourceModel || got.Stale {
		t.Fatalf("stored narrow scrape source after the change = %+v, want model, not stale", got)
	}
}
