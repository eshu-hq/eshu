// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// Proof-specific environment pair for the #7805 identity epoch proofs, in the
// convention of the other live-postgres-readiness rows: the DSN names the
// administrative postgres database of a disposable server, and the DISPOSABLE
// flag acknowledges that the proof may create and drop a schema there.
const (
	identityEpochProofDSNEnv        = "ESHU_IDENTITY_EPOCH_PROOF_DSN"
	identityEpochProofDisposableEnv = "ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE"
)

// openIdentityEpochProofSchema returns a database handle whose search_path
// holds a private schema built by the real bootstrap migrations, so the proofs
// run against the production tables and indexes without touching, or being
// disturbed by, any other writer on the server. Local runs may set
// ESHU_POSTGRES_TEST_DSN directly; the readiness runner sets only the family
// pair, and a family DSN without its disposable acknowledgment fails closed.
func openIdentityEpochProofSchema(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv(identityEpochProofDSNEnv)); dsn != "" {
		if os.Getenv(identityEpochProofDisposableEnv) != "1" {
			t.Fatalf("%s is set without %s=1", identityEpochProofDSNEnv, identityEpochProofDisposableEnv)
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	return openGenerationRetentionMigratedSchema(t)
}

// TestIdentityEpochIgnoresSupersededGenerationRowsLive is the #7805 epoch
// regression against real Postgres. Deleting an identity fact of a superseded
// generation (what retention does every few minutes) must leave the epoch
// unchanged, while inserting or deleting an identity fact of the active
// generation must change it.
//
// Run locally with ESHU_IDENTITY_EPOCH_PROOF_DSN and
// ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE=1, or ESHU_POSTGRES_TEST_DSN.
func TestIdentityEpochIgnoresSupersededGenerationRowsLive(t *testing.T) {
	sqlDB, ctx := openIdentityEpochProofSchema(t)
	stamp := time.Now().Format("20060102-150405.000")
	scopeID := "live-epoch-active-set-" + stamp
	activeGen := "live-epoch-active-gen-" + stamp
	oldGen := "live-epoch-old-gen-" + stamp

	seedIngestionScopeLive(t, sqlDB, scopeID, activeGen)
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
		 VALUES ($1, $2, 'manual', $3, $3, 'superseded', $3)`,
		scopeID, oldGen, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("seed superseded generation: %v", err)
	}
	seedIdentityFactLive(t, sqlDB, scopeID, oldGen)
	seedIdentityFactLive(t, sqlDB, scopeID, activeGen)
	store := NewFactStore(SQLDB{DB: sqlDB})

	baseline, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("baseline probe: %v", err)
	}

	// Retention-style delete on the superseded generation: epoch must not move.
	if _, err := sqlDB.ExecContext(ctx,
		`DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2`, scopeID, oldGen); err != nil {
		t.Fatalf("delete superseded-generation fact: %v", err)
	}
	afterSupersededDelete, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probe after superseded delete: %v", err)
	}
	if afterSupersededDelete != baseline {
		t.Fatalf("deleting a superseded-generation fact moved the epoch: before=%+v after=%+v", baseline, afterSupersededDelete)
	}

	// An insert on the active generation must move the epoch.
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
		    collector_kind, fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
		 VALUES ($1, $2, $3, 'oci_registry.image_tag_observation', $1, '1.0.0', 'oci_registry', 0, 'reported',
		    'oci_registry', $1, $4, $4, FALSE, '{}'::jsonb)`,
		"fact-extra-"+stamp, scopeID, activeGen, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("insert active-generation fact: %v", err)
	}
	afterInsert, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probe after active insert: %v", err)
	}
	if afterInsert == afterSupersededDelete {
		t.Fatalf("inserting an active-generation fact did not move the epoch: %+v", afterInsert)
	}

	// A delete on the active generation must move the epoch back off the insert.
	if _, err := sqlDB.ExecContext(ctx,
		`DELETE FROM fact_records WHERE fact_id = $1`, "fact-extra-"+stamp); err != nil {
		t.Fatalf("delete active-generation fact: %v", err)
	}
	afterActiveDelete, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probe after active delete: %v", err)
	}
	if afterActiveDelete == afterInsert {
		t.Fatalf("deleting an active-generation fact did not move the epoch: %+v", afterActiveDelete)
	}
}

// TestIdentityPageQueryServesOnlyActiveGenerationsLive pins the row set of the
// identity page query against real Postgres after the #7805 rewrite from a JOIN
// to a hashed SubPlan filter. Only a non-tombstone identity fact whose
// generation is its scope's active generation AND whose generation row is
// status 'active' may be served.
//
// Run locally with ESHU_IDENTITY_EPOCH_PROOF_DSN and
// ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE=1, or ESHU_POSTGRES_TEST_DSN.
func TestIdentityPageQueryServesOnlyActiveGenerationsLive(t *testing.T) {
	sqlDB, ctx := openIdentityEpochProofSchema(t)
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
