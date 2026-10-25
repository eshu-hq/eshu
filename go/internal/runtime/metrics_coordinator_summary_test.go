// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/shared"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/workflow/snapshot"
)

func coordinatorScrapeRow(t *testing.T, asOf time.Time) [][]any {
	t.Helper()
	entries, err := snapshot.Encode(&statuspkg.CoordinatorSnapshot{
		WorkItemStatusCounts: []shared.NamedCount{{Name: "pending", Count: 7}},
		ActiveClaims:         3,
		OldestPendingAge:     30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := summary.EncodeEntries(entries)
	if err != nil {
		t.Fatal(err)
	}
	return [][]any{{
		summary.ModelCoordinatorSnapshot, summary.SchemaVersion, postgres.CoordinatorSummarySourceSHA256(),
		asOf, asOf, float64(140), len(entries), payload,
	}}
}

// TestStatusMetricsServeTheStoredCoordinatorWithItsOwnMarker drives the handler
// over the production store: a fresh coordinator row exports its counts and
// stale 0 under model_key="coordinator_snapshot"; once the writer stops the
// same counts export with an age that keeps growing, the oldest-pending age
// advanced to the read, and stale 1, without any live coordinator statement.
func TestStatusMetricsServeTheStoredCoordinatorWithItsOwnMarker(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	var (
		clock       = now
		storedRows  [][]any
		liveRuns    int
		queryer     = &fake.ExecQueryer{}
		coordMarker = func(name, want string) string {
			return name + `{model_key="coordinator_snapshot",service_name="collector-git"} ` + want + "\n"
		}
	)
	queryer.Routes = []fake.Route{func(query string, args []any) (*fake.Rows, bool) {
		switch {
		case strings.Contains(query, "to_regclass('status_summary_snapshots')"):
			return &fake.Rows{Data: [][]any{{clock, true}}}, true
		case strings.Contains(query, "FROM status_summary_snapshots"):
			if len(args) == 1 && args[0] == summary.ModelCoordinatorSnapshot {
				return &fake.Rows{Data: storedRows}, true
			}
			return &fake.Rows{}, true
		case strings.Contains(query, "configuration->'scan_interval'"), strings.Contains(query, "WITH workflow_collector_backpressure"),
			strings.Contains(query, "FROM workflow_runs\nGROUP BY status"), strings.Contains(query, "FROM workflow_work_items\nGROUP BY status"):
			liveRuns++
		}
		return &fake.Rows{}, true
	}}
	store := postgres.NewStatusStore(queryer).WithSummaryReader(
		postgres.NewStatusSummaryReaderWithConfig(summary.ReadConfig{Enabled: true, StaleAfter: 33 * time.Second}))

	// No row yet: no coordinator gauges, stale 1 and NaN age.
	body := scrapeBody(t, store)
	if !strings.Contains(body, coordMarker("eshu_runtime_status_summary_stale", "1")) ||
		!strings.Contains(body, coordMarker("eshu_runtime_status_summary_age_seconds", "NaN")) ||
		strings.Contains(body, "eshu_runtime_coordinator_active_claims") {
		t.Fatalf("missing row must export the stale marker and no coordinator gauge:\n%s", body)
	}

	storedRows = coordinatorScrapeRow(t, now.Add(-10*time.Second))
	body = scrapeBody(t, store)
	for _, line := range []string{
		coordMarker("eshu_runtime_status_summary_stale", "0"),
		coordMarker("eshu_runtime_status_summary_age_seconds", "10"),
		`eshu_runtime_coordinator_active_claims{service_name="collector-git"} 3` + "\n",
		`eshu_runtime_coordinator_oldest_pending_age_seconds{service_name="collector-git"} 40` + "\n",
		`eshu_runtime_coordinator_work_item_status{service_name="collector-git",status="pending"} 7` + "\n",
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("fresh scrape lacks %q:\n%s", line, body)
		}
	}

	// The writer stops: the row is 100 s old at the next scrape.
	clock = now.Add(100 * time.Second)
	body = scrapeBody(t, store)
	for _, line := range []string{
		coordMarker("eshu_runtime_status_summary_stale", "1"),
		coordMarker("eshu_runtime_status_summary_age_seconds", "110"),
		`eshu_runtime_coordinator_active_claims{service_name="collector-git"} 3` + "\n",
		`eshu_runtime_coordinator_oldest_pending_age_seconds{service_name="collector-git"} 140` + "\n",
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("stale scrape lacks %q:\n%s", line, body)
		}
	}
	if liveRuns != 0 {
		t.Fatalf("%d live coordinator statements ran on scrapes; a scrape must never run them", liveRuns)
	}
}
