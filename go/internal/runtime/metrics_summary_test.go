// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// scrapeInventory counts the statements one scrape issues.
type scrapeInventory struct {
	total, activeWork, factRecords, clock, modelRow, coordinator int
}

func inventoryOf(calls []fake.QueryCall) scrapeInventory {
	var inv scrapeInventory
	for _, call := range calls {
		inv.total++
		switch {
		case strings.Contains(call.Query, "to_regclass('status_summary_snapshots')"):
			inv.clock++
		case strings.Contains(call.Query, "FROM status_summary_snapshots"):
			inv.modelRow++
		case strings.Contains(call.Query, "active_work_stage"):
			inv.activeWork++
		case strings.Contains(call.Query, "fact_records"):
			inv.factRecords++
		case isLiveCoordinatorStatement(call.Query):
			inv.coordinator++
		}
	}
	return inv
}

// isLiveCoordinatorStatement matches the eight statements readCoordinatorSnapshot
// runs on an empty store (the terminal-failure read needs an enabled instance).
func isLiveCoordinatorStatement(query string) bool {
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
			return true
		}
	}
	return false
}

// emptyStoreQueryer answers every statement with no rows except the summary
// clock, which carries the database time and that the table exists.
func emptyStoreQueryer() *fake.ExecQueryer {
	return &fake.ExecQueryer{Routes: []fake.Route{func(query string, _ []any) (*fake.Rows, bool) {
		if strings.Contains(query, "to_regclass('status_summary_snapshots')") {
			return &fake.Rows{Data: [][]any{{time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), true}}}, true
		}
		return &fake.Rows{}, true
	}}}
}

func scrapeStore(queryer *fake.ExecQueryer, readerOn bool) postgres.StatusStore {
	return postgres.NewStatusStore(queryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: readerOn, StaleAfter: 33 * time.Second}))
}

// TestStatusMetricsStatementInventory runs the production StatusStore over an
// empty store and counts the statements one scrape issues: before this change
// (full minus Terraform, reader off), and now with the reader off and on. The
// reader on removes the live active-work statement and the eight live
// coordinator statements (#8828) and adds, per stored model, the clock and the
// keyed row read (two models, so two of each); both selections drop the fact_records aggregate and the three
// registry collector statements, which the scrape never renders. The
// terminal-failure read (#8803) runs only when an enabled collector instance
// exists; this empty store has none, so it adds no statement here. The
// stuck-partial scope read (#10389) runs on every selection, reader on or
// off, because no stored-summary model owns it. With the reader off and no
// stored row, the flag-off scrape probes the models (one clock plus one row
// read each) and then runs the full live path, so it issues the same
// statements as the full live read plus the two probe reads; with a stored
// row present it serves the stored row with zero live statements (#10872).
func TestStatusMetricsStatementInventory(t *testing.T) {
	t.Parallel()
	count := func(selection statuspkg.SnapshotSelection, readerOn bool) scrapeInventory {
		t.Helper()
		queryer := emptyStoreQueryer()
		if _, err := scrapeStore(queryer, readerOn).ReadStatusSnapshotFiltered(context.Background(), time.Now(), selection); err != nil {
			t.Fatal(err)
		}
		return inventoryOf(queryer.Queries)
	}
	before := count(statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence(), false)
	off := count(metricsSnapshotSelection(), false)
	on := count(metricsSnapshotSelection(), true)

	if want := (scrapeInventory{total: 26, activeWork: 1, factRecords: 1, coordinator: 8}); before != want {
		t.Fatalf("before = %+v, want %+v", before, want)
	}
	if want := (scrapeInventory{total: 26, activeWork: 1, factRecords: 0, clock: 2, modelRow: 2, coordinator: 8}); off != want {
		t.Fatalf("reader off = %+v, want %+v", off, want)
	}
	if want := (scrapeInventory{total: 17, clock: 2, modelRow: 2}); on != want {
		t.Fatalf("reader on = %+v, want %+v", on, want)
	}
}

const (
	scrapeQueueJSON = `{"total_count":4,"outstanding_count":2,"pending_count":2,"in_flight_count":0,"retrying_count":0,"succeeded_count":2,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":false,"provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":5,"overdue_claim_count":0}`
	scrapeStageJSON = `{"stage":"reducer","status":"pending","count":2}`
)

// scrapeRow is the stored row the production reader scans, written at asOf.
func scrapeRow(t *testing.T, asOf time.Time) [][]any {
	t.Helper()
	payload, err := summary.EncodeEntries([]summary.Entry{
		{Section: "queue", Ordinal: 1, JSON: scrapeQueueJSON},
		{Section: "stage", Ordinal: 1, JSON: scrapeStageJSON},
	})
	if err != nil {
		t.Fatal(err)
	}
	return [][]any{{
		summary.ModelActiveWorkSummary, summary.SchemaVersion, postgres.ActiveWorkSummarySourceSHA256(),
		asOf, asOf, float64(300), 2, payload,
	}}
}

// summaryScrapeQueryer answers the summary statements from a mutable row and
// clock and every other statement with no rows.
type summaryScrapeQueryer struct {
	*fake.ExecQueryer
	now       time.Time
	rows      [][]any
	coordRows [][]any
}

func newSummaryScrapeQueryer(now time.Time) *summaryScrapeQueryer {
	q := &summaryScrapeQueryer{ExecQueryer: &fake.ExecQueryer{}, now: now}
	q.Routes = []fake.Route{func(query string, args []any) (*fake.Rows, bool) {
		switch {
		case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
			return &fake.Rows{Data: [][]any{{q.now, true}}}, true
		case strings.Contains(query, "FROM status_summary_snapshots"):
			if len(args) == 1 && args[0] == summary.ModelCoordinatorSnapshot {
				return &fake.Rows{Data: q.coordRows}, true
			}
			return &fake.Rows{Data: q.rows}, true
		}
		return &fake.Rows{}, true
	}}
	return q
}

// TestStatusMetricsExportsTheSummaryMarker drives the handler end to end over
// the production store: a fresh row exports stale 0 and its age; a stale or
// missing row after a fresh one serves the last row and exports stale 1 with an
// age that keeps growing; a process with no row exports stale 1 and age NaN. In
// every case the live active-work statement runs zero times.
func TestStatusMetricsExportsTheSummaryMarker(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	queryer := newSummaryScrapeQueryer(now)
	store := postgres.NewStatusStore(queryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second}))

	// No row yet: the zero summary.
	body := scrapeBody(t, store)
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_stale", "1")
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_age_seconds", "NaN")

	// A fresh row 10 s old.
	queryer.rows = scrapeRow(t, now.Add(-10*time.Second))
	body = scrapeBody(t, store)
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_stale", "0")
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_age_seconds", "10")
	assertLine(t, body, `eshu_runtime_queue_oldest_outstanding_age_seconds{service_name="collector-git"} 15`)

	// The writer stops: the row is 100 s old at the next scrape, so the last
	// row is served stale, 110 s old, with its stored 5 s age advanced.
	queryer.now = now.Add(100 * time.Second)
	body = scrapeBody(t, store)
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_stale", "1")
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_age_seconds", "110")
	assertLine(t, body, `eshu_runtime_queue_oldest_outstanding_age_seconds{service_name="collector-git"} 115`)
	assertLine(t, body, `eshu_runtime_stage_items{service_name="collector-git",stage="reducer",status="pending"} 2`)

	// The row disappears: still the last row, still growing.
	queryer.rows = nil
	queryer.now = now.Add(200 * time.Second)
	body = scrapeBody(t, store)
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_stale", "1")
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_age_seconds", "210")

	if got := inventoryOf(queryer.Queries).activeWork; got != 0 {
		t.Fatalf("active-work statements = %d, want 0", got)
	}
}

// TestStatusMetricsWithTheReaderOffServeTheStoredSummary is the #10872
// contract at the handler level: a scrape with the reader flag off serves
// the same stored summary as a scrape with the reader on whenever a stored
// row is servable, and never the live active-work or coordinator statements
// in that case. A fresh row exports stale 0 with the stored values; a
// missing row runs the live statement (reason flag_off) instead of
// presenting a zero queue, since writer-off deployments have no stored row.
func TestStatusMetricsWithTheReaderOffServeTheStoredSummary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	offQueryer := newSummaryScrapeQueryer(now)
	offStore := postgres.NewStatusStore(offQueryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: false, StaleAfter: 33 * time.Second}))
	onQueryer := newSummaryScrapeQueryer(now)
	onStore := postgres.NewStatusStore(onQueryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second}))

	// Fresh rows 10 s old for both stored models: stale 0, the stored ages,
	// and the stored queue age advanced, byte-identical to the reader-on
	// scrape, with zero live statements on the flag-off path.
	offQueryer.rows = scrapeRow(t, now.Add(-10*time.Second))
	onQueryer.rows = scrapeRow(t, now.Add(-10*time.Second))
	offQueryer.coordRows = coordinatorScrapeRow(t, now.Add(-10*time.Second))
	onQueryer.coordRows = coordinatorScrapeRow(t, now.Add(-10*time.Second))
	offBody := scrapeBody(t, offStore)
	onBody := scrapeBody(t, onStore)
	if offBody != onBody {
		t.Fatalf("flag-off scrape differs from flag-on:\noff=%s\non=%s", offBody, onBody)
	}
	assertGauge(t, offBody, "collector-git", "eshu_runtime_status_summary_stale", "0")
	assertGauge(t, offBody, "collector-git", "eshu_runtime_status_summary_age_seconds", "10")
	assertLine(t, offBody, `eshu_runtime_queue_oldest_outstanding_age_seconds{service_name="collector-git"} 15`)
	assertLine(t, offBody, `eshu_runtime_status_summary_stale{model_key="coordinator_snapshot",service_name="collector-git"} 0`)
	assertLine(t, offBody, `eshu_runtime_status_summary_age_seconds{model_key="coordinator_snapshot",service_name="collector-git"} 10`)
	assertLine(t, offBody, `eshu_runtime_coordinator_active_claims{service_name="collector-git"} 3`)
	if got := inventoryOf(offQueryer.Queries); got.activeWork != 0 || got.coordinator != 0 {
		t.Fatalf("fresh flag-off scrape ran %d active-work and %d coordinator live statements, want 0", got.activeWork, got.coordinator)
	}

	// The row disappears: the reader-on process serves its remembered last
	// row, stale; the reader-off process has no servable row, so it runs
	// the live statement instead (reason flag_off). The two scrapes
	// legitimately differ — flag-off prefers a live answer over a zero
	// queue (#10872).
	offQueryer.rows = nil
	onQueryer.rows = nil
	offQueryer.coordRows = nil
	onQueryer.coordRows = nil
	offBody = scrapeBody(t, offStore)
	onBody = scrapeBody(t, onStore)
	if offBody == onBody {
		t.Fatal("flag-off live fallback equals the flag-on remembered scrape; want the live fallback to differ")
	}
	assertGauge(t, onBody, "collector-git", "eshu_runtime_status_summary_stale", "1")
	assertGauge(t, onBody, "collector-git", "eshu_runtime_status_summary_age_seconds", "10")
	assertLine(t, onBody, `eshu_runtime_queue_oldest_outstanding_age_seconds{service_name="collector-git"} 15`)
	assertLine(t, onBody, `eshu_runtime_status_summary_stale{model_key="coordinator_snapshot",service_name="collector-git"} 1`)
	assertLine(t, onBody, `eshu_runtime_status_summary_age_seconds{model_key="coordinator_snapshot",service_name="collector-git"} 10`)
	assertLine(t, onBody, `eshu_runtime_coordinator_active_claims{service_name="collector-git"} 3`)
	if got := inventoryOf(offQueryer.Queries); got.activeWork == 0 || got.coordinator == 0 {
		t.Fatalf("flag-off scrape did not run both live fallbacks after the rows disappeared; got %+v", got)
	}

	// A process that never served a row: the reader-on process exports the
	// zero summary (stale 1, NaN age); the reader-off process runs the live
	// statement instead (reason flag_off).
	freshOff := postgres.NewStatusStore(newSummaryScrapeQueryer(now)).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: false, StaleAfter: 33 * time.Second}))
	freshOn := postgres.NewStatusStore(newSummaryScrapeQueryer(now)).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second}))
	offBody = scrapeBody(t, freshOff)
	onBody = scrapeBody(t, freshOn)
	if offBody == onBody {
		t.Fatal("flag-off live scrape equals the flag-on zero serve; want the live fallback to differ")
	}
	assertGauge(t, onBody, "collector-git", "eshu_runtime_status_summary_stale", "1")
	assertGauge(t, onBody, "collector-git", "eshu_runtime_status_summary_age_seconds", "NaN")
	assertLine(t, onBody, `eshu_runtime_status_summary_stale{model_key="coordinator_snapshot",service_name="collector-git"} 1`)
	assertLine(t, onBody, `eshu_runtime_status_summary_age_seconds{model_key="coordinator_snapshot",service_name="collector-git"} NaN`)
}

// TestStatusMetricsWithTheReaderOffDecidesPerModel pins the per-model
// independence of the flag-off fallback (#10872): with a stored row for only
// one model, the scrape serves that model stored (with its markers) and runs
// the live statement only for the model with no row (reason flag_off). A
// coupled decision — both models stored or both live — fails this test.
func TestStatusMetricsWithTheReaderOffDecidesPerModel(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	// Only an active-work row: stored queue gauges with markers, and the
	// live coordinator statement runs for the missing coordinator row.
	activeQueryer := newSummaryScrapeQueryer(now)
	activeQueryer.rows = scrapeRow(t, now.Add(-10*time.Second))
	activeStore := postgres.NewStatusStore(activeQueryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: false, StaleAfter: 33 * time.Second}))
	body := scrapeBody(t, activeStore)
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_stale", "0")
	assertLine(t, body, `eshu_runtime_queue_oldest_outstanding_age_seconds{service_name="collector-git"} 15`)
	if strings.Contains(body, `eshu_runtime_status_summary_stale{model_key="coordinator_snapshot"`) {
		t.Fatalf("flag-off scrape with no coordinator row renders a coordinator marker:\n%s", body)
	}
	if got := inventoryOf(activeQueryer.Queries); got.activeWork != 0 || got.coordinator == 0 {
		t.Fatalf("flag-off scrape ran %d active-work and %d coordinator live statements, want 0 active-work and a coordinator fallback", got.activeWork, got.coordinator)
	}

	// Only a coordinator row: stored coordinator gauges with markers, and
	// the live active-work statement runs for the missing row.
	coordQueryer := newSummaryScrapeQueryer(now)
	coordQueryer.coordRows = coordinatorScrapeRow(t, now.Add(-10*time.Second))
	coordStore := postgres.NewStatusStore(coordQueryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: false, StaleAfter: 33 * time.Second}))
	body = scrapeBody(t, coordStore)
	assertLine(t, body, `eshu_runtime_status_summary_stale{model_key="coordinator_snapshot",service_name="collector-git"} 0`)
	assertLine(t, body, `eshu_runtime_coordinator_active_claims{service_name="collector-git"} 3`)
	if strings.Contains(body, `eshu_runtime_status_summary_stale{model_key="active_work_summary"`) {
		t.Fatalf("flag-off scrape with no active-work row renders a stored marker:\n%s", body)
	}
	if got := inventoryOf(coordQueryer.Queries); got.activeWork == 0 || got.coordinator != 0 {
		t.Fatalf("flag-off scrape ran %d active-work and %d coordinator live statements, want an active-work fallback and 0 coordinator", got.activeWork, got.coordinator)
	}
}

// TestSummaryMarkerRendersOnlyForTheScrapeSources pins the gauge text per
// source, and that a live or unreported source renders nothing.
func TestSummaryMarkerRendersOnlyForTheScrapeSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		source     statuspkg.ActiveWorkSource
		stale, age string
	}{
		{"fresh", statuspkg.ActiveWorkSource{Source: statuspkg.ActiveWorkSourceModel, Reason: "fresh", Age: 2500 * time.Millisecond}, "0", "2.5"},
		{"last row", statuspkg.ActiveWorkSource{Source: statuspkg.ActiveWorkSourceLastRow, Reason: "stale", Age: 90 * time.Second, Stale: true}, "1", "90"},
		{"zero", statuspkg.ActiveWorkSource{Source: statuspkg.ActiveWorkSourceZero, Reason: "missing", Stale: true}, "1", "NaN"},
		{"live", statuspkg.ActiveWorkSource{Source: statuspkg.ActiveWorkSourceLive, Reason: "flag_off"}, "", ""},
		{"live fallback", statuspkg.ActiveWorkSource{Source: statuspkg.ActiveWorkSourceLiveFallback, Reason: "stale"}, "", ""},
		{"unreported", statuspkg.ActiveWorkSource{}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := renderStatusMetrics("svc", statuspkg.Report{ActiveWorkSource: tc.source})
			stale := strings.Contains(out, `eshu_runtime_status_summary_stale{model_key="active_work_summary"} `)
			if tc.stale == "" {
				if stale || strings.Contains(out, "eshu_runtime_status_summary_age_seconds") {
					t.Fatalf("rendered a summary gauge for %q:\n%s", tc.source.Source, out)
				}
				return
			}
			assertGauge(t, out, "svc", "eshu_runtime_status_summary_stale", tc.stale)
			assertGauge(t, out, "svc", "eshu_runtime_status_summary_age_seconds", tc.age)
		})
	}
}

func assertLine(t *testing.T, body, line string) {
	t.Helper()
	if !strings.Contains(body, line+"\n") {
		t.Fatalf("scrape lacks %q:\n%s", line, body)
	}
}

func assertGauge(t *testing.T, body, service, name, want string) {
	t.Helper()
	line := name + `{model_key="active_work_summary",service_name="` + service + `"} ` + want + "\n"
	if !strings.Contains(body, line) {
		t.Fatalf("scrape lacks %q:\n%s", line, body)
	}
}

func scrapeBody(t *testing.T, reader statuspkg.Reader) string {
	t.Helper()
	handler, err := NewStatusMetricsHandler("collector-git", reader)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestStatusMetricsDatabaseErrorFailsTheScrape states the error contract: a
// failed clock or row read is a 500 like any other failed status read, never a
// stale serve and never a live run. The composite /metrics handler reports it as
// eshu_runtime_status_snapshot_available 0.
func TestStatusMetricsDatabaseErrorFailsTheScrape(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	queryer := newSummaryScrapeQueryer(now)
	store := postgres.NewStatusStore(queryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second}))
	queryer.rows = scrapeRow(t, now.Add(-time.Second))
	scrapeBody(t, store) // the process holds a last row

	queryer.Routes = append([]fake.Route{func(query string, _ []any) (*fake.Rows, bool) {
		if strings.Contains(query, "to_regclass('status_summary_snapshots')") {
			return &fake.Rows{FailWith: errors.New("connection reset")}, true
		}
		return nil, false
	}}, queryer.Routes...)
	handler, err := NewStatusMetricsHandler("collector-git", store)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "status_summary") {
		t.Fatalf("a failed scrape carried a summary gauge: %s", rec.Body.String())
	}
	if got := inventoryOf(queryer.Queries).activeWork; got != 0 {
		t.Fatalf("active-work statements = %d, want 0", got)
	}
}

// TestStatusMetricsAfterARestartDuringAWriterOutageServeTheStaleRow is the
// restart case end to end: a new process finds only a row 100 s old. It must
// export that row's counts with stale 1 and its real age, not zeros with a
// healthy state, so a queue alert on the pod keeps seeing the stall.
func TestStatusMetricsAfterARestartDuringAWriterOutageServeTheStaleRow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	queryer := newSummaryScrapeQueryer(now)
	queryer.rows = scrapeRow(t, now.Add(-100*time.Second))
	store := postgres.NewStatusStore(queryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second}))

	body := scrapeBody(t, store)
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_stale", "1")
	assertGauge(t, body, "collector-git", "eshu_runtime_status_summary_age_seconds", "100")
	assertLine(t, body, `eshu_runtime_queue_oldest_outstanding_age_seconds{service_name="collector-git"} 105`)
	assertLine(t, body, `eshu_runtime_queue_outstanding{service_name="collector-git"} 2`)
	if got := inventoryOf(queryer.Queries).activeWork; got != 0 {
		t.Fatalf("active-work statements = %d, want 0", got)
	}
}
