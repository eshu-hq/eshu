// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestDefaultGenerationRetentionPolicyHardCeiling pins the #7585 hard
// history ceiling: an unset ceiling keeps 90 days so ordinary superseded
// history cannot outlive it even when the count window would retain it.
func TestDefaultGenerationRetentionPolicyHardCeiling(t *testing.T) {
	t.Parallel()

	policy := DefaultGenerationRetentionPolicy()
	want := 90 * 24 * time.Hour
	if policy.HardMaxSupersededAge != want {
		t.Fatalf("HardMaxSupersededAge = %v, want %v", policy.HardMaxSupersededAge, want)
	}
}

// TestGenerationRetentionPolicyNormalizeHardCeilingDefaults proves an unset
// (non-positive) hard ceiling falls back to the 90-day default instead of
// disabling the ceiling.
func TestGenerationRetentionPolicyNormalizeHardCeilingDefaults(t *testing.T) {
	t.Parallel()

	policy := GenerationRetentionPolicy{}.normalize()
	want := 90 * 24 * time.Hour
	if policy.HardMaxSupersededAge != want {
		t.Fatalf("normalized HardMaxSupersededAge = %v, want %v", policy.HardMaxSupersededAge, want)
	}
}

// TestGenerationRetentionPolicyNormalizeDerivesHardCeilingFromLongSoftAge
// pins #7611: an unset hard ceiling resolves to the soft window when that is
// longer than 90 days, so it never contradicts the window it caps.
func TestGenerationRetentionPolicyNormalizeDerivesHardCeilingFromLongSoftAge(t *testing.T) {
	t.Parallel()

	soft := 87600 * time.Hour
	policy := GenerationRetentionPolicy{MaxSupersededAge: soft}.normalize()
	if policy.HardMaxSupersededAge != soft {
		t.Fatalf("normalized HardMaxSupersededAge = %v, want %v", policy.HardMaxSupersededAge, soft)
	}
	if got := DefaultGenerationRetentionHardMaxAge(soft); got != soft {
		t.Fatalf("DefaultGenerationRetentionHardMaxAge(%v) = %v, want %v", soft, got, soft)
	}
	if got, want := DefaultGenerationRetentionHardMaxAge(7*24*time.Hour), 90*24*time.Hour; got != want {
		t.Fatalf("DefaultGenerationRetentionHardMaxAge(168h) = %v, want %v", got, want)
	}
}

// TestGenerationRetentionStoreAcceptsUnsetHardCeilingWithLongSoftAge proves a
// programmatic caller that leaves the hard ceiling unset with a soft window
// above 90 days is not refused as contradictory (#7611).
func TestGenerationRetentionStoreAcceptsUnsetHardCeilingWithLongSoftAge(t *testing.T) {
	t.Parallel()

	store := NewGenerationRetentionStore(&generationRetentionFakeDB{})
	_, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         87600 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	})
	if err != nil && strings.Contains(err.Error(), "hard max superseded age") {
		t.Fatalf("PruneSupersededGenerations() error = %v, want no hard-ceiling contradiction for an unset ceiling", err)
	}
}

// TestGenerationRetentionCandidateQueryEnforcesHardCeiling proves the
// all-scope candidate query caps the count preference with the hard
// ceiling: a generation older than the hard cutoff is eligible even when
// its rank is within the retained count.
func TestGenerationRetentionCandidateQueryEnforcesHardCeiling(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"OR ranked.superseded_at < $4",
		"superseded_rank > $2",
	} {
		if !strings.Contains(generationRetentionCandidateQuery, want) {
			t.Fatalf("candidate query missing hard-ceiling branch %q", want)
		}
	}
}

// TestGenerationRetentionTargetedQueryEnforcesHardCeiling proves the
// single-candidate re-lock honors the same hard ceiling so an over-limit
// batch of one narrowed after the savepoint rollback cannot keep a
// hard-expired generation.
func TestGenerationRetentionTargetedQueryEnforcesHardCeiling(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"OR generation.superseded_at < $6",
	} {
		if !strings.Contains(generationRetentionTargetedCandidateQuery, want) {
			t.Fatalf("targeted candidate query missing hard-ceiling branch %q", want)
		}
	}
}

// TestGenerationRetentionStoreRejectsHardCeilingBelowSoftAge proves a
// programmatic policy whose hard ceiling undercuts the soft keep window
// fails closed before locking anything, mirroring the reducer startup
// validation on the env path.
func TestGenerationRetentionStoreRejectsHardCeilingBelowSoftAge(t *testing.T) {
	t.Parallel()

	store := NewGenerationRetentionStore(&generationRetentionFakeDB{})
	_, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	})
	if err == nil || !strings.Contains(err.Error(), "hard max superseded age") {
		t.Fatalf("PruneSupersededGenerations() error = %v, want hard-ceiling contradiction rejection", err)
	}
}
