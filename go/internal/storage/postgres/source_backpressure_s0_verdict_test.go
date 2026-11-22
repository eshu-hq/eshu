// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestSourceBackpressureProbeMatchesGroupedAndVerdicts is the S0
// row-equivalence and verdict-simulation probe (#10882): on a small
// multi-source seed with payload attribution, scope-fallback attribution,
// and a single-poison-row source, the bounded consult probe returns the
// same age and depth per source as the grouped publication signal, and
// the store verdicts engage, release, and floor-suppress as decided.
// It also runs the family-to-source census the design requires: three
// families resolving to one, two, and zero pending sources.
func TestSourceBackpressureProbeMatchesGroupedAndVerdicts(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the source-backpressure S0 verdict probe")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated postgres connection: %v", err)
	}
	defer func() { _ = sqlConn.Close() }()
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("source_bp_s0v_%d", time.Now().UnixNano())
	defer func() { _, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create replay schema: %v", err)
	}
	for _, def := range []string{"workflow_control_plane", "workflow_claim_pacing"} {
		if _, err := conn.ExecContext(ctx, MigrationSQL(def)); err != nil {
			t.Fatalf("create %s: %v", def, err)
		}
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set verdict search_path: %v", err)
	}

	now := time.Now().UTC()
	seedS0VerdictShapes(t, ctx, conn, now)

	store := NewWorkflowControlStore(conn)
	cfg := sourceBackpressureTestConfig()

	// Row-equivalence: bounded probe vs grouped signal per source.
	grouped, err := store.sourceClaimableSignals(ctx, cfg, now)
	if err != nil {
		t.Fatalf("grouped signals: %v", err)
	}
	bySource := map[string]workflow.SourceQueueSignal{}
	for _, signal := range grouped {
		bySource[signal.SourceSystem] = signal
	}
	for _, source := range []string{"vuln-intel", "git", "npm", "poison", "busy"} {
		bounded, err := store.sourceClaimableSignalForSource(ctx, cfg, source, now)
		if err != nil {
			t.Fatalf("bounded probe for %s: %v", source, err)
		}
		want, ok := bySource[source]
		if !ok {
			t.Fatalf("grouped signal missing source %s (have %v)", source, bySource)
		}
		if bounded.ClaimableDepth != want.ClaimableDepth {
			t.Fatalf("bounded depth for %s = %d, want grouped %d", source, bounded.ClaimableDepth, want.ClaimableDepth)
		}
		if bounded.RowsPastEngageAge != want.RowsPastEngageAge || bounded.RowsPastReleaseAge != want.RowsPastReleaseAge {
			t.Fatalf("bounded aged rows for %s = (engage %d, release %d), want grouped (%d, %d)",
				source, bounded.RowsPastEngageAge, bounded.RowsPastReleaseAge, want.RowsPastEngageAge, want.RowsPastReleaseAge)
		}
		if bounded.OldestClaimableAge != want.OldestClaimableAge {
			t.Fatalf("bounded age for %s = %s, want grouped %s", source, bounded.OldestClaimableAge, want.OldestClaimableAge)
		}
		t.Logf("S0 verdict seed: source=%s depth=%d age=%s past_engage=%d past_release=%d", source, bounded.ClaimableDepth, bounded.OldestClaimableAge, bounded.RowsPastEngageAge, bounded.RowsPastReleaseAge)
	}

	// Verdict simulation over the seeded shapes.
	vuln, err := store.CheckSourceBackpressureForSource(ctx, "vuln-intel", cfg, now)
	if err != nil {
		t.Fatalf("vuln-intel consult: %v", err)
	}
	if !vuln.Slowed || vuln.Reason != workflow.SourceBackpressureReasonSourceBehind {
		t.Fatalf("vuln-intel verdict = %+v, want slowed with source_behind", vuln)
	}
	git, err := store.CheckSourceBackpressureForSource(ctx, "git", cfg, now)
	if err != nil {
		t.Fatalf("git consult: %v", err)
	}
	if git.Slowed {
		t.Fatalf("git verdict = %+v, want unslowed (fresh queue)", git)
	}
	poison, err := store.CheckSourceBackpressureForSource(ctx, "poison", cfg, now)
	if err != nil {
		t.Fatalf("poison consult: %v", err)
	}
	if poison.Slowed {
		t.Fatalf("poison verdict = %+v, want unslowed (single old row below depth floor)", poison)
	}
	busy, err := store.CheckSourceBackpressureForSource(ctx, "busy", cfg, now)
	if err != nil {
		t.Fatalf("busy consult: %v", err)
	}
	if busy.Slowed {
		t.Fatalf("busy verdict = %+v, want unslowed (one old row among 200 fresh rows is below the aged-row floor)", busy)
	}
	// npm rows carry no payload source and resolve via the scope fallback.
	npm, err := store.CheckSourceBackpressureForSource(ctx, "npm", cfg, now)
	if err != nil {
		t.Fatalf("npm consult: %v", err)
	}
	if !npm.Slowed {
		t.Fatalf("npm verdict = %+v, want slowed (scope-fallback attribution, old and deep)", npm)
	}

	// Release: once the vuln queue drains to fresh rows, the engaged
	// latch re-reads unslowed and the release writes the latch row.
	fresh := now.Format(time.RFC3339)
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items
SET created_at = $1, updated_at = $1 WHERE scope_id = 's0v-vuln'`, fresh); err != nil {
		t.Fatalf("drain vuln queue: %v", err)
	}
	released, err := store.CheckSourceBackpressureForSource(ctx, "vuln-intel", cfg, now)
	if err != nil {
		t.Fatalf("vuln-intel release consult: %v", err)
	}
	if released.Slowed {
		t.Fatalf("vuln-intel verdict after drain = %+v, want released", released)
	}
	// Resume consults from a released latch pace nothing.
	resumed, err := store.CheckSourceBackpressureForSource(ctx, "vuln-intel", cfg, now)
	if err != nil {
		t.Fatalf("vuln-intel resume consult: %v", err)
	}
	if resumed.Slowed {
		t.Fatalf("vuln-intel verdict on resume = %+v, want unslowed", resumed)
	}

	// Family-to-source census: three families with distinct shapes.
	seedS0Families(t, ctx, conn, now)
	censusStart := time.Now()
	for family, want := range map[string][]string{
		"vuln":  {"vuln-intel"},
		"mixed": {"git", "vuln-intel"},
		"idle":  {},
	} {
		var kind, instance string
		if family == "idle" {
			kind, instance = "idle", "idle-primary"
		} else {
			kind, instance = family, family+"-primary"
		}
		rows, err := sqlConn.QueryContext(ctx, familyPendingSourcesQuery, kind, instance)
		if err != nil {
			t.Fatalf("census for %s: %v", family, err)
		}
		var got []string
		for rows.Next() {
			var source string
			if err := rows.Scan(&source); err != nil {
				_ = rows.Close()
				t.Fatalf("scan census: %v", err)
			}
			got = append(got, source)
		}
		_ = rows.Close()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("family %s sources = %v, want %v", family, got, want)
		}
	}
	t.Logf("S0 family census: 3 families resolved in %s", time.Since(censusStart))
}

// seedS0VerdictShapes plants one scope per source with pinned generations:
// vuln-intel old (30 rows) and deep, git fresh (30 rows), npm with empty
// payload sources resolved via the scope fallback (30 old rows), and a
// single old poison row below the depth floor.
func seedS0VerdictShapes(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time) {
	t.Helper()
	type shape struct {
		scope, source, payload string
		count                  int
		age                    time.Duration
	}
	shapes := []shape{
		{scope: "s0v-vuln", source: "vuln-intel", payload: "vuln-intel", count: 30, age: 30 * 24 * time.Hour},
		{scope: "s0v-git", source: "git", payload: "git", count: 30, age: 30 * time.Minute},
		{scope: "s0v-npm", source: "npm", payload: "", count: 30, age: 30 * 24 * time.Hour},
		{scope: "s0v-poison", source: "poison", payload: "poison", count: 1, age: 30 * 24 * time.Hour},
		// busy: one poisoned old row among 200 fresh rows (#10882 F5). The
		// total depth clears the floor, but only one row is past the engage
		// age, so the source must not slow.
		{scope: "s0v-busy-fresh", source: "busy", payload: "busy", count: 200, age: time.Minute},
		{scope: "s0v-busy-old", source: "busy", payload: "busy", count: 1, age: 3 * 24 * time.Hour},
	}
	for _, shape := range shapes {
		if _, err := conn.ExecContext(ctx, `INSERT INTO ingestion_scopes (
scope_id, scope_kind, source_system, source_key, parent_scope_id,
collector_kind, partition_key, status, active_generation_id, payload,
observed_at, ingested_at) VALUES ($1,'repository',$2,$1,NULL,'git',$1,'active',$3,'{}'::jsonb,$4,$4)`,
			shape.scope, shape.source, shape.scope+"-gen", now); err != nil {
			t.Fatalf("insert verdict scope %s: %v", shape.scope, err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO scope_generations (
generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
ingested_at, status, activated_at, superseded_at, payload) VALUES ($1,$2,'snapshot','s0v',$3,$3,'active',$3,NULL,'{}'::jsonb)`,
			shape.scope+"-gen", shape.scope, now); err != nil {
			t.Fatalf("insert verdict generation %s: %v", shape.scope, err)
		}
		created := now.Add(-shape.age)
		ts := created.Format(time.RFC3339)
		sts := now.Format(time.RFC3339)
		for i := 0; i < shape.count; i++ {
			payload := fmt.Sprintf(`{"entity_key":"s0v","source_system":%s}`, payloadJSON(shape.payload))
			if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
conflict_key, status, attempt_count, lease_owner, claim_until, visible_at,
last_attempt_at, next_attempt_at, failure_class, failure_message,
failure_details, payload, created_at, updated_at) VALUES (
$1,$2,$3,'reducer','supply_chain_impact','scope',NULL,'pending',0,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,$4::jsonb,$5,$6)`,
				fmt.Sprintf("s0v-work-%s-%d", shape.scope, i), shape.scope, shape.scope+"-gen",
				payload, ts, sts); err != nil {
				t.Fatalf("insert verdict work %s/%d: %v", shape.scope, i, err)
			}
		}
	}
}

// payloadJSON renders one payload source_system value: empty stays an
// empty string (the scope-fallback arm), never null.
func payloadJSON(source string) string {
	if source == "" {
		return `""`
	}
	return `"` + source + `"`
}

// seedS0Families plants workflow runs and pending items for the census:
// vuln holds one source, mixed holds two, and idle holds none.
func seedS0Families(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time) {
	t.Helper()
	ts := now.Format(time.RFC3339)
	for _, run := range []string{"s0v-run-vuln", "s0v-run-mixed"} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO workflow_runs (
run_id, trigger_kind, status, requested_scope_set, requested_collector,
created_at, updated_at, finished_at) VALUES ($1,'snapshot','running','[]',$2,$3,$3,NULL)`,
			run, "test", ts); err != nil {
			t.Fatalf("insert census run %s: %v", run, err)
		}
	}
	items := []struct {
		id, run, kind, instance, source string
	}{
		{"s0v-item-v1", "s0v-run-vuln", "vuln", "vuln-primary", "vuln-intel"},
		{"s0v-item-m1", "s0v-run-mixed", "mixed", "mixed-primary", "git"},
		{"s0v-item-m2", "s0v-run-mixed", "mixed", "mixed-primary", "vuln-intel"},
	}
	for _, item := range items {
		if _, err := conn.ExecContext(ctx, `INSERT INTO workflow_work_items (
work_item_id, run_id, collector_kind, collector_instance_id, source_system,
scope_id, tenant_id, workspace_id, subject_class, policy_revision_hash,
acceptance_unit_id, source_run_id, generation_id, fairness_key, status,
attempt_count, current_claim_id, current_fencing_token, current_owner_id,
lease_expires_at, visible_at, last_claimed_at, last_completed_at,
last_failure_class, last_failure_message, created_at, updated_at) VALUES (
$1,$2,$3,$4,$5,'s0v-scope','','','','','s0v-accept','s0v-run','s0v-gen','', 'pending',
0,NULL,0,NULL,NULL,NULL,NULL,NULL,NULL,NULL,$6,$6)`,
			item.id, item.run, item.kind, item.instance, item.source, ts); err != nil {
			t.Fatalf("insert census item %s: %v", item.id, err)
		}
	}
}
