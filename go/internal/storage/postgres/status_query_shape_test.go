// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestActiveFactWorkItemsCTEResolvesScopeStateOnce guards issue #6794. The
// status snapshot evaluates activeFactWorkItemsCTE five times per request. When
// ingestion_scopes and the active scope_generations row were joined per work
// item, Postgres underestimated the (scope_id, generation_id) join by more
// than two orders of magnitude on a production-scale instance and chose
// per-row index nested loops: two index probes per work item, 4-6s per
// evaluation on a CPU-saturated database. The scope/active-generation pair is
// one row per scope, so it is resolved once in a CTE the planner hashes.
//
// AS MATERIALIZED is load-bearing: the scope-state CTE is referenced once, so
// PostgreSQL 12+ would otherwise inline it, and the measured inlined plan fell
// back to the same per-row nested loops.
func TestActiveFactWorkItemsCTEResolvesScopeStateOnce(t *testing.T) {
	t.Parallel()

	if !strings.Contains(activeFactWorkItemsCTE, "active_fact_work_items_scope_state AS MATERIALIZED (") {
		t.Fatalf("activeFactWorkItemsCTE must resolve per-scope active generation state once in a MATERIALIZED CTE:\n%s", activeFactWorkItemsCTE)
	}
	if !strings.Contains(activeFactWorkItemsCTE, "JOIN active_fact_work_items_scope_state AS scope_state") {
		t.Fatalf("active_fact_work_items must join the materialized scope state:\n%s", activeFactWorkItemsCTE)
	}
	_, body, found := strings.Cut(activeFactWorkItemsCTE, "active_fact_work_items AS (")
	if !found {
		t.Fatalf("activeFactWorkItemsCTE must define active_fact_work_items:\n%s", activeFactWorkItemsCTE)
	}
	for _, perRowJoin := range []string{
		"JOIN ingestion_scopes AS scope",
		"LEFT JOIN scope_generations AS active_generation",
	} {
		if strings.Contains(body, perRowJoin) {
			t.Fatalf("active_fact_work_items must not re-join %q per work item:\n%s", perRowJoin, body)
		}
	}
}

// TestDomainBacklogQueryReadsOnlyPendingSharedProjectionIntents guards issue
// #6794. shared_projection_intents keeps completed rows, so on a
// production-scale instance it holds hundreds of thousands of rows. The prior
// shape built a domain list with a UNION over every intent row and then
// LEFT JOINed every intent row back to it (two full passes, about 11.7s of a
// 13.1s query under load), even though only pending intents
// (completed_at IS NULL) and live leases can produce a backlog row. The
// aggregate must read pending intents once and FULL JOIN the live leases.
func TestDomainBacklogQueryReadsOnlyPendingSharedProjectionIntents(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{
		"shared_projection_domains AS",
		"LEFT JOIN shared_projection_intents",
		// The lease-only phantom count needed a per-domain probe; it is gone.
		"any_intent",
	} {
		if strings.Contains(domainBacklogQuery, forbidden) {
			t.Fatalf("domainBacklogQuery must not scan every shared projection intent (%q):\n%s", forbidden, domainBacklogQuery)
		}
	}
	for _, want := range []string{
		"shared_projection_pending AS (",
		"WHERE completed_at IS NULL\n  GROUP BY projection_domain",
		"FULL OUTER JOIN shared_projection_active_leases AS active",
	} {
		if !strings.Contains(domainBacklogQuery, want) {
			t.Fatalf("domainBacklogQuery missing pending-only shared projection shape %q:\n%s", want, domainBacklogQuery)
		}
	}
}

// TestCollectorFactEvidenceQueryMaterializesWorkflowInstances guards issue
// #6794. Postgres estimates the active_scopes CTE at one row (hundreds on a
// production-scale instance), so an inlined workflow_instances DISTINCT ON
// subquery was re-executed once per summary row (thousands of loops, about
// 1.9s under load). Materializing it evaluates the DISTINCT ON once.
func TestCollectorFactEvidenceQueryMaterializesWorkflowInstances(t *testing.T) {
	t.Parallel()

	if !strings.Contains(collectorFactEvidenceQuery, "workflow_instances AS MATERIALIZED (") {
		t.Fatalf("collectorFactEvidenceQuery must materialize workflow_instances:\n%s", collectorFactEvidenceQuery)
	}
}

// countingEmptyQueryer answers every query with zero rows and records the
// statements, so a test can count the round trips one status snapshot makes.
type countingEmptyQueryer struct{ queries []string }

func (q *countingEmptyQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	return &fakeRows{}, nil
}

// TestReadStatusSnapshotEvaluatesActiveWorkOnce guards the #6794 dedupe: the
// stage counts, queue snapshot, domain backlog, conflict blockages, and latest
// failure reads all derive from active_fact_work_items, so one snapshot must
// evaluate it in a single statement (one round trip) instead of five.
func TestReadStatusSnapshotEvaluatesActiveWorkOnce(t *testing.T) {
	t.Parallel()

	queryer := &countingEmptyQueryer{}
	if _, err := NewStatusStore(queryer).ReadStatusSnapshotFiltered(
		context.Background(), time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), statuspkg.FullSnapshotSelection(),
	); err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	evaluations := 0
	for _, query := range queryer.queries {
		if strings.Contains(query, "active_fact_work_items AS") {
			evaluations++
		}
	}
	if evaluations != 1 {
		t.Fatalf("active_fact_work_items evaluated in %d statements per snapshot, want 1", evaluations)
	}
	if got, want := len(queryer.queries), 25; got != want {
		t.Fatalf("status snapshot issued %d queries, want %d", got, want)
	}
}

// TestActiveWorkSummaryDecoderRejectsMissingOrMistypedKeys guards the #6794
// review finding F-05: the five standalone reads scanned positional columns and
// failed loudly on a missing or mistyped column. The summary decodes JSON rows,
// so a renamed SELECT alias or a type change must be an error, never a silent 0.
func TestActiveWorkSummaryDecoderRejectsMissingOrMistypedKeys(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		section string
		raw     string
	}{
		"stage missing count":           {activeWorkSectionStage, `{"stage":"reducer","status":"pending"}`},
		"backlog renamed in-flight key": {activeWorkSectionBacklog, `{"domain":"d","outstanding_count":1,"inflight_count":0,"retrying_count":0,"dead_letter_count":0,"failed_count":0,"oldest_outstanding_age_seconds":1.0}`},
		"queue mistyped applied flag":   {activeWorkSectionQueue, `{"total_count":1,"outstanding_count":1,"pending_count":1,"in_flight_count":0,"retrying_count":0,"succeeded_count":0,"dead_letter_count":0,"failed_count":0,"provenance_edge_identity_upgrade_applied":"yes","provenance_edge_identity_upgrade_required":0,"oldest_outstanding_age_seconds":0,"overdue_claim_count":0}`},
		"blockage count is text":        {activeWorkSectionBlockage, `{"stage":"reducer","domain":"d","conflict_domain":"c","conflict_key":"k","blocked_count":"2","oldest_blocked_age_seconds":1.0}`},
		"failure missing generation id": {activeWorkSectionFailure, `{"stage":"reducer","domain":"d","status":"failed","work_item_id":"w","scope_id":"s","failure_class":"x","failure_message":"","failure_details":"","updated_at":null}`},
		"unknown section":               {"bogus", `{}`},
	} {
		summary := activeWorkSummary{}
		if err := summary.add(tc.section, tc.raw); err == nil {
			t.Errorf("%s: add() error = nil, want a decode error", name)
		}
	}

	// updated_at may be null, exactly as the standalone scanner allowed.
	summary := activeWorkSummary{}
	if err := summary.add(activeWorkSectionFailure, `{"stage":"reducer","domain":"d","status":"failed","work_item_id":"w","scope_id":"s","generation_id":"g","failure_class":"x","failure_message":"","failure_details":"","updated_at":null}`); err != nil {
		t.Fatalf("failure row with null updated_at: %v", err)
	}
}

// TestSelectiveActiveWorkProbesUsePerRowFilter guards #6794 review finding
// F-03. The drain EXISTS (activeReducerGraphWorkQuery, polled by the code-call
// quiescence loop) and the producer write-backpressure gate
// (reducerGraphWriteTimeoutDepthQuery) filter to a handful of rows. With the
// materialized scope-state CTE they had to build one row per ingestion scope
// before matching: on a 5k-scope fixture the drain EXISTS went from 0.1ms to
// 5ms with one qualifying row, and the gate from 0.6ms to 4.8ms with 50. These
// selective probes keep the per-row join form; the full-set aggregates keep
// the materialized form, which reads half the buffers there.
func TestSelectiveActiveWorkProbesUsePerRowFilter(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"activeReducerGraphWorkQuery":        activeReducerGraphWorkQuery,
		"reducerGraphWriteTimeoutDepthQuery": reducerGraphWriteTimeoutDepthQuery,
	} {
		if strings.Contains(query, "active_fact_work_items_scope_state") {
			t.Fatalf("%s must not materialize per-scope state before its selective filter:\n%s", name, query)
		}
		if !strings.Contains(query, "LEFT JOIN scope_generations AS active_generation") {
			t.Fatalf("%s must use the per-row active-work filter:\n%s", name, query)
		}
	}
}

// TestReducerConflictBlockageFiltersEligibleByHashedLeaseSet guards #6794. The
// blockage section used to join every eligible reducer row to a live lease on
// the same conflict key. The eligible CTE's row estimate is far below its real
// size, so with missing or stale statistics the planner ran that join as a
// nested loop, rescanning one side once per row of the other: the cost grew
// with eligible rows times leases. blocked is now a filter on eligible, an
// uncorrelated IN over the live-lease CTE that PostgreSQL runs as a hashed
// SubPlan, so there is no join method for the planner to choose. The
// COALESCE(..., FALSE) fence keeps the IN from being pulled up into a
// semi-join, which would be a join again.
func TestReducerConflictBlockageFiltersEligibleByHashedLeaseSet(t *testing.T) {
	t.Parallel()

	cte := func(name, next string) string {
		t.Helper()
		start := strings.Index(activeWorkSummaryQuery, "\n"+name+" AS ")
		end := strings.Index(activeWorkSummaryQuery, "\n"+next+" AS ")
		if start < 0 || end < 0 || start > end {
			t.Fatalf("summary query must define %s before %s:\n%s", name, next, activeWorkSummaryQuery)
		}
		return activeWorkSummaryQuery[start:end]
	}
	eligible := cte("eligible", "inflight_leases")
	leases := cte("inflight_leases", "blocked")
	blocked := cte("blocked", "readiness_blocked")

	for name, pair := range map[string][2]string{
		"eligible key":       {eligible, "COALESCE(conflict_key, scope_id) AS conflict_key"},
		"lease key":          {leases, "COALESCE(conflict_key, scope_id) AS conflict_key"},
		"lease CTE":          {leases, "inflight_leases AS MATERIALIZED ("},
		"lease source":       {leases, "FROM fact_work_items"},
		"lease stage":        {leases, "stage = 'reducer'"},
		"lease status":       {leases, "status IN ('claimed', 'running')"},
		"lease expiry":       {leases, "claim_until > $1"},
		"filter on eligible": {blocked, "FROM eligible"},
		"fenced IN":          {blocked, "WHERE COALESCE("},
		"lease set":          {blocked, "(conflict_domain, conflict_key) IN (SELECT conflict_domain, conflict_key FROM inflight_leases)"},
		"fence default":      {blocked, "FALSE)"},
	} {
		if !strings.Contains(pair[0], pair[1]) {
			t.Fatalf("%s: missing %q in:\n%s", name, pair[1], pair[0])
		}
	}
	if strings.Contains(blocked, "JOIN") || strings.Contains(blocked, "fact_work_items") || strings.Contains(blocked, " OVER ") {
		t.Fatalf("blocked must filter eligible, not join or window:\n%s", blocked)
	}
	if strings.Contains(activeWorkSummaryQuery, "lease_keyed") {
		t.Fatalf("summary query still defines lease_keyed:\n%s", activeWorkSummaryQuery)
	}
}
