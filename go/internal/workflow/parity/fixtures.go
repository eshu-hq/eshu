// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Suite seeds and clocks every scenario identically on both legs:
// deterministic IDs under one run prefix, one captured clock per
// scenario, one fixed rotation salt. No wall-clock reads and no random
// IDs inside scenario bodies.
type Suite struct {
	T     *testing.T
	RunID string
	Now   time.Time
	Lease time.Duration
}

// parityClock is the fixed scenario clock. Every timestamp derives from
// it, so both legs and every rerun observe identical values. Lease and
// reap math stays relative to this clock; no backend validates it against
// wall time on the exercised paths.
var parityClock = time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC)

// NewSuite builds one scenario run: deterministic IDs under a fixed run
// prefix plus one clock shared by both legs of the scenario. The default
// clock is fixed for rerunnability. Against a long-lived deployment
// whose background reaper ticks on wall time (PARITY_LIVE_CLOCK=1), the
// clock is captured once per scenario instead: a fixed past clock would
// leave every lease already expired for the background reaper. The run
// prefix keeps every seeded row attributable to its scenario for cleanup
// and for filtering shared-table reads on legs that cannot isolate by
// schema.
func NewSuite(t *testing.T, name string) *Suite {
	t.Helper()
	now := parityClock
	if os.Getenv("PARITY_LIVE_CLOCK") == "1" {
		now = time.Now().UTC().Truncate(time.Millisecond)
	}
	return &Suite{
		T:     t,
		RunID: "parity-" + name + "-run",
		Now:   now,
		Lease: 5 * time.Minute,
	}
}

// ItemID returns the deterministic work-item id for slot n.
func (s *Suite) ItemID(n int) string {
	return fmt.Sprintf("%s-item-%02d", s.RunID, n)
}

// Selector returns a deterministic claim selector for an owner.
func (s *Suite) Selector(owner, claim string) workflow.ClaimSelector {
	return workflow.ClaimSelector{
		CollectorKind:       scope.CollectorKind("jira"),
		CollectorInstanceID: "jira-primary",
		OwnerID:             owner,
		ClaimID:             claim,
		RotationSalt:        "parity-fixed-salt",
	}
}

// SeedRun creates the suite run on a backend.
func (s *Suite) SeedRun(ctx context.Context, backend Backend) {
	s.T.Helper()
	run := workflow.Run{
		RunID: s.RunID, TriggerKind: workflow.TriggerKindSchedule,
		Status:    workflow.RunStatusCollectionActive,
		CreatedAt: s.Now, UpdatedAt: s.Now,
	}
	if err := backend.CreateRun(ctx, run); err != nil {
		s.T.Fatalf("%s: seed run: %v", backend.Name(), err)
	}
}

// SeedItems enqueues count pending items with deterministic identity.
func (s *Suite) SeedItems(ctx context.Context, backend Backend, count int) {
	s.T.Helper()
	items := make([]workflow.WorkItem, 0, count)
	for i := range count {
		id := s.ItemID(i)
		items = append(items, workflow.WorkItem{
			WorkItemID: id, RunID: s.RunID,
			CollectorKind:       scope.CollectorKind("jira"),
			CollectorInstanceID: "jira-primary",
			SourceSystem:        "jira", ScopeID: "scope-" + id,
			AcceptanceUnitID: "au-" + id, SourceRunID: "sr-" + id, GenerationID: "gen-" + id,
			Status:    workflow.WorkItemStatusPending,
			CreatedAt: s.Now, UpdatedAt: s.Now,
		})
	}
	if err := backend.EnqueueWorkItems(ctx, items); err != nil {
		s.T.Fatalf("%s: seed %d items: %v", backend.Name(), count, err)
	}
}

// Mutation builds a fenced mutation for a claim epoch.
func (s *Suite) Mutation(itemID, claimID string, token int64, owner string) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID: itemID, ClaimID: claimID,
		FencingToken: token, OwnerID: owner,
		ObservedAt: s.Now, LeaseDuration: s.Lease, VisibleAt: s.Now,
	}
}

// Outcome records one backend's canonical observable events for a
// scenario. Only codes and durable values enter the record: message text
// is backend detail and never compared, but it is kept alongside so a
// mismatch prints the underlying errors for diagnosis.
type Outcome struct {
	Events []string
	Texts  []string
}

// Record appends a formatted event.
func (o *Outcome) Record(format string, args ...any) {
	o.Events = append(o.Events, fmt.Sprintf(format, args...))
	o.Texts = append(o.Texts, "")
}

// Call records the code of one backend call.
func (o *Outcome) Call(op string, err error) {
	o.Events = append(o.Events, fmt.Sprintf("%s=%s", op, Classify(err)))
	if err == nil {
		o.Texts = append(o.Texts, "")
	} else {
		o.Texts = append(o.Texts, fmt.Sprintf("%s: %v", op, err))
	}
}

// Equal fails the test when two legs disagree, printing both records
// with the underlying error texts.
func (o *Outcome) Equal(t *testing.T, scenario string, other *Outcome) {
	t.Helper()
	if len(o.Events) != len(other.Events) {
		t.Fatalf("%s: event counts differ:\nlegA=%v\nlegB=%v", scenario, o.Events, other.Events)
	}
	for i := range o.Events {
		if o.Events[i] != other.Events[i] {
			t.Fatalf("%s: event %d differs:\nlegA=%v\nlegB=%v\nlegA texts=%v\nlegB texts=%v",
				scenario, i, o.Events, other.Events, o.Texts, other.Texts)
		}
	}
}

// satelliteTables are the satellite-owned tables the harness truncates so
// one scenario never reads another's rows. The pin is a column on
// workflow_claims, not a table, so it needs no entry.
var satelliteTables = []string{
	"workflow_claims",
	"workflow_run_progress",
	"workflow_run_completeness",
	"workflow_work_items",
	"workflow_runs",
	"collector_instances",
	"workflow_claim_pacing",
	"component_quota_windows",
}

// truncateSatelliteTables is the TRUNCATE statement ResetSatellite runs. It
// is a constant, not built by string joining, so no caller input can reach
// the query text. TestTruncateCoversSatelliteTables pins it to
// satelliteTables.
const truncateSatelliteTables = "TRUNCATE workflow_claims, " +
	"workflow_run_progress, workflow_run_completeness, workflow_work_items, " +
	"workflow_runs, collector_instances, workflow_claim_pacing, " +
	"component_quota_windows RESTART IDENTITY CASCADE"

// ResetSatellite clears satellite state on the database named by dsn. It
// runs at suite start and at suite end (cleanup), never between the two
// legs of one scenario: scenarios use disjoint run IDs.
//
// Local legs truncate every satellite table. On a dark deployment
// (PARITY_DARK=1) it instead deletes only rows from this suite's
// deterministic run IDs: truncating a shared dark database would wipe
// rows the suite does not own. Dark cleanup additionally relies on each
// scenario's drain discipline, and the caller verifies the depth
// readback afterwards.
func ResetSatellite(t *testing.T, dsn string) {
	t.Helper()
	if strings.TrimSpace(dsn) == "" {
		t.Skip("PARITY_SATELLITE_DSN is not set; skipping satellite parity leg")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open satellite db: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if os.Getenv("PARITY_DARK") == "1" {
		resetDarkSuiteRows(t, ctx, db)
		return
	}
	if _, err := db.ExecContext(ctx, truncateSatelliteTables); err != nil {
		t.Fatalf("truncate satellite tables: %v", err)
	}
}

// resetDarkSuiteRows deletes only this suite's synthetic rows, child
// first. Every seeded id carries the parity- run prefix, so the filter
// cannot meet another tenant's rows.
func resetDarkSuiteRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	statements := []string{
		"DELETE FROM workflow_claims WHERE work_item_id LIKE 'parity-%'",
		"DELETE FROM workflow_run_progress WHERE run_id LIKE 'parity-%'",
		"DELETE FROM workflow_run_completeness WHERE run_id LIKE 'parity-%'",
		"DELETE FROM workflow_work_items WHERE work_item_id LIKE 'parity-%'",
		"DELETE FROM workflow_runs WHERE run_id LIKE 'parity-%'",
	}
	for _, query := range statements {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("delete suite rows: %v", err)
		}
	}
}

// CountSuiteRows reports how many synthetic rows of this suite remain on
// the satellite database. Dark runs assert zero afterwards.
func CountSuiteRows(t *testing.T, dsn string) int {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open satellite db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var total int
	queries := []string{
		"SELECT count(*) FROM workflow_claims WHERE work_item_id LIKE 'parity-%'",
		"SELECT count(*) FROM workflow_work_items WHERE work_item_id LIKE 'parity-%'",
		"SELECT count(*) FROM workflow_runs WHERE run_id LIKE 'parity-%'",
	}
	for _, query := range queries {
		var n int
		if err := db.QueryRowContext(context.Background(), query).Scan(&n); err != nil {
			t.Fatalf("count suite rows: %v", err)
		}
		total += n
	}
	return total
}

// OpenSatellite opens the HTTP leg against the satellite base URL.
func OpenSatellite(t *testing.T, baseURL string) Backend {
	t.Helper()
	if strings.TrimSpace(baseURL) == "" {
		t.Skip("PARITY_SATELLITE_URL is not set; skipping satellite parity leg")
	}
	return &satelliteBackend{client: workflow.NewClaimPlaneClient(baseURL, nil)}
}
