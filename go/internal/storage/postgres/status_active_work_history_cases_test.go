// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// historyEdgeAsOf is the status clock of the edge cases ported from the #7009
// C3 shim fixture; their timestamps and expected values are relative to it.
var historyEdgeAsOf = time.Date(2026, time.October, 5, 2, 1, 41, 0, time.UTC)

// historyCase seeds one state of the active-work summary differential and
// lists hand-derived values the shipped query must report for it. Every case
// starts from an empty work, scope, intent, lease, and readiness state; the
// bootstrap's upgrade markers are kept.
type historyCase struct {
	name   string
	asOf   time.Time
	seed   func(ctx context.Context, t *testing.T, conn *sql.Conn)
	expect []historyExpectation
	// wantMode is the gate branch the shipped threshold must take on this
	// state ("" when the case leaves pg_stats unanalyzed, so the branch is
	// whatever earlier statistics say); analyzed cases also check the gate
	// estimate against the true live fraction.
	wantMode string
	analyzed bool
}

// historyExpectation is one value read from the shipped query's result:
// section row ordinal (0 means "count the section's rows") and JSON field.
type historyExpectation struct {
	section string
	ordinal int64
	field   string
	want    string
}

// historyWork is one fact_work_items row. Empty strings map to NULL (or to
// the column default for conflict_domain and payload); timestamps are UTC
// text.
type historyWork struct {
	id, scope, gen, stage, domain, status string
	conflictDomain, conflictKey           string
	claimUntil, visibleAt                 string
	failureClass, failureMessage          string
	failureDetails                        string
	provenance                            bool
	payload                               string
	created, updated                      string
}

func historyExec(ctx context.Context, t *testing.T, conn *sql.Conn, query string, args ...any) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, query)
	}
}

// resetHistoryCase empties every table the summary reads, except the upgrade
// markers the bootstrap records.
func resetHistoryCase(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	historyExec(ctx, t, conn, `DELETE FROM fact_work_items;
DELETE FROM graph_projection_phase_state;
DELETE FROM shared_projection_intents;
DELETE FROM shared_projection_partition_leases;
DELETE FROM scope_generations;
DELETE FROM ingestion_scopes`)
}

func historyScopes(ctx context.Context, t *testing.T, conn *sql.Conn, pairs ...[2]string) {
	t.Helper()
	for _, pair := range pairs {
		historyExec(ctx, t, conn, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system,
  source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, TIMESTAMPTZ '2026-10-04 00:00:00+00',
  TIMESTAMPTZ '2026-10-04 00:00:00+00', 'active', NULLIF($2::text, ''), '{}'::jsonb)`, pair[0], pair[1])
	}
}

func historyGenerations(ctx context.Context, t *testing.T, conn *sql.Conn, rows ...[4]string) {
	t.Helper()
	for _, row := range rows {
		historyExec(ctx, t, conn, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind,
  observed_at, ingested_at, status, payload)
VALUES ($1::text, $2::text, 'snapshot', ($3::text)::timestamptz, ($3::text)::timestamptz, $4::text, '{}'::jsonb)`,
			row[0], row[1], row[2], row[3])
	}
}

func historyWorkRows(ctx context.Context, t *testing.T, conn *sql.Conn, rows ...historyWork) {
	t.Helper()
	for _, w := range rows {
		historyExec(ctx, t, conn, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id,
  stage, domain, status, conflict_domain, conflict_key, claim_until, visible_at, failure_class,
  failure_message, failure_details, provenance_edge_identity_upgrade_required, payload, created_at, updated_at)
VALUES ($1::text, $2::text, $3::text, $4::text, $5::text, $6::text,
  COALESCE(NULLIF($7::text, ''), 'scope'), NULLIF($8::text, ''),
  NULLIF($9::text, '')::timestamptz, NULLIF($10::text, '')::timestamptz,
  NULLIF($11::text, ''), NULLIF($12::text, ''), NULLIF($13::text, ''), $14::boolean,
  COALESCE(NULLIF($15::text, ''), '{}')::jsonb, ($16::text)::timestamptz, ($17::text)::timestamptz)`,
			w.id, w.scope, w.gen, w.stage, w.domain, w.status, w.conflictDomain, w.conflictKey,
			w.claimUntil, w.visibleAt, w.failureClass, w.failureMessage, w.failureDetails,
			w.provenance, w.payload, w.created, w.updated)
	}
}

// staleScopeGenerations is the shim's two-scope layout: sa has an old
// generation, an equal-ingested-time tie below and above its active
// generation; sb's generation lets a work row on scope sa name another
// scope's generation (the composite scope/generation mismatch).
func staleScopeGenerations(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	historyScopes(ctx, t, conn, [2]string{"sa", "ga2"}, [2]string{"sb", "gb1"})
	historyGenerations(ctx, t, conn,
		[4]string{"ga0", "sa", "2026-10-05 00:00:00+00", "superseded"},
		[4]string{"ga1", "sa", "2026-10-05 02:00:00+00", "superseded"},
		[4]string{"ga2", "sa", "2026-10-05 02:00:00+00", "active"},
		[4]string{"ga3", "sa", "2026-10-05 02:00:00+00", "pending"},
		[4]string{"gb1", "sb", "2026-10-05 02:00:00+00", "active"})
}

const (
	historyCreated = "2026-10-05 01:01:41+00"
	historyUpdated = "2026-10-05 02:00:00+00"
)

// staleShimRows are the shim's stale_equal_time_and_composite rows.
func staleShimRows(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	staleScopeGenerations(ctx, t, conn)
	row := func(id, gen, stage, domain, status, key string) historyWork {
		return historyWork{
			id: id, scope: "sa", gen: gen, stage: stage, domain: domain, status: status,
			conflictKey: key, created: historyCreated, updated: historyUpdated,
		}
	}
	historyWorkRows(ctx, t, conn,
		row("w-old", "ga0", "reducer", "d", "pending", ""),
		row("w-tie-low", "ga1", "reducer", "d", "retrying", ""),
		row("w-tie-high", "ga3", "reducer", "d", "pending", ""),
		row("w-old-running", "ga0", "reducer", "d", "running", "running-key"),
		row("w-proj-old", "ga0", "projector", "d", "pending", ""),
		row("w-mismatch", "gb1", "reducer", "d", "pending", ""),
		row("w-unknown", "ga2", "novel", "weird", "mystery", ""))
}

// historyEdgeCases ports the C3 shim edge fixture (7009-c3-shim-src/edge) to
// the bootstrapped schema and adds the history-row cases the grouped pass
// must fence: terminal and quarantined rows on stale generations and on a
// mismatched scope/generation pair.
func historyEdgeCases() []historyCase {
	return []historyCase{
		{
			name: "empty",
			asOf: historyEdgeAsOf,
			seed: func(context.Context, *testing.T, *sql.Conn) {},
			expect: []historyExpectation{
				{"stage", 0, "", "0"},
				{"backlog", 0, "", "0"},
				{"blockage", 0, "", "0"},
				{"failure", 0, "", "0"},
				{"queue", 1, "total_count", "0"},
				{"queue", 1, "outstanding_count", "0"},
			},
		},
		{
			name: "stale_equal_time_and_composite",
			asOf: historyEdgeAsOf,
			seed: staleShimRows,
			expect: []historyExpectation{
				{"stage", 0, "", "4"},
				{"queue", 1, "total_count", "7"},
				{"queue", 1, "outstanding_count", "3"},
				{"queue", 1, "pending_count", "2"},
				{"stage", 1, "stage", `"novel"`},
				{"stage", 1, "status", `"mystery"`},
				{"stage", 4, "status", `"running"`},
			},
		},
		{
			name: "null_active_pointer",
			asOf: historyEdgeAsOf,
			seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
				staleShimRows(ctx, t, conn)
				historyExec(ctx, t, conn, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = 'sa'`)
			},
			expect: []historyExpectation{{"queue", 1, "outstanding_count", "5"}},
		},
		{
			name: "dangling_active_pointer",
			asOf: historyEdgeAsOf,
			seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
				staleShimRows(ctx, t, conn)
				historyExec(ctx, t, conn, `UPDATE ingestion_scopes SET active_generation_id = 'not-a-generation' WHERE scope_id = 'sa'`)
			},
			expect: []historyExpectation{{"queue", 1, "outstanding_count", "5"}},
		},
		historyFenceCase(),
		queueFailureProvenanceCase(),
		backlogOrderLeaseOnlyCase(),
		blockageLeaseReadinessCase(),
	}
}

// historyBusyWorkSQL is the busy shape the C3 shim measured A2 on: about 20%
// of 3,000 work rows are live or failed. Scopes cover a null active pointer
// (every seventh), a dangling pointer (every eleventh), and an
// equal-ingested-time tie among generations 2-4 (every third); one row in 97
// names another scope's generation. Live rows share conflict keys with
// earlier claimed rows, so blockages appear; payload entity keys meet one
// committed readiness phase per scope.
const historyBusyWorkSQL = `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'bs-' || i, 'repository', 'git', 'bs-' || i, 'git', 'bs-' || i,
       TIMESTAMPTZ '2026-10-04 00:00:00+00', TIMESTAMPTZ '2026-10-04 00:00:00+00', 'active',
       CASE WHEN i % 7 = 0 THEN NULL
            WHEN i % 11 = 0 THEN 'bg-missing-' || i
            ELSE 'bg-' || i || '-3' END,
       '{}'::jsonb
FROM generate_series(1, 40) AS i;
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT 'bg-' || i || '-' || g, 'bs-' || i, 'snapshot', ts, ts,
       CASE WHEN g = 3 AND i % 7 <> 0 AND i % 11 <> 0 THEN 'active' ELSE 'superseded' END, '{}'::jsonb
FROM generate_series(1, 40) AS i
CROSS JOIN generate_series(0, 4) AS g
CROSS JOIN LATERAL (
  SELECT TIMESTAMPTZ '2026-10-04 00:00:00+00'
         + make_interval(hours => CASE WHEN g >= 2 AND i % 3 = 0 THEN 2 ELSE g END) AS ts
) AS stamp;
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
  conflict_domain, conflict_key, claim_until, visible_at, failure_message, payload, created_at, updated_at)
SELECT 'bw-' || n, 'bs-' || scope_n,
       CASE WHEN n % 97 = 0 THEN 'bg-' || (1 + (n + 1) % 40) || '-' || n % 5
            ELSE 'bg-' || scope_n || '-' || n % 5 END,
       CASE WHEN n % 4 = 0 THEN 'projector' ELSE 'reducer' END,
       (ARRAY['package_source_correlation', 'gcp_relationship_materialization', 'alpha', 'beta',
              'container_image_identity'])[1 + (n / 5) % 5],
       status, 'busy',
       CASE WHEN status IN ('claimed', 'running') THEN 'bk-' || n ELSE 'bk-' || (n - 10) END,
       CASE WHEN status IN ('claimed', 'running') THEN
              TIMESTAMPTZ '2026-10-05 02:01:41+00'
              + CASE WHEN n % 20 < 10 THEN INTERVAL '-1 minute' ELSE INTERVAL '10 minutes' END
       END,
       CASE WHEN n % 30 = 0 THEN TIMESTAMPTZ '2026-10-05 03:01:41+00' END,
       CASE WHEN status IN ('retrying', 'failed', 'dead_letter') AND n % 3 <> 0 THEN 'busy-fail-' || n END,
       jsonb_build_object('entity_key', 'ek-' || n % 7),
       TIMESTAMPTZ '2026-10-05 02:01:41+00' - make_interval(mins => n % 300),
       TIMESTAMPTZ '2026-10-05 02:01:41+00' - make_interval(mins => n % 17)
FROM generate_series(1, 3000) AS n
CROSS JOIN LATERAL (
  SELECT 1 + n % 40 AS scope_n,
         CASE WHEN n % 10 IN (0, 1)
              THEN (ARRAY['pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter'])[1 + (n / 10) % 6]
              WHEN n % 50 = 3 THEN 'quarantined'
              WHEN n % 10 = 2 THEN 'superseded'
              ELSE 'succeeded' END AS status
) AS shape;
INSERT INTO graph_projection_phase_state (scope_id, acceptance_unit_id, source_run_id, generation_id,
  keyspace, phase, committed_at, updated_at)
SELECT 'bs-' || i, 'ek-1', 'bg-' || i || '-3', 'bg-' || i || '-3', 'cloud_resource_uid',
       'canonical_nodes_committed', TIMESTAMPTZ '2026-10-05 01:00:00+00', TIMESTAMPTZ '2026-10-05 01:00:00+00'
FROM generate_series(1, 40) AS i;
ANALYZE`

// historyFixtureCases are the equality-only cases: the busy shape and the
// #6794 status semantics fixture with its differential extras.
func historyFixtureCases() []historyCase {
	return append(historyLiveShareCases(), []historyCase{
		{
			name: "busy_20pct_live",
			asOf: historyEdgeAsOf,
			seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
				historyExec(ctx, t, conn, historyBusyWorkSQL)
			},
			expect:   []historyExpectation{{"queue", 1, "total_count", "3000"}},
			wantMode: activeWorkModeGrouped,
			analyzed: true,
		},
		{
			name: "status_semantics_fixture",
			asOf: statusSemanticsAsOf,
			seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
				seedStatusSemanticsFixture(ctx, t, conn)
				seedActiveWorkDifferentialExtras(ctx, t, conn)
			},
		},
	}...)
}
