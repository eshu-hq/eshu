// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

// TestIdentityPageQueryServesOnlyActiveGenerationsLive pins the row set of the
// identity page query against real Postgres after the #7805 rewrite from a JOIN
// to a hashed SubPlan filter. Only a non-tombstone identity fact whose
// generation is its scope's active generation AND whose generation row is
// status 'active' may be served.
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityPageQueryServesOnlyActiveGenerationsLive(t *testing.T) {
	if os.Getenv("ESHU_IDENTITY_EPOCH_LIVE") != "1" {
		t.Skip("set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	ctx := context.Background()
	stamp := time.Now().Format("20060102-150405.000")
	now := time.Now().UTC()
	scopeID := "live-page-scope-" + stamp
	activeGen := "live-page-active-" + stamp
	oldGen := "live-page-old-" + stamp
	staleScope := "live-page-stale-scope-" + stamp
	staleGen := "live-page-stale-gen-" + stamp

	seedIngestionScopeLive(t, sqlDB, scopeID, activeGen)
	// A scope whose active_generation_id points at a generation row that is no
	// longer status 'active' must serve nothing.
	seedIngestionScopeLive(t, sqlDB, staleScope, staleGen)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sqlDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
	      VALUES ($1, $2, 'manual', $3, $3, 'superseded', $3)`, scopeID, oldGen, now.Add(-time.Hour))
	exec(`UPDATE scope_generations SET status = 'superseded', superseded_at = $2 WHERE generation_id = $1`, staleGen, now)
	t.Cleanup(func() {
		cleanup := context.Background()
		for _, id := range []string{scopeID, staleScope} {
			_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM fact_records WHERE scope_id = $1`, id)
			_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM scope_generations WHERE scope_id = $1`, id)
			_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, id)
		}
	})

	insertFact := func(factID, scope, gen, kind string, tombstone bool) {
		t.Helper()
		exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
		        collector_kind, fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		      VALUES ($1, $2, $3, $4, $1, '1.0.0', 'oci_registry', 0, 'reported', 'oci_registry', $1, $5, $5, $6, '{}'::jsonb)`,
			factID, scope, gen, kind, now, tombstone)
	}
	wantServed := "live-page-served-" + stamp
	insertFact(wantServed, scopeID, activeGen, "oci_registry.image_tag_observation", false)
	insertFact("live-page-superseded-"+stamp, scopeID, oldGen, "oci_registry.image_tag_observation", false)
	insertFact("live-page-tombstone-"+stamp, scopeID, activeGen, "oci_registry.image_tag_observation", true)
	insertFact("live-page-other-kind-"+stamp, scopeID, activeGen, "repository", false)
	insertFact("live-page-stale-"+stamp, staleScope, staleGen, "oci_registry.image_tag_observation", false)

	store := NewFactStore(SQLDB{DB: sqlDB})
	loaded, err := store.loadIdentityFactsUncached(ctx)
	if err != nil {
		t.Fatalf("loadIdentityFactsUncached: %v", err)
	}
	var mine []string
	for _, env := range loaded {
		if env.ScopeID == scopeID || env.ScopeID == staleScope {
			mine = append(mine, env.FactID)
		}
	}
	sort.Strings(mine)
	if want := fmt.Sprint([]string{wantServed}); fmt.Sprint(mine) != want {
		t.Fatalf("served fact ids for the test scopes = %v, want exactly %v", mine, want)
	}
}
