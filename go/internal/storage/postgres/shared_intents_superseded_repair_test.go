// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// openSupersededProofSchema opens a private schema on the disposable Postgres
// named by ESHU_SUPERSEDED_GENERATION_PROOF_DSN and applies the production
// bootstrap to it, so a live proof runs on a bare database instead of relying
// on a pre-migrated public schema. It skips when the DSN is unset.
func openSupersededProofSchema(t *testing.T, ctx context.Context, prefix string) *sql.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_SUPERSEDED_GENERATION_PROOF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_SUPERSEDED_GENERATION_PROOF_DSN to run the superseded-generation live proof")
	}
	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	admin := openActiveOCIWarningIndexProofDB(t, dsn)
	// The content_store bootstrap needs pg_trgm's gin_trgm_ops. The extension is
	// database-wide, so install it in public (the private schema keeps public on
	// its search_path) instead of into the throwaway schema that is dropped.
	if _, err := admin.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
		t.Fatalf("ensure public pg_trgm extension: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE")
	})
	conn := openActiveOCIWarningIndexProofDB(t, activeOCIWarningIndexSchemaDSN(t, dsn, schema))
	if err := ApplyBootstrap(ctx, SQLDB{DB: conn}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	return conn
}

// TestSupersededGenerationIDsSQLShapeExcludesLiveRepairRows locks the phase
// repair queue guard (#7121): a durable repair row is a producer the lookup must
// treat as in flight, correlated on the queue's (scope_id, generation_id) prefix.
func TestSupersededGenerationIDsSQLShapeExcludesLiveRepairRows(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM graph_projection_phase_repair_queue",
		"r.scope_id = g.scope_id",
		"r.generation_id = g.generation_id",
	} {
		if !strings.Contains(supersededGenerationIDsSQL, want) {
			t.Fatalf("supersededGenerationIDsSQL missing %q:\n%s", want, supersededGenerationIDsSQL)
		}
	}
}

// TestSupersededGenerationIDsDefersToPhaseRepairRowsAgainstPostgres proves on a
// real schema that a graph_projection_phase_repair_queue row keeps a superseded
// generation out of the drain set until the repair runner deletes the row. The
// runner later publishes the phase row for that generation (workload
// materialization skips the accepted-generation check), so draining while the
// row exists would lose the edge of a delta successor.
func TestSupersededGenerationIDsDefersToPhaseRepairRowsAgainstPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	conn := openSupersededProofSchema(t, ctx, "eshu_7121_repair")
	store := NewSharedIntentStore(SQLDB{DB: conn})

	now := time.Now().UTC()
	seedGeneration := func(scopeID, genID, status string, at time.Time) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', FALSE, $3, $3, $4)`, genID, scopeID, at, status); err != nil {
			t.Fatalf("seed generation %s: %v", genID, err)
		}
	}
	seedScope := func(scopeID string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active')`, scopeID, now); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
	}
	seedRepair := func(scopeID, genID, keyspace, phase string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, `
INSERT INTO graph_projection_phase_repair_queue
    (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase,
     committed_at, enqueued_at, next_attempt_at)
VALUES ($1, 'unit', $2, $2, $3, $4, $5, $5, $6)`, scopeID, genID, keyspace, phase, now, now.Add(5*time.Minute)); err != nil {
			t.Fatalf("seed repair row: %v", err)
		}
	}

	// A: superseded, live repair row (future next_attempt_at, i.e. mid-backoff).
	// B: superseded, no repair row.
	// C: superseded, repair row belongs to the successor generation only.
	for _, id := range []string{"a", "b", "c"} {
		scope := "repository:7121-repair-" + id
		seedScope(scope)
		seedGeneration(scope, "generation:7121-repair-"+id+"-old", "superseded", now)
		seedGeneration(scope, "generation:7121-repair-"+id+"-new", "active", now.Add(time.Second))
	}
	seedRepair("repository:7121-repair-a", "generation:7121-repair-a-old", "service_uid", "workload_materialization")
	seedRepair("repository:7121-repair-c", "generation:7121-repair-c-new", "canonical_nodes", "canonical_nodes_committed")

	ids := []string{
		"generation:7121-repair-a-old",
		"generation:7121-repair-b-old",
		"generation:7121-repair-c-old",
	}
	got, err := store.SupersededGenerationIDs(ctx, ids)
	if err != nil {
		t.Fatalf("SupersededGenerationIDs() error = %v", err)
	}
	if _, drained := got["generation:7121-repair-a-old"]; drained {
		t.Errorf("generation with a live repair row was drained: %v", got)
	}
	for _, id := range []string{"generation:7121-repair-b-old", "generation:7121-repair-c-old"} {
		if _, drained := got[id]; !drained {
			t.Errorf("%s was deferred with no repair row of its own: %v", id, got)
		}
	}

	// The runner deletes the row once it publishes (or deletes it as stale); the
	// same generation then becomes drainable.
	if _, err := conn.ExecContext(ctx, `
DELETE FROM graph_projection_phase_repair_queue WHERE generation_id = 'generation:7121-repair-a-old'`); err != nil {
		t.Fatalf("delete repair row: %v", err)
	}
	after, err := store.SupersededGenerationIDs(ctx, []string{"generation:7121-repair-a-old"})
	if err != nil {
		t.Fatalf("SupersededGenerationIDs() after delete error = %v", err)
	}
	if _, drained := after["generation:7121-repair-a-old"]; !drained {
		t.Errorf("generation still deferred after its repair row was deleted: %v", after)
	}
}
