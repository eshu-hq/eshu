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
	total, activeWork, factRecords, clock, modelRow int
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
		}
	}
	return inv
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
// reader on removes the live active-work statement and adds the clock and the
// keyed row read; both selections drop the fact_records aggregate and the three
// registry collector statements, which the scrape never renders.
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

	if want := (scrapeInventory{total: 24, activeWork: 1, factRecords: 1}); before != want {
		t.Fatalf("before = %+v, want %+v", before, want)
	}
	if want := (scrapeInventory{total: 20, activeWork: 1}); off != want {
		t.Fatalf("reader off = %+v, want %+v", off, want)
	}
	if want := (scrapeInventory{total: 21, clock: 1, modelRow: 1}); on != want {
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
	now  time.Time
	rows [][]any
}

func newSummaryScrapeQueryer(now time.Time) *summaryScrapeQueryer {
	q := &summaryScrapeQueryer{ExecQueryer: &fake.ExecQueryer{}, now: now}
	q.Routes = []fake.Route{func(query string, _ []any) (*fake.Rows, bool) {
		switch {
		case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
			return &fake.Rows{Data: [][]any{{q.now, true}}}, true
		case strings.Contains(query, "FROM status_summary_snapshots"):
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

// TestStatusMetricsWithTheReaderOffAreUnchanged is the flag-off contract: the
// scrape over the production store with the reader off carries no summary
// gauge and is byte-identical to the scrape from the selection before this
// change.
func TestStatusMetricsWithTheReaderOffAreUnchanged(t *testing.T) {
	t.Parallel()
	scrape := func(selection statuspkg.SnapshotSelection) string {
		t.Helper()
		queryer := emptyStoreQueryer()
		report, err := statuspkg.LoadReportWithSelection(context.Background(), scrapeStore(queryer, false),
			time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), statuspkg.DefaultOptions(), selection)
		if err != nil {
			t.Fatal(err)
		}
		return renderStatusMetrics("collector-git", report)
	}
	before := scrape(statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence())
	after := scrape(metricsSnapshotSelection())
	if after != before {
		t.Fatalf("scrape with the reader off changed:\nbefore=%s\nafter=%s", before, after)
	}
	if strings.Contains(after, "status_summary") {
		t.Fatalf("scrape with the reader off carries a summary gauge:\n%s", after)
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
