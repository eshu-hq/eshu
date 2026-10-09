// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestIdentityEpochProbeOnFIPSPostgresLive checks the production epoch probe
// against a PostgreSQL server where MD5 is unavailable. It makes no writes.
// Set ESHU_IDENTITY_EPOCH_FIPS_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityEpochProbeOnFIPSPostgresLive(t *testing.T) {
	if os.Getenv("ESHU_IDENTITY_EPOCH_FIPS_LIVE") != "1" {
		t.Skip("set ESHU_IDENTITY_EPOCH_FIPS_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("ESHU_POSTGRES_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var md5Result string
	if err := sqlDB.QueryRowContext(ctx, "SELECT md5('foo')").Scan(&md5Result); err == nil {
		t.Skip("PostgreSQL permits MD5; run against a FIPS-configured server")
	} else if !strings.Contains(err.Error(), "could not compute MD5 hash: unsupported") {
		t.Fatalf("MD5 precondition returned an unexpected error: %v", err)
	}

	epoch, err := NewFactStore(SQLDB{DB: sqlDB}).probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probe identity epoch on FIPS PostgreSQL: %v", err)
	}
	if len(epoch.activeFingerprint) != 64 {
		t.Fatalf("active fingerprint length = %d, want SHA-256 hex length 64", len(epoch.activeFingerprint))
	}
}

// TestIdentityEpochProbeIsValidSQLLive proves the probe query is valid SQL
// against a real Postgres connection. It seeds a minimal identity fact and
// verifies the probe returns rows without error and the new fact is counted.
// The uncached load is NOT exercised against the full shim (which is large
// and slow); probe validity is the regression we are guarding against.
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityEpochProbeIsValidSQLLive(t *testing.T) {
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

	scopeID := "live-epoch-probe-test-" + time.Now().Format("20060102-150405.000")
	genID := "live-epoch-gen-" + time.Now().Format("20060102-150405.000")
	seedIdentityEpochLive(t, sqlDB, scopeID, genID)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = sqlDB.ExecContext(ctx, `DELETE FROM fact_records WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(ctx, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	db := SQLDB{DB: sqlDB}
	factStore := NewFactStore(db)

	// Probe must return rows without error.
	epoch, err := factStore.probeIdentityEpoch(context.Background())
	if err != nil {
		t.Fatalf("probeIdentityEpoch error = %v, want nil", err)
	}
	if epoch.count < 1 {
		t.Fatalf("probe count = %d, want >= 1", epoch.count)
	}
	if epoch.activeFingerprint == "" {
		t.Fatalf("probe active_fingerprint = %q, want non-empty (at least one scope seeded)", epoch.activeFingerprint)
	}
	t.Logf("probe passed: count=%d max_obs=%v fingerprint=%s", epoch.count, epoch.maxObservedAt, epoch.activeFingerprint)
}

// TestIdentityEpochProbeDetectsSupersessionLive proves the collision-resistant
// SHA-256 fingerprint (issue #5438 P1-B fix) detects a real active-generation
// supersession against live Postgres. It seeds one ingestion_scope, probes
// the epoch, then flips ONLY that scope's active_generation_id to a new
// generation — a supersession that touches no fact_records rows, so fact
// count and max(observed_at) are provably unchanged by the flip alone — and
// probes again. The fingerprint (and therefore the whole epoch) MUST change,
// or the cache would serve stale identity facts after a supersession with
// this scope shape. This is the regression guard for the bug the old
// sum(hashtext(...)) fingerprint could silently miss (a 32-bit collision or
// offsetting deltas that cancel in the sum).
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityEpochProbeDetectsSupersessionLive(t *testing.T) {
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
	now := time.Now().UTC()
	scopeID := "live-epoch-supersession-test-" + time.Now().Format("20060102-150405.000")
	genA := "live-epoch-supersession-gen-a-" + time.Now().Format("20060102-150405.000")
	genB := "live-epoch-supersession-gen-b-" + time.Now().Format("20060102-150405.000")

	seedIngestionScopeLive(t, sqlDB, scopeID, genA)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = sqlDB.ExecContext(cleanupCtx, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(cleanupCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	db := SQLDB{DB: sqlDB}
	factStore := NewFactStore(db)

	epoch1, err := factStore.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch (before supersession): %v", err)
	}

	// Supersede: genA is marked superseded (required by the
	// scope_generations_active_scope_idx UNIQUE partial index, which allows
	// only one 'active' row per scope_id) and genB becomes the new active
	// generation for the SAME scope. No fact_records rows are touched by
	// this step, so count and max(observed_at) are provably unchanged.
	_, err = sqlDB.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'superseded', superseded_at = $2 WHERE scope_id = $1 AND generation_id = $3`,
		scopeID, now, genA)
	if err != nil {
		t.Fatalf("supersede generation A: %v", err)
	}
	_, err = sqlDB.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
		 VALUES ($1, $2, 'manual', $3, $3, 'active')`,
		scopeID, genB, now)
	if err != nil {
		t.Fatalf("seed superseding generation B: %v", err)
	}
	_, err = sqlDB.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID, genB)
	if err != nil {
		t.Fatalf("flip active_generation_id to generation B: %v", err)
	}

	epoch2, err := factStore.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch (after supersession): %v", err)
	}

	if epoch1.count != epoch2.count {
		t.Fatalf("count changed across supersession (test precondition violated): before=%d after=%d", epoch1.count, epoch2.count)
	}
	if !epoch1.maxObservedAt.Equal(epoch2.maxObservedAt) {
		t.Fatalf("max_observed_at changed across supersession (test precondition violated): before=%v after=%v", epoch1.maxObservedAt, epoch2.maxObservedAt)
	}
	if epoch1.activeFingerprint == epoch2.activeFingerprint {
		t.Fatalf("fingerprint did NOT change across active_generation_id supersession: before=%q after=%q — the cache would serve stale identity facts", epoch1.activeFingerprint, epoch2.activeFingerprint)
	}
	if epoch1 == epoch2 {
		t.Fatalf("epoch did NOT change across supersession — the cache would false-hit and serve stale identity facts")
	}
	t.Logf("supersession detected: count=%d max_obs=%v fingerprint before=%q after=%q", epoch1.count, epoch1.maxObservedAt, epoch1.activeFingerprint, epoch2.activeFingerprint)
}

// seedIngestionScopeLive seeds an ingestion_scopes row and its active
// scope_generations row. Split from the fact-seeding step (seedIdentityFactLive)
// so tests that only need a scope/generation shape — for example the
// supersession regression test, which flips active_generation_id without
// touching fact_records — do not have to seed an unused fact.
func seedIngestionScopeLive(t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	_, err := db.ExecContext(ctx,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
		 VALUES ($1, 'repository', 'oci_registry', $1, 'oci_registry', $1, $2, $2, 'active', $3)`,
		scopeID, now, generationID)
	if err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
		 VALUES ($1, $2, 'manual', $3, $3, 'active')`,
		scopeID, generationID, now)
	if err != nil {
		t.Fatalf("seed scope_generations: %v", err)
	}
}

// seedIdentityFactLive seeds one identity fact_records row for scopeID/generationID.
func seedIdentityFactLive(t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	factID := "fact-" + generationID + "-1"
	_, err := db.ExecContext(
		ctx,
		`INSERT INTO fact_records (
		    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		    schema_version, collector_kind, fencing_token, source_confidence,
		    source_system, source_fact_key, source_uri, source_record_id,
		    observed_at, ingested_at, is_tombstone, payload
		 ) VALUES (
		    $1, $2, $3, 'oci_registry.image_tag_observation', $4,
		    '1.0.0', 'oci_registry', 0, 'reported',
		    'oci_registry', $4, 'oci://example/repo:latest', 'repo:latest',
		    $5, $5, FALSE, '{"registry":"example.com","repository":"repo","tag":"latest"}'::jsonb
		 )`,
		factID, scopeID, generationID, "stable-"+factID, now,
	)
	if err != nil {
		t.Fatalf("seed fact_records: %v", err)
	}
}

// seedIdentityEpochLive seeds a scope, its active generation, and one
// identity fact for that generation.
func seedIdentityEpochLive(t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	seedIngestionScopeLive(t, db, scopeID, generationID)
	seedIdentityFactLive(t, db, scopeID, generationID)
}

// TestIdentityEpochIgnoresSupersededGenerationRowsLive is the #7805 epoch
// regression against real Postgres. Deleting an identity fact of a superseded
// generation (what retention does every few minutes) must leave the epoch
// unchanged, while inserting or deleting an identity fact of the active
// generation must change it.
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityEpochIgnoresSupersededGenerationRowsLive(t *testing.T) {
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
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM fact_records WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = sqlDB.ExecContext(cleanup, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

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

// TestIdentityPageQueryPlanRidesOrderedIndexLive pins the plan shape of the
// identity page query on a real server (#7805). The query must read
// fact_records through the ordered partial index fact_records_identity_epoch_idx_v2
// under the LIMIT, with the active-generation restriction as a hashed SubPlan
// filter, and must not sort the active set or sequentially scan fact_records.
// The rewrite from a JOIN to that filter depends on planner behavior ("OR FALSE"
// blocks the sublink pull-up), so a server upgrade or a change to the query that
// lets the planner pick the scope-driven plan again fails here instead of
// turning a 5 s load back into an 8 minute one.
//
// The fixture is a private schema that copies the three tables, the identity
// index, and the keyset index (the index the slow plan used), then drops it.
// Verified on PostgreSQL 18; the plan, not the SQL text, is what this pins.
//
// Set ESHU_IDENTITY_EPOCH_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestIdentityPageQueryPlanRidesOrderedIndexLive(t *testing.T) {
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
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("pin connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var base string
	if err := conn.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&base); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	scratch := fmt.Sprintf("plan7805_%d", time.Now().UnixNano())
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %.80q: %v", query, err)
		}
	}
	exec(`CREATE SCHEMA ` + scratch)
	t.Cleanup(func() { _, _ = sqlDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+scratch+` CASCADE`) })
	for _, table := range []string{"ingestion_scopes", "scope_generations", "fact_records"} {
		exec(fmt.Sprintf(`CREATE TABLE %s.%s (LIKE %s.%s INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`, scratch, table, base, table))
	}
	// Recreate the two indexes the planner chooses between, under their
	// production names, from the live definitions.
	for _, index := range []string{"fact_records_identity_epoch_idx_v2", "fact_records_scope_generation_keyset_idx"} {
		var def string
		err := conn.QueryRowContext(ctx,
			`SELECT indexdef FROM pg_indexes WHERE schemaname = $1 AND indexname = $2`, base, index).Scan(&def)
		if err != nil {
			t.Fatalf("read definition of %s: %v", index, err)
		}
		exec(strings.Replace(def, " ON "+base+".fact_records ", " ON "+scratch+".fact_records ", 1))
	}
	exec(`SET search_path TO ` + scratch + `, ` + base)

	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
	      SELECT 'scope-'||n, 'src', 'oci_registry', 'key-'||n, 'col', 'p'||n, now(), now(), 'active', 'gen-'||n||'-a' FROM generate_series(1,150) n`)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
	      SELECT 'gen-'||n||'-'||g, 'scope-'||n, 'sched', now(), now(), CASE g WHEN 'a' THEN 'active' ELSE 'superseded' END
	      FROM generate_series(1,150) n, (VALUES ('a'),('s')) gs(g)`)
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
	      SELECT 'f-'||n||'-'||g||'-'||i, 'scope-'||n, 'gen-'||n||'-'||g, 'oci_registry.image_tag_observation',
	             'k'||n||'-'||g||'-'||i, 'oci_registry', 'sk'||i, now() - (random() * interval '30 days'), now(), '{}'::jsonb
	      FROM generate_series(1,150) n, (VALUES ('a',500),('s',400)) gs(g,cnt), LATERAL generate_series(1,gs.cnt) i`)
	exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
	      SELECT 'b-'||n||'-'||i, 'scope-'||n, 'gen-'||n||'-a', 'file', 'b'||n||'-'||i, 'git', 'bk'||i, now(), now(), '{}'::jsonb
	      FROM generate_series(1,150) n, generate_series(1,800) i`)
	exec(`ANALYZE ingestion_scopes`)
	exec(`ANALYZE scope_generations`)
	exec(`ANALYZE fact_records`)

	var raw []byte
	if err := conn.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) `+listActiveContainerImageIdentityFactsQuery,
		nil, "", listFactsByKindPageSize).Scan(&raw); err != nil {
		t.Fatalf("explain page query: %v", err)
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v (%d plans)\n%s", err, len(plans), raw)
	}
	var nodeTypes []string
	var orderedIndexScan, hashedSubPlan, seqScanOnFacts bool
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		nodeTypes = append(nodeTypes, nodeType)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		filter, _ := node["Filter"].(string)
		if nodeType == "Index Scan" && index == "fact_records_identity_epoch_idx_v2" {
			orderedIndexScan = true
		}
		if nodeType == "Seq Scan" && relation == "fact_records" {
			seqScanOnFacts = true
		}
		if strings.Contains(filter, "hashed SubPlan") {
			hashedSubPlan = true
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				if m, ok := child.(map[string]any); ok {
					walk(m)
				}
			}
		}
	}
	walk(plans[0].Plan)

	if !orderedIndexScan {
		t.Errorf("plan has no Index Scan on fact_records_identity_epoch_idx_v2; node types %v\n%s", nodeTypes, raw)
	}
	if !hashedSubPlan {
		t.Errorf("plan does not filter the active generations with a hashed SubPlan; node types %v\n%s", nodeTypes, raw)
	}
	if seqScanOnFacts {
		t.Errorf("plan sequentially scans fact_records; node types %v\n%s", nodeTypes, raw)
	}
	for _, nodeType := range nodeTypes {
		if nodeType == "Sort" {
			t.Errorf("plan sorts the active set before the LIMIT (the quadratic shape); node types %v\n%s", nodeTypes, raw)
			break
		}
	}
}
