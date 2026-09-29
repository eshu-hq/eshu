// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestGenerationLivenessProgressPredicateSharedByBothQueries pins the #7265
// contract that the recovery sweep and the age-bucket gauge carry the identical
// progress predicate, so "stuck" keeps meaning "eligible for recovery".
func TestGenerationLivenessProgressPredicateSharedByBothQueries(t *testing.T) {
	t.Parallel()

	if !strings.Contains(recoverWedgedActiveGenerationsQuery, "AND NOT "+generationIntentProgressingPredicate) {
		t.Fatalf("recover query must exclude progressing generations with the shared predicate:\n%s", recoverWedgedActiveGenerationsQuery)
	}
	if !strings.Contains(countActiveGenerationsByAgeQuery, "WHEN "+generationIntentProgressingPredicate+" THEN 'draining'") {
		t.Fatalf("count query must bucket progressing generations as draining with the shared predicate:\n%s", countActiveGenerationsByAgeQuery)
	}
	// Progress is per domain queue: the probe itself must not filter
	// completions by source_run_id (arbiter ruling on #7265).
	domainSet := "SELECT DISTINCT progressed_intent.projection_domain\n"
	for name, query := range map[string]string{
		"recover": recoverWedgedActiveGenerationsQuery,
		"count":   countActiveGenerationsByAgeQuery,
	} {
		if !strings.Contains(query, domainSet) {
			t.Fatalf("%s query missing the DISTINCT domain set:\n%s", name, query)
		}
		if strings.Contains(query, "progressed_intent.source_run_id") {
			t.Fatalf("%s query filters progress completions on source_run_id:\n%s", name, query)
		}
		if strings.Contains(query, "AS MATERIALIZED") {
			t.Fatalf("%s query uses AS MATERIALIZED, which the measured plan does not need:\n%s", name, query)
		}
	}
	if !strings.Contains(generationIntentProgressingPredicate, "starts_with(progress_intent.source_run_id, 'repo_dependency:')") {
		t.Fatalf("progress predicate must keep the exact repo_dependency actionability exclusion:\n%s", generationIntentProgressingPredicate)
	}
}

// TestGenerationLivenessProgressWindowParameters pins the bind positions the
// store supplies: $5 in the recovery sweep and $4 in the gauge count, plus the
// measured fence shape (unfenced recover, OFFSET 0 fenced count).
func TestGenerationLivenessProgressWindowParameters(t *testing.T) {
	t.Parallel()

	if !strings.Contains(recoverWedgedActiveGenerationsQuery, "WHERE progressed_intent.completed_at > $5") {
		t.Fatalf("recover query must bind the progress window start as $5:\n%s", recoverWedgedActiveGenerationsQuery)
	}
	if !strings.Contains(countActiveGenerationsByAgeQuery, "WHERE progressed_intent.completed_at > $4") {
		t.Fatalf("count query must bind the progress window start as $4:\n%s", countActiveGenerationsByAgeQuery)
	}
	if strings.Contains(recoverWedgedActiveGenerationsQuery, "OFFSET 0") {
		t.Fatal("recover query must leave liveness_progress unfenced; the fence regressed its join order")
	}
	if !strings.Contains(countActiveGenerationsByAgeQuery, "OFFSET 0\n) AS liveness_progress") {
		t.Fatal("count query must fence liveness_progress with OFFSET 0 to keep the plan out of JIT optimization")
	}
}

func TestGenerationLivenessPolicyNormalizeProgressWindow(t *testing.T) {
	t.Parallel()

	if got := (GenerationLivenessPolicy{}).Normalize().ProgressWindow; got != 10*time.Minute {
		t.Fatalf("default ProgressWindow = %v, want 10m", got)
	}
	if got := (GenerationLivenessPolicy{ProgressWindow: -time.Minute}).Normalize().ProgressWindow; got != 10*time.Minute {
		t.Fatalf("negative ProgressWindow normalized to %v, want 10m", got)
	}
	if got := (GenerationLivenessPolicy{ProgressWindow: 3 * time.Minute}).Normalize().ProgressWindow; got != 3*time.Minute {
		t.Fatalf("explicit ProgressWindow normalized to %v, want 3m", got)
	}
}

// TestGenerationLivenessStoreProgressWindowArgs proves the store binds the
// progress window start and keeps the re-driven generation ids and attempts.
func TestGenerationLivenessStoreProgressWindowArgs(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{}},
			{rows: [][]any{{"scope-a", "gen-a", 2}}},
			{rows: [][]any{{"draining", int64(4)}}},
		},
	}
	store := NewGenerationLivenessStore(db)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	policy := GenerationLivenessPolicy{ActivationDeadline: 30 * time.Minute, ProgressWindow: 7 * time.Minute}

	result, err := store.RecoverWedgedGenerations(context.Background(), policy, now)
	if err != nil {
		t.Fatalf("RecoverWedgedGenerations() error = %v", err)
	}
	recoverArgs := db.queries[1].args
	if len(recoverArgs) != 5 {
		t.Fatalf("recover arg count = %d, want 5", len(recoverArgs))
	}
	if got, ok := recoverArgs[4].(time.Time); !ok || !got.Equal(now.Add(-7*time.Minute)) {
		t.Fatalf("recover $5 = %v, want now-7m", recoverArgs[4])
	}
	want := GenerationLivenessRecovery{ScopeID: "scope-a", GenerationID: "gen-a", LivenessRecoveryAttempts: 2}
	if len(result.Recoveries) != 1 || result.Recoveries[0] != want {
		t.Fatalf("Recoveries = %+v, want [%+v]", result.Recoveries, want)
	}
	if len(result.RecoveredScopeIDs) != 1 || result.RecoveredScopeIDs[0] != "scope-a" || result.Recovered != 1 {
		t.Fatalf("Recovered=%d RecoveredScopeIDs=%v, want 1 [scope-a]", result.Recovered, result.RecoveredScopeIDs)
	}

	counts, err := store.CountActiveGenerationsByAge(context.Background(), policy, now)
	if err != nil {
		t.Fatalf("CountActiveGenerationsByAge() error = %v", err)
	}
	countArgs := db.queries[2].args
	if len(countArgs) != 4 {
		t.Fatalf("count arg count = %d, want 4", len(countArgs))
	}
	if got, ok := countArgs[3].(time.Time); !ok || !got.Equal(now.Add(-7*time.Minute)) {
		t.Fatalf("count $4 = %v, want now-7m", countArgs[3])
	}
	if counts["draining"] != 4 || counts["stuck"] != 0 {
		t.Fatalf("counts = %v, want draining=4 and a zeroed stuck bucket", counts)
	}
}
