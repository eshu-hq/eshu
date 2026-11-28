// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// staleGate11234EdgeVerdicts maps every edge row seedStaleGate11234Edges
// plants to whether the gate fences it (true) or leaves it out of the
// stale set (false), and its domain for the filtered run.
var staleGate11234EdgeVerdicts = map[string]struct {
	fenced bool
	domain string
}{
	"edge-stale-pending":    {true, "deployment_mapping"},                       // older generation, pending
	"edge-stale-retrying":   {true, "deployment_mapping"},                       // older generation, retrying
	"edge-fanout-owed":      {false, "supply_chain_impact"},                     // #10060 fan-out-debt spare
	"edge-fanout-plain":     {true, "supply_chain_impact"},                      // no debt: fenced
	"edge-ready-neither":    {false, "aws_relationship_materialization"},        // phases under neither generation: held
	"edge-ready-active":     {true, "aws_relationship_materialization"},         // phases only under the active generation
	"edge-ready-own":        {true, "aws_relationship_materialization"},         // phases under its own generation
	"edge-live":             {false, "deployment_mapping"},                      // on the active generation
	"edge-crossplane-sole":  {false, "crossplane_satisfied_by_materialization"}, // no newer carrier: rebind fallback
	"edge-crossplane-stale": {true, "crossplane_satisfied_by_materialization"},  // newer pending carrier
	"edge-crossplane-carry": {false, "crossplane_satisfied_by_materialization"}, // the carrier itself
	"edge-tie-older":        {true, "deployment_mapping"},                       // equal ingested_at, smaller generation_id
	"edge-tie-newer":        {false, "deployment_mapping"},                      // equal ingested_at, larger generation_id
	"edge-newer-gen":        {false, "deployment_mapping"},                      // generation newer than the active one
	"edge-null-active":      {false, "deployment_mapping"},                      // scope without an active generation
	"edge-foreign-active":   {false, "deployment_mapping"},                      // active id names another scope's generation
	"edge-two-older-a":      {true, "deployment_mapping"},                       // two stale rows on two older generations
	"edge-two-older-b":      {true, "deployment_mapping"},
	"edge-gen-other-scope":  {false, "deployment_mapping"}, // generation row belongs to another scope
	"edge-filtered-out":     {true, "code_drifted"},        // fenced, but outside the domain filter
}

// staleGate11234EdgeDomains is the domain filter for the filtered run: it
// keeps every edge domain except aws_relationship_materialization, so the
// readiness rows must drop out of the stale set under the filter.
func staleGate11234EdgeDomains() []string {
	return []string{
		"deployment_mapping", "supply_chain_impact", "crossplane_satisfied_by_materialization",
		"advisory_group_projection", "workload_materialization",
	}
}

// seedStaleGate11234Edges plants the differential's edge cases next to the
// QA-shaped seed and returns the claim clock. Scope edge-s-<n> has active
// generation edge-a-<n> at t0 and older generation edge-o-<n> at t0-1h
// unless the case needs otherwise.
func seedStaleGate11234Edges(ctx context.Context, t testing.TB, conn db.Executor) time.Time {
	t.Helper()
	t0 := time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed edge %q: %v", strings.SplitN(strings.TrimSpace(query), "\n", 2)[0], err)
		}
	}
	scope := func(id, active string) {
		var activeID any
		if active != "" {
			activeID = active
		}
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`, id, t0, activeID)
	}
	gen := func(id, scopeID, status string, at time.Time) {
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint,
    observed_at, ingested_at, status, activated_at, superseded_at, payload)
VALUES ($1, $2, 'snapshot', 'edge', $3, $3, $4, $3, NULL, '{}'::jsonb)`, id, scopeID, at, status)
	}
	row := func(id, scopeID, generationID, status string, payload string) {
		var visible any
		if status == "retrying" {
			visible = t0.Add(-time.Minute)
		}
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, visible_at, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'scope', $1, $5, 0, $6, $7::jsonb, $8, $8)`,
			id, scopeID, generationID, staleGate11234EdgeVerdicts[id].domain, status, visible, payload, t0.Add(-2*time.Hour))
	}
	phase := func(scopeID, unit, generationID string) {
		exec(`INSERT INTO graph_projection_phase_state (scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, updated_at)
VALUES ($1, $2, $3, $3, 'cloud_resource_uid', 'canonical_nodes_committed', $4, $4)`, scopeID, unit, generationID, t0)
	}
	older := t0.Add(-time.Hour)
	// Scope 1: the plain fence, the fan-out spare, readiness and a live row.
	scope("edge-s-1", "edge-a-1")
	gen("edge-a-1", "edge-s-1", "active", t0)
	gen("edge-o-1", "edge-s-1", "superseded", older)
	row("edge-stale-pending", "edge-s-1", "edge-o-1", "pending", `{"source_system":"seed"}`)
	row("edge-stale-retrying", "edge-s-1", "edge-o-1", "retrying", `{"source_system":"seed"}`)
	row("edge-fanout-owed", "edge-s-1", "edge-o-1", "pending", `{"source_system":"seed","fanout_owed":"true"}`)
	row("edge-fanout-plain", "edge-s-1", "edge-o-1", "pending", `{"source_system":"seed"}`)
	row("edge-ready-neither", "edge-s-1", "edge-o-1", "pending", `{"entity_key":"unit-neither"}`)
	row("edge-ready-active", "edge-s-1", "edge-o-1", "pending", `{"entity_key":"unit-active"}`)
	row("edge-ready-own", "edge-s-1", "edge-o-1", "pending", `{"entity_key":"unit-own"}`)
	row("edge-live", "edge-s-1", "edge-a-1", "pending", `{"source_system":"seed"}`)
	phase("edge-s-1", "unit-active", "edge-a-1")
	phase("edge-s-1", "unit-own", "edge-o-1")
	// Scope 2: a sole stale Crossplane row (no carrier).
	scope("edge-s-2", "edge-a-2")
	gen("edge-a-2", "edge-s-2", "active", t0)
	gen("edge-o-2", "edge-s-2", "superseded", older)
	row("edge-crossplane-sole", "edge-s-2", "edge-o-2", "pending", `{}`)
	// Scope 3: a stale Crossplane row with a newer pending carrier.
	scope("edge-s-3", "edge-a-3")
	gen("edge-a-3", "edge-s-3", "active", t0)
	gen("edge-o-3", "edge-s-3", "superseded", older)
	row("edge-crossplane-stale", "edge-s-3", "edge-o-3", "pending", `{}`)
	row("edge-crossplane-carry", "edge-s-3", "edge-a-3", "pending", `{}`)
	// Scope 4: equal ingested_at, the tie broken by generation_id.
	scope("edge-s-4", "edge-a-4-m")
	gen("edge-a-4-m", "edge-s-4", "active", t0)
	gen("edge-a-4-a", "edge-s-4", "superseded", t0)
	gen("edge-a-4-z", "edge-s-4", "pending", t0)
	row("edge-tie-older", "edge-s-4", "edge-a-4-a", "pending", `{}`)
	row("edge-tie-newer", "edge-s-4", "edge-a-4-z", "pending", `{}`)
	// Scope 5: a row on a generation newer than the active one.
	scope("edge-s-5", "edge-a-5")
	gen("edge-a-5", "edge-s-5", "active", t0)
	gen("edge-n-5", "edge-s-5", "pending", t0.Add(time.Hour))
	row("edge-newer-gen", "edge-s-5", "edge-n-5", "pending", `{}`)
	// Scope 6: no active generation, with a non-superseded ('failed')
	// generation. #11703 fences retired scopes whose active_generation_id IS
	// NULL and stale_generation.status = 'superseded' (proven separately in
	// TestReducerSupersededNullActiveClaimAndSweepLive) while sparing 'failed'
	// generations so recover-failed-scopes keeps inspecting them.
	scope("edge-s-6", "")
	gen("edge-o-6", "edge-s-6", "failed", older)
	row("edge-null-active", "edge-s-6", "edge-o-6", "pending", `{}`)
	// Scope 7: the active id names scope 5's newer generation, not one of
	// its own (ingestion_scopes_active_generation_idx keeps active ids
	// unique, so it cannot be another scope's active generation).
	scope("edge-s-7", "edge-n-5")
	gen("edge-o-7", "edge-s-7", "superseded", older)
	row("edge-foreign-active", "edge-s-7", "edge-o-7", "pending", `{}`)
	// Scope 8: two stale rows on two different older generations, and a
	// stale row whose domain the filtered run excludes.
	scope("edge-s-8", "edge-a-8")
	gen("edge-a-8", "edge-s-8", "active", t0)
	gen("edge-o-8a", "edge-s-8", "superseded", older)
	gen("edge-o-8b", "edge-s-8", "superseded", older.Add(-time.Hour))
	row("edge-two-older-a", "edge-s-8", "edge-o-8a", "pending", `{}`)
	row("edge-two-older-b", "edge-s-8", "edge-o-8b", "retrying", `{}`)
	row("edge-filtered-out", "edge-s-8", "edge-o-8a", "pending", `{}`)
	// Scope 9: a row whose generation row exists but belongs to scope 1.
	// fact_work_items' foreign keys forbid a row without an
	// ingestion_scopes row or without a generation row, so this is the
	// closest legal "own generation missing" case.
	scope("edge-s-9", "edge-a-9")
	gen("edge-a-9", "edge-s-9", "active", t0)
	row("edge-gen-other-scope", "edge-s-9", "edge-o-1", "pending", `{}`)
	return t0.Add(time.Hour)
}

// staleGate11234AssertEdgeVerdicts checks that the stale set holds exactly
// the fenced edge rows (filtered by domain when the filter is set), so the
// differential's seeds exercise every gate branch instead of agreeing on
// nothing.
func staleGate11234AssertEdgeVerdicts(t *testing.T, staleSet []string, filtered bool) {
	t.Helper()
	got := map[string]bool{}
	for _, row := range staleSet {
		got[strings.TrimPrefix(row, "work_item_id=")] = true
	}
	allowed := map[string]bool{}
	for _, domain := range staleGate11234EdgeDomains() {
		allowed[domain] = true
	}
	for id, verdict := range staleGate11234EdgeVerdicts {
		want := verdict.fenced && (!filtered || allowed[verdict.domain])
		if got[id] != want {
			t.Errorf("edge row %s in stale set = %v, want %v (filtered=%v)", id, got[id], want, filtered)
		}
	}
}
