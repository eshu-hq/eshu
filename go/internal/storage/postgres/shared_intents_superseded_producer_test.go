// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSupersededGenerationIDsSQLShapeExcludesInFlightProducers locks the
// in-flight-producer guard (#7121): the lookup must probe fact_work_items by
// its (scope_id, generation_id) prefix so a live producer defers the drain.
func TestSupersededGenerationIDsSQLShapeExcludesInFlightProducers(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"NOT EXISTS",
		"FROM fact_work_items",
		"w.scope_id = g.scope_id",
		"w.generation_id = g.generation_id",
		"'claimed', 'running'",
	} {
		if !strings.Contains(supersededGenerationIDsSQL, want) {
			t.Fatalf("supersededGenerationIDsSQL missing %q:\n%s", want, supersededGenerationIDsSQL)
		}
	}
}

// TestSupersededGenerationIDsDefersToInFlightProducersAgainstPostgres runs the
// production lookup on a real schema. A superseded generation is drained only
// while no work item of that generation can still publish the prerequisite
// phase: a reducer item that is claimed or running (even with an expired
// lease, because the claim query re-claims those) and a projector item that is
// pending, retrying, claimed, or running (the projector claim has no
// superseded-generation filter) both defer the drain. Set
// ESHU_SUPERSEDED_GENERATION_PROOF_DSN to a disposable Postgres to run it.
func TestSupersededGenerationIDsDefersToInFlightProducersAgainstPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_SUPERSEDED_GENERATION_PROOF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_SUPERSEDED_GENERATION_PROOF_DSN to run the in-flight producer proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	schema := fmt.Sprintf("eshu_7121_producer_%d", time.Now().UnixNano())
	admin := openActiveOCIWarningIndexProofDB(t, dsn)
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
	store := NewSharedIntentStore(SQLDB{DB: conn})

	// One superseded generation per case so the cases cannot interfere. A
	// shared active successor keeps the scope shape realistic.
	cases := []struct {
		name         string
		stage        string
		status       string
		expiredLease bool
		otherGen     bool // the work item belongs to the successor, not the superseded generation
		wantDrained  bool
	}{
		{name: "no work items", wantDrained: true},
		{name: "reducer claimed", stage: "reducer", status: "claimed"},
		{name: "reducer running", stage: "reducer", status: "running"},
		{name: "reducer running with expired lease", stage: "reducer", status: "running", expiredLease: true},
		{name: "reducer claimed with expired lease", stage: "reducer", status: "claimed", expiredLease: true},
		{name: "projector pending", stage: "projector", status: "pending"},
		{name: "projector retrying", stage: "projector", status: "retrying"},
		{name: "projector claimed", stage: "projector", status: "claimed"},
		{name: "projector running", stage: "projector", status: "running"},
		// Unleased reducer rows are superseded by the claim sweep, so they never
		// publish and do not defer the drain.
		{name: "reducer pending", stage: "reducer", status: "pending", wantDrained: true},
		{name: "reducer retrying", stage: "reducer", status: "retrying", wantDrained: true},
		{name: "reducer succeeded", stage: "reducer", status: "succeeded", wantDrained: true},
		{name: "reducer superseded", stage: "reducer", status: "superseded", wantDrained: true},
		{name: "reducer failed", stage: "reducer", status: "failed", wantDrained: true},
		{name: "reducer dead_letter", stage: "reducer", status: "dead_letter", wantDrained: true},
		{name: "projector succeeded", stage: "projector", status: "succeeded", wantDrained: true},
		{name: "projector superseded", stage: "projector", status: "superseded", wantDrained: true},
		{name: "projector dead_letter", stage: "projector", status: "dead_letter", wantDrained: true},
		{name: "live item on the successor generation only", stage: "reducer", status: "running", otherGen: true, wantDrained: true},
	}

	now := time.Now().UTC()
	byID := make(map[string]bool, len(cases))
	ids := make([]string, 0, len(cases))
	for i, tc := range cases {
		scopeID := fmt.Sprintf("repository:7121-producer-%d", i)
		oldGen := fmt.Sprintf("generation:7121-producer-%d-old", i)
		newGen := fmt.Sprintf("generation:7121-producer-%d-new", i)
		if _, err := conn.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active')`, scopeID, now); err != nil {
			t.Fatalf("%s: seed scope: %v", tc.name, err)
		}
		for _, g := range []struct {
			id, status string
			at         time.Time
		}{{oldGen, "superseded", now}, {newGen, "active", now.Add(time.Second)}} {
			if _, err := conn.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', FALSE, $3, $3, $4)`, g.id, scopeID, g.at, g.status); err != nil {
				t.Fatalf("%s: seed generation: %v", tc.name, err)
			}
		}
		if tc.stage != "" {
			workGen := oldGen
			if tc.otherGen {
				workGen = newGen
			}
			claimUntil := now.Add(10 * time.Minute)
			if tc.expiredLease {
				claimUntil = now.Add(-10 * time.Minute)
			}
			if _, err := conn.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, claim_until, payload, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'intent', $1, $6, 1,
    CASE WHEN $6 IN ('claimed','running') THEN $7::timestamptz ELSE NULL END,
    '{}'::jsonb, $8, $8)`,
				fmt.Sprintf("work-7121-producer-%d", i), scopeID, workGen, tc.stage,
				map[string]string{"reducer": "workload_materialization", "projector": "source_local"}[tc.stage],
				tc.status, claimUntil, now); err != nil {
				t.Fatalf("%s: seed work item: %v", tc.name, err)
			}
		}
		byID[oldGen] = tc.wantDrained
		ids = append(ids, oldGen)
	}

	got, err := store.SupersededGenerationIDs(ctx, ids)
	if err != nil {
		t.Fatalf("SupersededGenerationIDs() error = %v", err)
	}
	for i, tc := range cases {
		oldGen := fmt.Sprintf("generation:7121-producer-%d-old", i)
		_, drained := got[oldGen]
		if drained != byID[oldGen] {
			t.Errorf("%s: drained = %t, want %t", tc.name, drained, byID[oldGen])
		}
	}

	// Once the in-flight item finishes, the same generation becomes drainable.
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded', claim_until = NULL
WHERE work_item_id = 'work-7121-producer-1'`); err != nil {
		t.Fatalf("finish in-flight item: %v", err)
	}
	after, err := store.SupersededGenerationIDs(ctx, []string{"generation:7121-producer-1-old"})
	if err != nil {
		t.Fatalf("SupersededGenerationIDs() after finish error = %v", err)
	}
	if _, ok := after["generation:7121-producer-1-old"]; !ok {
		t.Errorf("generation is still deferred after its in-flight item succeeded: %v", after)
	}
}
