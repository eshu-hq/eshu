// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7981 disposable PostgreSQL 18 proof that the general and targeted candidate queries admit the same same-SHA duplicates past the count window in lockstep, that the witness retains each batch's newest copy per identity, and that a covered-but-never-activated generation prunes past the hard ceiling while an uncovered writer is retained and counted, enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the hard-ceiling proof does. local run passed all 3 tests in 6.81s wall with the migrated-schema setup on postgres:16 on 2026-10-14
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// The #7981 same-identity live proofs share one fixture shape: a scope whose
// active generation carries a recorded commit, so superseded duplicates of
// that commit are eligible past the count window while every other shape
// stays retained. The dupe-leg EXISTS over scope_generations subqueries is
// evaluated under each statement's own snapshot (a MATERIALIZED CTE in the
// general query, a provisional CTE in the targeted one): the re-lock's fresh
// snapshot, not row-level EvalPlanQual, is what drops a witness that changed
// between selection and locking — the same residual class as the #7903
// coverage horizon, where closing the microsecond window would serialize
// activation against retention.

// sameSHAGenerationSeed describes one superseded scope_generations row with
// an explicit same-identity triple. A nil sha writes NULL; a nil hint writes
// NULL (read back as ” by the candidate queries' COALESCE).
type sameSHAGenerationSeed struct {
	supersededAt   time.Time
	sha            *string
	isDelta        bool
	hint           *string
	activatedAt    time.Time
	writeStartedAt time.Time
}

func sameSHAPtr(s string) *string { return &s }

// seedSameSHAGeneration inserts one superseded generation with an explicit
// identity triple, activation and projection-write marker.
func seedSameSHAGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string, seed sameSHAGenerationSeed) {
	t.Helper()
	var activatedAt, writeStartedAt any
	if !seed.activatedAt.IsZero() {
		activatedAt = seed.activatedAt
	}
	if !seed.writeStartedAt.IsZero() {
		writeStartedAt = seed.writeStartedAt
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
    superseded_at, source_commit_sha, is_delta, freshness_hint, activated_at, projection_write_started_at)
VALUES ($1, $2, 'snapshot', $3, $3, 'superseded', $3, $4, $5, $6, $7, $8)`,
		generationID, scopeID, seed.supersededAt, seed.sha, seed.isDelta, seed.hint, activatedAt, writeStartedAt,
	); err != nil {
		t.Fatalf("seed same-SHA generation %s: %v", generationID, err)
	}
}

// setScopeActiveIdentity records the commit identity on a scope's active
// generation, the witness the dupe leg matches superseded copies against.
func setScopeActiveIdentity(t *testing.T, ctx context.Context, database *sql.DB, scopeID, sha string, isDelta bool, hint *string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET source_commit_sha = $1, is_delta = $2, freshness_hint = $3
WHERE generation_id = $4`, sha, isDelta, hint, scopeID+"-active"); err != nil {
		t.Fatalf("set active identity %s: %v", scopeID, err)
	}
}

// bridgeRetentionFamilyDSN maps the live-postgres-readiness runner's family
// DSN onto the shared helper's generic variable, as the older retention
// live tests do. Local runs keep using ESHU_POSTGRES_TEST_DSN directly.
func bridgeRetentionFamilyDSN(t *testing.T) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
}

// seedSameSHALockstepFixture seeds one scope with every identity shape: three
// same-SHA duplicates of the active commit (dupe-old/mid/new), one
// distinct-SHA generation, NULL- and empty-SHA generations, one uncovered
// same-SHA writer, and the activated full that sets the coverage horizon. All
// superseded rows are 10-12 days old with a 24-generation count window, so
// only the dupe leg admits anything.
func seedSameSHALockstepFixture(t *testing.T, ctx context.Context, database *sql.DB, scopeID string, now time.Time) {
	t.Helper()
	testfixtures.SeedScope(t, ctx, database, scopeID)
	setScopeActiveIdentity(t, ctx, database, scopeID, "commit-x", false, sameSHAPtr("stable"))

	dupe := func(gen string, age time.Duration) {
		seedSameSHAGeneration(t, ctx, database, scopeID, gen, sameSHAGenerationSeed{
			supersededAt: now.Add(-age), sha: sameSHAPtr("commit-x"), hint: sameSHAPtr("stable"),
		})
	}
	dupe(scopeID+"-dupe-old", 12*24*time.Hour)
	dupe(scopeID+"-dupe-mid", 11*24*time.Hour)
	dupe(scopeID+"-dupe-new", 10*24*time.Hour)
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-distinct", sameSHAGenerationSeed{
		supersededAt: now.Add(-11 * 24 * time.Hour), sha: sameSHAPtr("commit-other"), hint: sameSHAPtr("stable"),
	})
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-nullsha", sameSHAGenerationSeed{
		supersededAt: now.Add(-11 * 24 * time.Hour), hint: sameSHAPtr("stable"),
	})
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-emptysha", sameSHAGenerationSeed{
		supersededAt: now.Add(-11 * 24 * time.Hour), sha: sameSHAPtr(""), hint: sameSHAPtr("stable"),
	})
	// The coverage horizon: an activated full too recent to be eligible
	// itself. The uncovered writer wrote after it, so the probe still needs
	// it even though its SHA matches.
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-full", sameSHAGenerationSeed{
		supersededAt: now.Add(-30 * time.Minute), activatedAt: now.Add(-30 * time.Minute),
		writeStartedAt: now.Add(-2 * time.Hour),
	})
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-uncovered", sameSHAGenerationSeed{
		supersededAt: now.Add(-11 * 24 * time.Hour), sha: sameSHAPtr("commit-x"),
		hint: sameSHAPtr("stable"), writeStartedAt: now.Add(-time.Hour),
	})
}

// queryGeneralPrunable runs the shipped general candidate query directly and
// splits its rows into prunable and reported sets.
func queryGeneralPrunable(t *testing.T, ctx context.Context, database *sql.DB, now time.Time, policy GenerationRetentionPolicy) (prunable, reported []string) {
	t.Helper()
	rows, err := database.QueryContext(ctx, generationRetentionCandidateQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations,
		policy.BatchGenerationLimit, now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		t.Fatalf("general candidate query: %v", err)
	}
	candidates, err := scanRetentionCandidates(rows)
	if err != nil {
		t.Fatalf("scan general candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.uncovered {
			reported = append(reported, candidate.generationID)
		} else {
			prunable = append(prunable, candidate.generationID)
		}
	}
	slices.Sort(prunable)
	slices.Sort(reported)
	return prunable, reported
}

// queryTargetedPrunable runs the shipped targeted re-lock query over ids and
// splits its rows the same way.
func queryTargetedPrunable(t *testing.T, ctx context.Context, database *sql.DB, now time.Time, policy GenerationRetentionPolicy, scopeIDs, generationIDs []string) (prunable, reported []string) {
	t.Helper()
	rows, err := database.QueryContext(ctx, generationRetentionTargetedCandidateQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations, []string{},
		scopeIDs, generationIDs, now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		t.Fatalf("targeted candidate query: %v", err)
	}
	candidates, err := scanRetentionCandidates(rows)
	if err != nil {
		t.Fatalf("scan targeted candidates: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.uncovered {
			reported = append(reported, candidate.generationID)
		} else {
			prunable = append(prunable, candidate.generationID)
		}
	}
	slices.Sort(prunable)
	slices.Sort(reported)
	return prunable, reported
}

func sameSHAPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "7981-samesha-live",
	}
}

// TestGenerationRetentionSameSHALockstepLive pins the general and targeted
// queries in lockstep over one shared fixture: both admit exactly the three
// same-SHA duplicates past the count window, both report the uncovered
// writer instead of pruning it, and both retain the distinct, NULL and empty
// SHA shapes on rank and age alone.
func TestGenerationRetentionSameSHALockstepLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID = "samesha-lockstep"
	seedSameSHALockstepFixture(t, ctx, database, scopeID, now)
	policy := sameSHAPolicy()

	wantPrunable := []string{scopeID + "-dupe-mid", scopeID + "-dupe-new", scopeID + "-dupe-old"}
	slices.Sort(wantPrunable)
	wantReported := []string{scopeID + "-uncovered"}

	generalPrunable, generalReported := queryGeneralPrunable(t, ctx, database, now, policy)
	if !slices.Equal(generalPrunable, wantPrunable) {
		t.Errorf("general prunable = %v, want %v (only the same-SHA duplicates)", generalPrunable, wantPrunable)
	}
	if !slices.Equal(generalReported, wantReported) {
		t.Errorf("general reported = %v, want %v (the uncovered writer)", generalReported, wantReported)
	}

	all := []string{
		scopeID + "-dupe-old", scopeID + "-dupe-mid", scopeID + "-dupe-new",
		scopeID + "-distinct", scopeID + "-nullsha", scopeID + "-emptysha", scopeID + "-uncovered",
	}
	targetedPrunable, targetedReported := queryTargetedPrunable(t, ctx, database, now, policy, []string{scopeID}, all)
	if !slices.Equal(targetedPrunable, wantPrunable) {
		t.Errorf("targeted prunable = %v, want %v (lockstep with the general query)", targetedPrunable, wantPrunable)
	}
	if !slices.Equal(targetedReported, wantReported) {
		t.Errorf("targeted reported = %v, want %v (lockstep with the general query)", targetedReported, wantReported)
	}
}

// TestGenerationRetentionSameSHAPrunesOldestKeepsNewestLive is the #7981
// witness through the real store: the first pass prunes the two older
// duplicates past the count window while retaining the newest copy of the
// identity, the uncovered writer and every other shape; the second pass
// prunes the retained copy once its group is gone.
func TestGenerationRetentionSameSHAPrunesOldestKeepsNewestLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID = "samesha-witness"
	seedSameSHALockstepFixture(t, ctx, database, scopeID, now)
	policy := sameSHAPolicy()

	prune := func() GenerationRetentionResult {
		t.Helper()
		store := NewGenerationRetentionStore(SQLDB{DB: database})
		store.Now = func() time.Time { return now }
		result, err := store.PruneSupersededGenerations(ctx, policy)
		if err != nil {
			t.Fatalf("PruneSupersededGenerations() error = %v", err)
		}
		return result
	}

	first := prune()
	if first.GenerationsPruned != 2 {
		t.Fatalf("first pass GenerationsPruned = %d, want 2 (the two older duplicates)", first.GenerationsPruned)
	}
	if got := first.Skipped[sameSHANewestSkippedReason]; got != 1 {
		t.Errorf("first pass Skipped[same_sha_newest] = %d, want 1 (the newest duplicate)", got)
	}
	remaining := remainingScopeGenerations(t, ctx, database, []string{
		scopeID + "-dupe-old", scopeID + "-dupe-mid", scopeID + "-dupe-new",
		scopeID + "-distinct", scopeID + "-nullsha", scopeID + "-emptysha",
		scopeID + "-uncovered", scopeID + "-full",
	})
	for _, id := range []string{scopeID + "-dupe-old", scopeID + "-dupe-mid"} {
		if remaining[id] {
			t.Errorf("%s survived the first pass, want pruned (older same-SHA copy)", id)
		}
	}
	for _, id := range []string{
		scopeID + "-dupe-new", scopeID + "-distinct",
		scopeID + "-nullsha", scopeID + "-emptysha", scopeID + "-uncovered", scopeID + "-full",
	} {
		if !remaining[id] {
			t.Errorf("%s was pruned in the first pass, want retained", id)
		}
	}

	second := prune()
	if second.GenerationsPruned != 1 {
		t.Fatalf("second pass GenerationsPruned = %d, want 1 (the retained newest copy)", second.GenerationsPruned)
	}
	if got := second.Skipped[sameSHANewestSkippedReason]; got != 0 {
		t.Errorf("second pass Skipped[same_sha_newest] = %d, want 0 (no group left to protect)", got)
	}
	remaining = remainingScopeGenerations(t, ctx, database, []string{
		scopeID + "-dupe-new", scopeID + "-uncovered",
	})
	if remaining[scopeID+"-dupe-new"] {
		t.Errorf("%s survived the second pass, want pruned (its identity group is gone)", scopeID+"-dupe-new")
	}
	if !remaining[scopeID+"-uncovered"] {
		t.Errorf("%s was pruned, want retained (uncovered outranks the exemption)", scopeID+"-uncovered")
	}
}

// TestGenerationRetentionCoveredNeverActivatedPrunesPastCeilingLive pins the
// #7981 Q2 ruling: NO uncovered change. A covered-but-never-activated
// generation older than the hard ceiling prunes despite its rank, while an
// uncovered writer older than the ceiling is retained and counted.
func TestGenerationRetentionCoveredNeverActivatedPrunesPastCeilingLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID = "samesha-ceiling"
	testfixtures.SeedScope(t, ctx, database, scopeID)
	century := now.Add(-100 * 24 * time.Hour)

	// The coverage horizon: an activated full too recent to be eligible.
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-full", sameSHAGenerationSeed{
		supersededAt: now.Add(-30 * time.Minute), activatedAt: now.Add(-30 * time.Minute),
		writeStartedAt: now.Add(-2 * time.Hour),
	})
	// Covered but never activated: wrote before the last activated full, so
	// the probe does not need it; rank 1 of 24, past the 90-day ceiling.
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-covered", sameSHAGenerationSeed{
		supersededAt: century, writeStartedAt: century.Add(time.Hour),
	})
	// Uncovered past the ceiling: wrote after the last activated full, so
	// the probe still needs it at any age.
	seedSameSHAGeneration(t, ctx, database, scopeID, scopeID+"-uncovered", sameSHAGenerationSeed{
		supersededAt: century, writeStartedAt: now.Add(-time.Hour),
	})

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(ctx, sameSHAPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1 (the covered-but-never-activated generation)", result.GenerationsPruned)
	}
	if got := result.Skipped["uncovered_writer"]; got != 1 {
		t.Errorf(`Skipped["uncovered_writer"] = %d, want 1 (the past-ceiling uncovered writer)`, got)
	}
	remaining := remainingScopeGenerations(t, ctx, database, []string{
		scopeID + "-full", scopeID + "-covered", scopeID + "-uncovered",
	})
	if remaining[scopeID+"-covered"] {
		t.Error("covered-but-never-activated generation was retained, want pruned past the ceiling despite rank 1 of 24")
	}
	if !remaining[scopeID+"-uncovered"] {
		t.Error("past-ceiling uncovered writer was pruned, want retained and counted")
	}
	if !remaining[scopeID+"-full"] {
		t.Error("coverage-horizon full was pruned, want retained (too recent to be eligible)")
	}
}
