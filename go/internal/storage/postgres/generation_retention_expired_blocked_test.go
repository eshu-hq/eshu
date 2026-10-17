// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// The #7585 PR1 boundary proofs for the expired-but-blocked report:
// history past the hard ceiling that a safety exception retains must
// carry its blocked reason and age instead of reading as compliant.

func expiredBlockedTestPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     90 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	}
}

// TestEvaluateGenerationRetentionActiveCurrentException proves a generation
// past the ceiling that is still the scope's active generation reports
// expired-but-blocked with the active/current reason and its age, never
// eligible and never silently compliant.
func TestEvaluateGenerationRetentionActiveCurrentException(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	evaluation := expiredBlockedTestPolicy().EvaluateGenerationRetention(now,
		GenerationRetentionEvaluationInput{
			SupersededAt:       now.Add(-100 * 24 * time.Hour),
			IsActiveGeneration: true,
		})
	if !evaluation.Expired {
		t.Fatal("Expired = false, want true for 100-day-old history past the 90-day ceiling")
	}
	if !evaluation.ExpiredBlocked() {
		t.Fatal("ExpiredBlocked() = false, want true for an active generation past the ceiling")
	}
	if evaluation.Compliant() {
		t.Fatal("Compliant() = true, want false for expired-but-blocked history")
	}
	if evaluation.BlockedReason != GenerationRetentionBlockedActiveCurrent {
		t.Fatalf("BlockedReason = %q, want %q", evaluation.BlockedReason, GenerationRetentionBlockedActiveCurrent)
	}
	if evaluation.SupersededAge != 100*24*time.Hour {
		t.Fatalf("SupersededAge = %v, want %v", evaluation.SupersededAge, 100*24*time.Hour)
	}
}

// TestEvaluateGenerationRetentionLiveWorkPin proves past-ceiling history with
// in-flight (claimed or running) work reports the live-work reason.
func TestEvaluateGenerationRetentionLiveWorkPin(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	evaluation := expiredBlockedTestPolicy().EvaluateGenerationRetention(now,
		GenerationRetentionEvaluationInput{
			SupersededAt: now.Add(-100 * 24 * time.Hour),
			HasLiveWork:  true,
		})
	if !evaluation.ExpiredBlocked() {
		t.Fatal("ExpiredBlocked() = false, want true for live-work-pinned history past the ceiling")
	}
	if evaluation.BlockedReason != GenerationRetentionBlockedLiveWork {
		t.Fatalf("BlockedReason = %q, want %q", evaluation.BlockedReason, GenerationRetentionBlockedLiveWork)
	}
	if evaluation.SupersededAge != 100*24*time.Hour {
		t.Fatalf("SupersededAge = %v, want %v", evaluation.SupersededAge, 100*24*time.Hour)
	}
}

// TestEvaluateGenerationRetentionRetryingPin proves past-ceiling history with
// only retrying work reports the retry reason, distinct from in-flight work.
func TestEvaluateGenerationRetentionRetryingPin(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	evaluation := expiredBlockedTestPolicy().EvaluateGenerationRetention(now,
		GenerationRetentionEvaluationInput{
			SupersededAt:    now.Add(-100 * 24 * time.Hour),
			HasRetryingWork: true,
		})
	if !evaluation.ExpiredBlocked() {
		t.Fatal("ExpiredBlocked() = false, want true for retry-pinned history past the ceiling")
	}
	if evaluation.BlockedReason != GenerationRetentionBlockedRetrying {
		t.Fatalf("BlockedReason = %q, want %q", evaluation.BlockedReason, GenerationRetentionBlockedRetrying)
	}
}

// TestEvaluateGenerationRetentionDependencyPin proves past-ceiling history a
// dependency pins reports the dependency-pin reason.
func TestEvaluateGenerationRetentionDependencyPin(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	evaluation := expiredBlockedTestPolicy().EvaluateGenerationRetention(now,
		GenerationRetentionEvaluationInput{
			SupersededAt:     now.Add(-100 * 24 * time.Hour),
			DependencyPinned: true,
		})
	if !evaluation.ExpiredBlocked() {
		t.Fatal("ExpiredBlocked() = false, want true for dependency-pinned history past the ceiling")
	}
	if evaluation.BlockedReason != GenerationRetentionBlockedDependencyPin {
		t.Fatalf("BlockedReason = %q, want %q", evaluation.BlockedReason, GenerationRetentionBlockedDependencyPin)
	}
}

// TestEvaluateGenerationRetentionCeilingEdge pins the exact 90-day boundary
// against the candidate SQL (`superseded_at < now - ceiling`): history
// exactly at the ceiling is still compliant, one second past it is expired
// and, with no block signal, eligible with an empty reason.
func TestEvaluateGenerationRetentionCeilingEdge(t *testing.T) {
	t.Parallel()

	policy := expiredBlockedTestPolicy()
	now := time.Now().UTC()
	atCeiling := policy.EvaluateGenerationRetention(now, GenerationRetentionEvaluationInput{
		SupersededAt: now.Add(-90 * 24 * time.Hour),
	})
	if atCeiling.Expired {
		t.Fatal("Expired = true, want false for history exactly at the 90-day ceiling")
	}
	if !atCeiling.Compliant() {
		t.Fatal("Compliant() = false, want true for history exactly at the ceiling")
	}
	pastCeiling := policy.EvaluateGenerationRetention(now, GenerationRetentionEvaluationInput{
		SupersededAt: now.Add(-90*24*time.Hour - time.Second),
	})
	if !pastCeiling.Expired {
		t.Fatal("Expired = false, want true for history one second past the ceiling")
	}
	if pastCeiling.ExpiredBlocked() {
		t.Fatal("ExpiredBlocked() = true, want false for unblocked history past the ceiling")
	}
	if pastCeiling.BlockedReason != "" {
		t.Fatalf("BlockedReason = %q, want empty for eligible history", pastCeiling.BlockedReason)
	}
}

// TestEvaluateGenerationRetentionWithinCeilingIgnoresBlockSignals proves
// history inside the ceiling stays compliant even when every block signal
// is set: the report covers expired-but-blocked history only.
func TestEvaluateGenerationRetentionWithinCeilingIgnoresBlockSignals(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	evaluation := expiredBlockedTestPolicy().EvaluateGenerationRetention(now,
		GenerationRetentionEvaluationInput{
			SupersededAt:       now.Add(-10 * 24 * time.Hour),
			IsActiveGeneration: true,
			HasLiveWork:        true,
			HasRetryingWork:    true,
			DependencyPinned:   true,
		})
	if evaluation.Expired {
		t.Fatal("Expired = true, want false for 10-day-old history inside the ceiling")
	}
	if !evaluation.Compliant() {
		t.Fatal("Compliant() = false, want true for history inside the ceiling")
	}
	if evaluation.BlockedReason != "" {
		t.Fatalf("BlockedReason = %q, want empty for history inside the ceiling", evaluation.BlockedReason)
	}
}

// TestEvaluateGenerationRetentionBlockedPrecedence pins the reason order
// when several blocks apply: active/current outranks live work, which
// outranks retrying, which outranks a dependency pin.
func TestEvaluateGenerationRetentionBlockedPrecedence(t *testing.T) {
	t.Parallel()

	policy := expiredBlockedTestPolicy()
	now := time.Now().UTC()
	for _, tc := range []struct {
		name  string
		input GenerationRetentionEvaluationInput
		want  GenerationRetentionBlockedReason
	}{
		{
			name: "active outranks everything",
			input: GenerationRetentionEvaluationInput{
				IsActiveGeneration: true, HasLiveWork: true, HasRetryingWork: true, DependencyPinned: true,
			},
			want: GenerationRetentionBlockedActiveCurrent,
		},
		{
			name:  "live work outranks retrying and pin",
			input: GenerationRetentionEvaluationInput{HasLiveWork: true, HasRetryingWork: true, DependencyPinned: true},
			want:  GenerationRetentionBlockedLiveWork,
		},
		{
			name:  "retrying outranks pin",
			input: GenerationRetentionEvaluationInput{HasRetryingWork: true, DependencyPinned: true},
			want:  GenerationRetentionBlockedRetrying,
		},
	} {
		tc.input.SupersededAt = now.Add(-100 * 24 * time.Hour)
		if evaluation := policy.EvaluateGenerationRetention(now, tc.input); evaluation.BlockedReason != tc.want {
			t.Errorf("%s: BlockedReason = %q, want %q", tc.name, evaluation.BlockedReason, tc.want)
		}
	}
}

// TestEvaluateGenerationRetentionUnsetCeilingDefaultsToNinetyDays proves the
// evaluation resolves an unset ceiling through the same 90-day default as
// the prune, so the report and the deletion bound cannot drift apart.
func TestEvaluateGenerationRetentionUnsetCeilingDefaultsToNinetyDays(t *testing.T) {
	t.Parallel()

	var policy GenerationRetentionPolicy
	now := time.Now().UTC()
	if evaluation := policy.EvaluateGenerationRetention(now, GenerationRetentionEvaluationInput{
		SupersededAt: now.Add(-100 * 24 * time.Hour),
	}); !evaluation.Expired {
		t.Fatal("Expired = false, want true for 100-day-old history under the default ceiling")
	}
	if evaluation := policy.EvaluateGenerationRetention(now, GenerationRetentionEvaluationInput{
		SupersededAt: now.Add(-10 * 24 * time.Hour),
	}); !evaluation.Compliant() {
		t.Fatal("Compliant() = false, want true for 10-day-old history under the default ceiling")
	}
}

// TestEvaluateGenerationRetentionFutureSupersededAtClampsToZero proves a
// superseded timestamp ahead of now (clock skew, never observed in the
// prune) evaluates as brand-new history rather than a negative age.
func TestEvaluateGenerationRetentionFutureSupersededAtClampsToZero(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	evaluation := expiredBlockedTestPolicy().EvaluateGenerationRetention(now,
		GenerationRetentionEvaluationInput{SupersededAt: now.Add(time.Hour)})
	if evaluation.SupersededAge != 0 {
		t.Fatalf("SupersededAge = %v, want 0 for a future superseded_at", evaluation.SupersededAge)
	}
	if !evaluation.Compliant() {
		t.Fatal("Compliant() = false, want true for a future superseded_at")
	}
}
