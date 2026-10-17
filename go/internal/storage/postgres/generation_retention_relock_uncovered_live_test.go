// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7903 disposable PostgreSQL 18 proof that the targeted re-lock drops a candidate that flips to uncovered-writer coverage after selection and counts it as skipped while still pruning a covered bystander, enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the hard-ceiling proof does. local run passed in 1.31s wall with the full 188-migration setup on postgres:18 on 2026-10-11
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestGenerationRetentionRelockDropsNewlyUncoveredWriterLive is #7903: the
// targeted re-lock must re-check uncovered-writer coverage at lock time. A
// candidate that was covered at selection flips to uncovered when a full
// generation with inverted ordering (newest by ingested_at, oldest by
// projection_write_started_at) activates between the selection's savepoint
// rollback and the re-lock. The re-lock must drop the flipped candidate,
// count it as skipped so it surfaces in the cycle log/counter instead of
// vanishing, and never prune it — while still pruning a bystander that
// stays covered across the flip.
func TestGenerationRetentionRelockDropsNewlyUncoveredWriterLive(t *testing.T) {
	// Bridge the live-postgres-readiness runner's family DSN onto the shared
	// helper's generic variable. Local runs keep using ESHU_POSTGRES_TEST_DSN
	// directly; the runner sets only the family pair, and a family DSN
	// without its disposable acknowledgment fails closed.
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	database, ctx := openGenerationRetentionMigratedSchema(t)
	now := time.Now().UTC()
	const scopeID = "retention-relock-flip"
	testfixtures.SeedScope(t, ctx, database, scopeID)

	old := now.Add(-10 * 24 * time.Hour)

	// F1: the coverage horizon at selection — an activated full, too recent
	// to be eligible itself, whose write started after both writers'.
	seedUncoveredWriterGeneration(t, ctx, database, scopeID, scopeID+"-full1", generationWriterSeed{
		supersededAt: now.Add(-30 * time.Minute), ingestedAt: now.Add(-30 * time.Minute),
		activatedAt: now.Add(-30 * time.Minute), writeStartedAt: now.Add(-2 * time.Hour),
		isDelta: false,
	})
	// Candidate: superseded, old, covered at selection (wrote after F1).
	seedUncoveredWriterGeneration(t, ctx, database, scopeID, scopeID+"-candidate", generationWriterSeed{
		supersededAt: old, ingestedAt: old,
		writeStartedAt: old.Add(24 * time.Hour), isDelta: false,
	})
	// Bystander: superseded, old, covered at selection and still covered
	// after the flip (wrote before F2 as well as F1: F2's write is the
	// oldest of all, the starkest inverted ordering).
	seedUncoveredWriterGeneration(t, ctx, database, scopeID, scopeID+"-bystander", generationWriterSeed{
		supersededAt: old, ingestedAt: old,
		writeStartedAt: old.Add(-72 * time.Hour), isDelta: true,
	})

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	store.beforeTargetedLock = func() {
		// A full activates between select and re-lock with inverted
		// ordering: newest by ingested_at, oldest by write-started. The
		// horizon flips from F1 to F2, the candidate flips covered to
		// uncovered, and the bystander stays covered.
		seedUncoveredWriterGeneration(t, ctx, database, scopeID, scopeID+"-full2", generationWriterSeed{
			supersededAt: now, ingestedAt: now, activatedAt: now,
			writeStartedAt: old.Add(-48 * time.Hour), isDelta: false,
		})
	}
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            1_000_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "7903-relock-live",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}

	remaining := remainingScopeGenerations(t, ctx, database, []string{
		scopeID + "-full1", scopeID + "-full2",
		scopeID + "-candidate", scopeID + "-bystander",
	})
	if !remaining[scopeID+"-candidate"] {
		t.Errorf("flipped candidate %s was pruned, want retained (uncovered at lock time)", scopeID+"-candidate")
	}
	if remaining[scopeID+"-bystander"] {
		t.Errorf("covered bystander %s was retained, want pruned (covered at lock time)", scopeID+"-bystander")
	}
	if !remaining[scopeID+"-full1"] || !remaining[scopeID+"-full2"] {
		t.Errorf("horizon fulls retained = %v, want both retained (too recent to be eligible)", remaining)
	}
	if result.GenerationsPruned != 1 {
		t.Errorf("GenerationsPruned = %d, want 1 (the bystander only)", result.GenerationsPruned)
	}
	if got := result.Skipped["uncovered_writer"]; got != 1 {
		t.Errorf(`Skipped["uncovered_writer"] = %d, want 1 (the flipped candidate)`, got)
	}
}
