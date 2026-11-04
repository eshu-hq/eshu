// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8875 disposable PostgreSQL 18 proof, on the migrated schema in a private schema, that the grouped epoch-probe reformulation returns the same whole epoch triple (count, max observed_at, active fingerprint) as an independent Go oracle over shaped fixtures: empty set, multi-row groups, tombstones, superseded generations, non-identity kinds, delimiter-hostile scope:generation pairs, and identity-empty activation
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_IDENTITY_EPOCH_PROOF_DISPOSABLE ESHU_IDENTITY_EPOCH_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
	"time"
)

// oracleEpochFingerprint is the independent oracle for the probe's active
// fingerprint: SHA-256 over the "|" joined, byte-ordered distinct
// "scope:generation" pair strings. It uses Go's crypto/sha256, not the
// database, so agreement with the probe proves the reformulation digests
// exactly the intended pair set in the intended order.
func oracleEpochFingerprint(pairs []string) string {
	distinct := make(map[string]struct{}, len(pairs))
	for _, p := range pairs {
		distinct[p] = struct{}{}
	}
	ordered := make([]string, 0, len(distinct))
	for p := range distinct {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	sum := sha256.Sum256([]byte(strings.Join(ordered, "|")))
	return hex.EncodeToString(sum[:])
}

// TestIdentityEpochProbeReformulationMatchesOracleLive proves the #8875
// grouped probe reformulation byte-identical: over a shaped fixture the
// probe's whole triple (count, max observed_at, fingerprint) equals the
// hand-computed oracle, and an identity-empty activation still moves nothing
// while a later identity insert moves the epoch.
func TestIdentityEpochProbeReformulationMatchesOracleLive(t *testing.T) {
	sqlDB, ctx := openIdentityEpochProofSchema(t)
	stamp := time.Now().Format("20060102-150405.000")

	// Pair strings are designed so their order is decided by distinct leading
	// lowercase letters, which every plausible database collation agrees on,
	// so the Go byte-order oracle and the probe's ORDER BY cannot disagree.
	scopeA := "alpha-" + stamp
	scopeB := "beta-" + stamp
	scopeC := "gamma-" + stamp
	scopeD := "delta-" + stamp
	scopeZ := "zeta:quo|te-" + stamp
	// Generation ids are globally unique (partial unique index on
	// ingestion_scopes.active_generation_id), so each scope gets its own.
	genA := "gen-a1"
	genB := "gen-b1"
	genC := "gen-c1"
	genD := "gen-d1"
	genZ := "gen:x|y"

	seeds := []struct{ scope, gen string }{
		{scopeA, genA}, {scopeB, genB}, {scopeC, genC}, {scopeD, genD}, {scopeZ, genZ},
	}
	for _, seed := range seeds {
		seedIngestionScopeLive(t, sqlDB, seed.scope, seed.gen)
		t.Cleanup(func(scope string) func() {
			return func() {
				cleanupCtx := context.Background()
				_, _ = sqlDB.ExecContext(cleanupCtx, `DELETE FROM fact_records WHERE scope_id = $1`, scope)
				_, _ = sqlDB.ExecContext(cleanupCtx, `DELETE FROM scope_generations WHERE scope_id = $1`, scope)
				_, _ = sqlDB.ExecContext(cleanupCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scope)
			}
		}(seed.scope))
	}

	store := NewFactStore(SQLDB{DB: sqlDB})

	// Empty set first: count 0, zero max, SHA-256 of the empty string.
	empty, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch on empty set: %v", err)
	}
	if empty.count != 0 {
		t.Fatalf("empty-set count = %d, want 0", empty.count)
	}
	if !empty.maxObservedAt.IsZero() {
		t.Fatalf("empty-set maxObservedAt = %v, want zero time", empty.maxObservedAt)
	}
	if want := oracleEpochFingerprint(nil); empty.activeFingerprint != want {
		t.Fatalf("empty-set fingerprint = %q, want oracle %q", empty.activeFingerprint, want)
	}

	base := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	mid := base.Add(time.Hour)
	top := base.Add(2 * time.Hour)

	// Scope A: three identity facts in the active generation (multi-row group).
	seedIdentityFactAtLive(t, sqlDB, "fact-a1-"+stamp, scopeA, genA, base)
	seedIdentityFactAtLive(t, sqlDB, "fact-a2-"+stamp, scopeA, genA, mid)
	seedIdentityFactAtLive(t, sqlDB, "fact-a3-"+stamp, scopeA, genA, top)
	// A tombstoned identity fact and a non-identity-kind fact on the same
	// pair: both must stay out of the triple.
	seedTombstonedIdentityFactLive(t, sqlDB, "fact-a-tomb-"+stamp, scopeA, genA, top)
	seedNonIdentityFactLive(t, sqlDB, "fact-a-other-"+stamp, scopeA, genA, top)
	// Scope B: one identity fact, plus two facts on a superseded generation
	// that retention-style deletes target (excluded from the triple).
	seedIdentityFactAtLive(t, sqlDB, "fact-b1-"+stamp, scopeB, genB, mid)
	seedSupersededGenerationWithFactsLive(t, sqlDB, "fact-b-old", scopeB, "old-b-"+stamp, mid)
	// Scope C: superseded generation holds the only facts; the ACTIVE
	// generation is identity-empty, so scope C contributes no pair. The
	// initial generation is superseded first, mirroring a real activation.
	seedSupersededGenerationWithFactsLive(t, sqlDB, "fact-c-old", scopeC, "old-c-"+stamp, base)
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'superseded', superseded_at = $2 WHERE scope_id = $1 AND generation_id = $3`,
		scopeC, base, genC); err != nil {
		t.Fatalf("supersede initial generation: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
		 VALUES ($1, $2, 'manual', $3, $3, 'active')`,
		scopeC, "empty-"+stamp, base); err != nil {
		t.Fatalf("seed identity-empty active generation: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeC, "empty-"+stamp); err != nil {
		t.Fatalf("activate identity-empty generation: %v", err)
	}
	// Scope D: active generation with no facts at all.
	// Scope Z: delimiter-hostile scope and generation ids, two facts.
	seedIdentityFactAtLive(t, sqlDB, "fact-z1-"+stamp, scopeZ, genZ, base)
	seedIdentityFactAtLive(t, sqlDB, "fact-z2-"+stamp, scopeZ, genZ, mid)

	coveredPairs := []string{
		scopeA + ":" + genA,
		scopeB + ":" + genB,
		scopeZ + ":" + genZ,
	}
	got, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch on shaped fixture: %v", err)
	}
	if got.count != 6 {
		t.Fatalf("fixture count = %d, want 6 (3 A + 1 B + 2 Z)", got.count)
	}
	if !got.maxObservedAt.Equal(top) {
		t.Fatalf("fixture maxObservedAt = %v, want %v", got.maxObservedAt, top)
	}
	if want := oracleEpochFingerprint(coveredPairs); got.activeFingerprint != want {
		t.Fatalf("fixture fingerprint = %q, want oracle %q", got.activeFingerprint, want)
	}

	// Identity-empty activation on scope D moves nothing.
	before, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch before empty flip: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'superseded', superseded_at = $2 WHERE scope_id = $1 AND generation_id = $3`,
		scopeD, top, genD); err != nil {
		t.Fatalf("supersede generation: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
		 VALUES ($1, $2, 'manual', $3, $3, 'active')`,
		scopeD, "next-"+stamp, top); err != nil {
		t.Fatalf("seed next generation: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeD, "next-"+stamp); err != nil {
		t.Fatalf("flip to identity-empty generation: %v", err)
	}
	afterFlip, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch after empty flip: %v", err)
	}
	if afterFlip != before {
		t.Fatalf("identity-empty activation moved the epoch: before=%+v after=%+v", before, afterFlip)
	}

	// An identity fact in the newly active generation moves count and pairs.
	seedIdentityFactAtLive(t, sqlDB, "fact-d1-"+stamp, scopeD, "next-"+stamp, top)
	afterInsert, err := store.probeIdentityEpoch(ctx)
	if err != nil {
		t.Fatalf("probeIdentityEpoch after identity insert: %v", err)
	}
	if afterInsert == afterFlip {
		t.Fatalf("identity insert into the active generation did not move the epoch: %+v", afterInsert)
	}
	if afterInsert.count != 7 {
		t.Fatalf("post-insert count = %d, want 7", afterInsert.count)
	}
	wantPairs := append(append([]string{}, coveredPairs...), scopeD+":"+"next-"+stamp)
	if want := oracleEpochFingerprint(wantPairs); afterInsert.activeFingerprint != want {
		t.Fatalf("post-insert fingerprint = %q, want oracle %q", afterInsert.activeFingerprint, want)
	}
}

// seedTombstonedIdentityFactLive seeds an identity-kind fact with
// is_tombstone TRUE, which the probe must exclude.
func seedTombstonedIdentityFactLive(t *testing.T, db *sql.DB, factID, scopeID, generationID string, observedAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO fact_records (
		    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		    schema_version, collector_kind, fencing_token, source_confidence,
		    source_system, source_fact_key, source_uri, source_record_id,
		    observed_at, ingested_at, is_tombstone, payload
		 ) VALUES (
		    $1, $2, $3, 'oci_registry.image_tag_observation', $4,
		    '1.0.0', 'oci_registry', 0, 'reported',
		    'oci_registry', $4, 'oci://example/repo:latest', 'repo:latest',
		    $5, $5, TRUE, '{"registry":"example.com","repository":"repo","tag":"latest"}'::jsonb
		 )`,
		factID, scopeID, generationID, "stable-"+factID, observedAt,
	)
	if err != nil {
		t.Fatalf("seed tombstoned fact_records: %v", err)
	}
}

// seedNonIdentityFactLive seeds a fact row no identity filter arm matches
// (a git file fact without dockerfile content), which the probe must exclude.
func seedNonIdentityFactLive(t *testing.T, db *sql.DB, factID, scopeID, generationID string, observedAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO fact_records (
		    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		    schema_version, collector_kind, fencing_token, source_confidence,
		    source_system, source_fact_key, source_uri, source_record_id,
		    observed_at, ingested_at, is_tombstone, payload
		 ) VALUES (
		    $1, $2, $3, 'file', $4,
		    '1.0.0', 'git', 0, 'reported',
		    'git', $4, '', '',
		    $5, $5, FALSE, '{}'::jsonb
		 )`,
		factID, scopeID, generationID, "stable-"+factID, observedAt,
	)
	if err != nil {
		t.Fatalf("seed non-identity fact_records: %v", err)
	}
}

// seedSupersededGenerationWithFactsLive seeds a superseded generation
// holding two identity facts, modeling what retention deletes.
func seedSupersededGenerationWithFactsLive(t *testing.T, db *sql.DB, factPrefix, scopeID, generationID string, observedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
		 VALUES ($1, $2, 'manual', $3, $3, 'superseded', $3)`,
		scopeID, generationID, observedAt); err != nil {
		t.Fatalf("seed superseded generation: %v", err)
	}
	seedIdentityFactAtLive(t, db, factPrefix+"-1", scopeID, generationID, observedAt)
	seedIdentityFactAtLive(t, db, factPrefix+"-2", scopeID, generationID, observedAt)
}
