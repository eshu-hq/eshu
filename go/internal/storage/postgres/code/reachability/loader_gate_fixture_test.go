// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// legacyListPendingCodeReachabilityInputsSQL is the candidate statement the
// loader shipped before #7547 (one join of every completed intent row, the
// watermark probed per intent row, then GROUP BY). It is kept here, test-only,
// as the row-set oracle for the #7547 restructure: on runs the completeness
// gate admits, the new statement must return exactly these rows.
const legacyListPendingCodeReachabilityInputsSQL = `
WITH candidate AS (
    SELECT acceptance.scope_id,
           acceptance.acceptance_unit_id AS repository_id,
           acceptance.source_run_id,
           acceptance.generation_id,
           max(intent.completed_at) AS completed_at,
           max(watermark.updated_at) AS reach_updated_at,
           max(watermark.verdict_schema_epoch) AS reach_verdict_epoch
    FROM shared_projection_acceptance AS acceptance
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = acceptance.scope_id
     AND scope.active_generation_id = acceptance.generation_id
    JOIN scope_generations AS generation
      ON generation.generation_id = acceptance.generation_id
     AND generation.status = 'active'
    JOIN shared_projection_intents AS intent
      ON intent.scope_id = acceptance.scope_id
     AND intent.acceptance_unit_id = acceptance.acceptance_unit_id
     AND intent.source_run_id = acceptance.source_run_id
     AND intent.generation_id = acceptance.generation_id
     AND intent.projection_domain IN ('code_calls', 'inheritance_edges')
     AND intent.completed_at IS NOT NULL
    LEFT JOIN code_reachability_repository_watermarks AS watermark
      ON watermark.scope_id = acceptance.scope_id
     AND watermark.generation_id = acceptance.generation_id
     AND watermark.repository_id = acceptance.acceptance_unit_id
    GROUP BY acceptance.scope_id, acceptance.acceptance_unit_id,
             acceptance.source_run_id, acceptance.generation_id
)
SELECT scope_id, repository_id, source_run_id, generation_id, completed_at
FROM candidate
WHERE reach_updated_at IS NULL
   OR completed_at > reach_updated_at
   OR coalesce(reach_verdict_epoch, 0) < $2
ORDER BY completed_at ASC, repository_id ASC
LIMIT $1
`

// loaderGateRun describes one seeded repository run for the #7547 candidate
// statement proofs. The zero value (plus a name) is a complete full run: a
// non-delta active generation, both reducer materialization work items
// succeeded, one completed code_calls intent, no pending intent, and no
// watermark.
type loaderGateRun struct {
	name        string
	isDelta     bool
	workItems   map[string]string // domain -> status; nil means both succeeded
	extraItem   [2]string         // optional second work item {domain, status}
	pending     string            // projection domain of a pending intent; "" = none
	completedAt time.Time
	watermark   *loaderGateWatermark
	// oldGeneration seeds an older, superseded generation with its own run,
	// acceptance row, and a NEWER completed intent; it must never leak.
	oldGeneration bool
	// strayRunIntent seeds a completed intent of another run in the active
	// generation with a newer completed_at; it must not move completed_at.
	strayRunIntent bool
}

type loaderGateWatermark struct {
	updatedAt time.Time
	epoch     int
}

// loaderGateRow is one candidate row as the statement returns it.
type loaderGateRow struct {
	ScopeID, RepositoryID, SourceRunID, GenerationID string
	CompletedAt                                      time.Time
}

// seedLoaderGateRun seeds one run under its own scope keyed by suffix+name
// and registers cleanup. It returns the scope id the run lives under.
func seedLoaderGateRun(t *testing.T, ctx context.Context, db *sql.DB, suffix string, run loaderGateRun) string {
	t.Helper()
	key := run.name + "-" + suffix
	scopeID, repoID := "scope-"+key, "repo-"+key
	generationID, runID := "gen-"+key, "run-"+key
	registerRouteLivenessCleanup(t, db, scopeID, repoID)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %s: %q: %v", run.name, q, err)
		}
	}
	at := run.completedAt
	exec(`INSERT INTO ingestion_scopes
	  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
	   observed_at, ingested_at, status, active_generation_id, payload)
	  VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3,'{}'::jsonb)`, scopeID, at, generationID)
	generation := func(genID, status string, isDelta bool) {
		exec(`INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, is_delta)
		  VALUES ($1,$2,'manual',$3,$3,$4,$3,$5)`, genID, scopeID, at, status, isDelta)
	}
	acceptance := func(genID, sourceRunID string) {
		exec(`INSERT INTO shared_projection_acceptance
		  (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
		  VALUES ($1,$2,$3,$4,$5,$5)`, scopeID, repoID, sourceRunID, genID, at)
	}
	intent := func(id, domain, genID, sourceRunID string, completedAt *time.Time) {
		exec(`INSERT INTO shared_projection_intents
		  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
		   source_run_id, generation_id, payload, created_at, completed_at)
		  VALUES ($1,$2,$3,$4,$3,$3,$5,$6,'{}'::jsonb,$7,$8)`,
			"intent-"+id+"-"+key, domain, repoID, scopeID, sourceRunID, genID, at, completedAt)
	}
	workItem := func(id, genID, domain, status string) {
		exec(`INSERT INTO fact_work_items
		  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
		   payload, created_at, updated_at)
		  VALUES ($1,$2,$3,'reducer',$4,$5,1,'{}'::jsonb,$6,$6)`,
			"wi-"+id+"-"+key, scopeID, genID, domain, status, at)
	}

	if run.oldGeneration {
		oldGen, oldRun := "old-"+generationID, "old-"+runID
		generation(oldGen, "superseded", false)
		acceptance(oldGen, oldRun)
		later := at.Add(time.Hour)
		intent("old", "code_calls", oldGen, oldRun, &later)
		workItem("old-calls", oldGen, "code_call_materialization", "succeeded")
		workItem("old-inh", oldGen, "inheritance_materialization", "succeeded")
	}
	generation(generationID, "active", run.isDelta)
	acceptance(generationID, runID)
	intent("done", "code_calls", generationID, runID, &at)
	if run.strayRunIntent {
		later := at.Add(time.Hour)
		intent("stray", "inheritance_edges", generationID, "stray-"+runID, &later)
	}
	if run.pending != "" {
		intent("pending", run.pending, generationID, runID, nil)
	}
	items := run.workItems
	if items == nil {
		items = map[string]string{
			"code_call_materialization":   "succeeded",
			"inheritance_materialization": "succeeded",
		}
	}
	for domain, status := range items {
		workItem(domain, generationID, domain, status)
	}
	if run.extraItem[0] != "" {
		workItem("extra", generationID, run.extraItem[0], run.extraItem[1])
	}
	if run.watermark != nil {
		exec(`INSERT INTO code_reachability_repository_watermarks
		  (scope_id, generation_id, repository_id, truncated, updated_at, verdict_schema_epoch)
		  VALUES ($1,$2,$3,false,$4,$5)`, scopeID, generationID, repoID, run.watermark.updatedAt, run.watermark.epoch)
	}
	return scopeID
}

// queryLoaderGateRows runs a candidate statement and keeps only the rows
// whose scope belongs to the given set, so leftovers on a shared
// ESHU_POSTGRES_DSN never change the assertion. Order is preserved.
func queryLoaderGateRows(t *testing.T, ctx context.Context, db *sql.DB, statement string, limit, epoch int, scopes map[string]bool) []loaderGateRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, statement, limit, epoch)
	if err != nil {
		t.Fatalf("query candidates: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []loaderGateRow
	for rows.Next() {
		var row loaderGateRow
		if err := rows.Scan(&row.ScopeID, &row.RepositoryID, &row.SourceRunID, &row.GenerationID, &row.CompletedAt); err != nil {
			t.Fatalf("scan candidate: %v", err)
		}
		if scopes[row.ScopeID] {
			out = append(out, row)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate candidates: %v", err)
	}
	return out
}
