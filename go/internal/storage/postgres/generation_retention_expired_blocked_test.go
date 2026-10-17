// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
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

// expiredBlockedFixtureRow builds one #7585 PR2 report fixture row in the
// report query's column order: scope id, generation id, superseded at,
// is-active, live work, retrying work, uncovered, delta-protected.
func expiredBlockedFixtureRow(now time.Time, generationID string, age time.Duration, isActive, live, retrying, uncovered, delta bool) []any {
	return []any{"scope-1", generationID, now.Add(-age), isActive, live, retrying, uncovered, delta}
}

// selectExpiredWithFixtures runs the PR2 live-signal report against canned
// store rows at a fixed now, returning the report keyed by generation id.
func selectExpiredWithFixtures(t *testing.T, now time.Time, policy GenerationRetentionPolicy, rows [][]any) map[string]GenerationRetentionExpiredGeneration {
	t.Helper()

	database := &generationRetentionFakeDB{expiredBlockedRows: rows}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	report, err := store.SelectExpiredGenerations(context.Background(), policy)
	if err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	byID := make(map[string]GenerationRetentionExpiredGeneration, len(report))
	for _, generation := range report {
		byID[generation.GenerationID] = generation
	}
	return byID
}

// TestSelectExpiredGenerationsMapsLiveStoreSignals proves the PR2 wiring
// maps the candidate selection's store state onto the PR1 evaluation: past
// the 90-day ceiling, claimed/running work reports live_work, retrying-only
// work reports retrying, a stuck active pointer reports active_current with
// precedence over work signals, and unblocked history reports eligible with
// an empty reason instead of reading as compliant.
func TestSelectExpiredGenerationsMapsLiveStoreSignals(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	century := 100 * 24 * time.Hour
	byID := selectExpiredWithFixtures(t, now, expiredBlockedTestPolicy(), [][]any{
		expiredBlockedFixtureRow(now, "gen-live", century, false, true, false, false, false),
		expiredBlockedFixtureRow(now, "gen-retrying", century, false, false, true, false, false),
		expiredBlockedFixtureRow(now, "gen-active", century, true, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-eligible", century, false, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-multi", century, true, true, true, false, false),
	})
	for _, tc := range []struct {
		generationID string
		wantReason   GenerationRetentionBlockedReason
		wantBlocked  bool
	}{
		{"gen-live", GenerationRetentionBlockedLiveWork, true},
		{"gen-retrying", GenerationRetentionBlockedRetrying, true},
		{"gen-active", GenerationRetentionBlockedActiveCurrent, true},
		{"gen-multi", GenerationRetentionBlockedActiveCurrent, true},
		{"gen-eligible", "", false},
	} {
		generation, ok := byID[tc.generationID]
		if !ok {
			t.Fatalf("report omits %s, want expired history past the ceiling surfaced", tc.generationID)
		}
		if generation.Evaluation.BlockedReason != tc.wantReason {
			t.Errorf("%s: BlockedReason = %q, want %q", tc.generationID, generation.Evaluation.BlockedReason, tc.wantReason)
		}
		if generation.ExpiredBlocked() != tc.wantBlocked {
			t.Errorf("%s: ExpiredBlocked() = %v, want %v", tc.generationID, generation.ExpiredBlocked(), tc.wantBlocked)
		}
		if !generation.Evaluation.Expired {
			t.Errorf("%s: Expired = false, want true for 100-day-old history", tc.generationID)
		}
		if generation.Evaluation.Compliant() {
			t.Errorf("%s: Compliant() = true, want false for expired history", tc.generationID)
		}
		if generation.Evaluation.SupersededAge != century {
			t.Errorf("%s: SupersededAge = %v, want %v", tc.generationID, generation.Evaluation.SupersededAge, century)
		}
		if generation.ScopedOut() {
			t.Errorf("%s: ScopedOut() = true, want false for a plain expired generation", tc.generationID)
		}
	}
}

// TestSelectExpiredGenerationsReturnsOldestFirst proves the report
// surfaces the oldest expired history first under a small limit, mirroring
// the live statement's ORDER BY superseded_at ASC, generation_id ASC: with
// newest-first fixtures and limit 2, the two oldest rows win.
func TestSelectExpiredGenerationsReturnsOldestFirst(t *testing.T) {
	t.Parallel()

	if !strings.Contains(generationRetentionExpiredReportQuery, "work.scope_id = generation.scope_id") {
		t.Fatal("report query omits the scope_id probe predicate, want prefix-seeking EXISTS probes")
	}

	now := time.Now().UTC()
	policy := expiredBlockedTestPolicy()
	policy.BatchGenerationLimit = 2
	database := &generationRetentionFakeDB{expiredBlockedRows: [][]any{
		expiredBlockedFixtureRow(now, "gen-new", 100*24*time.Hour, false, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-old", 200*24*time.Hour, false, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-oldest", 300*24*time.Hour, false, false, false, false, false),
	}}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	report, err := store.SelectExpiredGenerations(context.Background(), policy)
	if err != nil {
		t.Fatalf("SelectExpiredGenerations() error = %v", err)
	}
	if len(report) != 2 {
		t.Fatalf("len(report) = %d, want 2 oldest rows under limit 2", len(report))
	}
	if report[0].GenerationID != "gen-oldest" || report[1].GenerationID != "gen-old" {
		t.Errorf("report order = [%s %s], want [gen-oldest gen-old]",
			report[0].GenerationID, report[1].GenerationID)
	}
}

// TestSelectExpiredGenerationsOmitsCompliantHistory proves superseded
// history inside the ceiling never enters the expired report: compliant
// history stays absent rather than surfaced with a reason or an age.
func TestSelectExpiredGenerationsOmitsCompliantHistory(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	byID := selectExpiredWithFixtures(t, now, expiredBlockedTestPolicy(), [][]any{
		expiredBlockedFixtureRow(now, "gen-young", 10*24*time.Hour, false, true, false, false, false),
		expiredBlockedFixtureRow(now, "gen-old", 100*24*time.Hour, false, true, false, false, false),
	})
	if _, ok := byID["gen-young"]; ok {
		t.Fatal("report surfaces gen-young, want 10-day-old history omitted as compliant")
	}
	if _, ok := byID["gen-old"]; !ok {
		t.Fatal("report omits gen-old, want 100-day-old history surfaced")
	}
}

// TestSelectExpiredGenerationsCeilingEdge pins the report's ceiling cutoff
// against the candidate SQL (`superseded_at < now - ceiling`): history
// exactly at the 90-day ceiling stays out of the report, one second past it
// is surfaced as expired and eligible.
func TestSelectExpiredGenerationsCeilingEdge(t *testing.T) {
	t.Parallel()

	if !strings.Contains(generationRetentionExpiredReportQuery, "superseded_at < ") {
		t.Fatal("report query misses the strict `superseded_at < cutoff` predicate the candidate query uses")
	}
	now := time.Now().UTC()
	ceiling := 90 * 24 * time.Hour
	byID := selectExpiredWithFixtures(t, now, expiredBlockedTestPolicy(), [][]any{
		expiredBlockedFixtureRow(now, "gen-at-ceiling", ceiling, false, false, false, false, false),
		expiredBlockedFixtureRow(now, "gen-past-ceiling", ceiling+time.Second, false, false, false, false, false),
	})
	if _, ok := byID["gen-at-ceiling"]; ok {
		t.Fatal("report surfaces gen-at-ceiling, want history exactly at the ceiling omitted")
	}
	past, ok := byID["gen-past-ceiling"]
	if !ok {
		t.Fatal("report omits gen-past-ceiling, want history one second past the ceiling surfaced")
	}
	if !past.Evaluation.Expired || past.Evaluation.BlockedReason != "" {
		t.Fatalf("gen-past-ceiling = %+v, want expired and eligible with an empty reason", past.Evaluation)
	}
}

// TestSelectExpiredGenerationsScopesOutUncoveredAndDeltaProtected proves the
// PR2 report explicitly scopes out the classes with no reason in the PR1
// vocabulary (#7472 uncovered writers, #7802 delta-protected history): they
// ride the report with their flags visible but never read as
// expired-but-blocked or eligible.
func TestSelectExpiredGenerationsScopesOutUncoveredAndDeltaProtected(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	century := 100 * 24 * time.Hour
	byID := selectExpiredWithFixtures(t, now, expiredBlockedTestPolicy(), [][]any{
		expiredBlockedFixtureRow(now, "gen-uncovered", century, false, false, false, true, false),
		expiredBlockedFixtureRow(now, "gen-delta", century, false, false, false, false, true),
	})
	uncovered, ok := byID["gen-uncovered"]
	if !ok {
		t.Fatal("report omits gen-uncovered, want scoped-out history visible with its flag")
	}
	if !uncovered.Uncovered || !uncovered.ScopedOut() {
		t.Fatalf("gen-uncovered = %+v, want Uncovered with ScopedOut() true", uncovered)
	}
	if uncovered.ExpiredBlocked() {
		t.Fatal("gen-uncovered: ExpiredBlocked() = true, want false for scoped-out history")
	}
	delta, ok := byID["gen-delta"]
	if !ok {
		t.Fatal("report omits gen-delta, want scoped-out history visible with its flag")
	}
	if !delta.DeltaProtected || !delta.ScopedOut() {
		t.Fatalf("gen-delta = %+v, want DeltaProtected with ScopedOut() true", delta)
	}
	if delta.ExpiredBlocked() {
		t.Fatal("gen-delta: ExpiredBlocked() = true, want false for scoped-out history")
	}
}

// TestSelectExpiredGenerationsDefaultsUnsetCeilingToNinetyDays proves the
// report resolves an unset ceiling through the same normalize the prune
// uses, so the report and the deletion bound cannot drift apart.
func TestSelectExpiredGenerationsDefaultsUnsetCeilingToNinetyDays(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	byID := selectExpiredWithFixtures(t, now, GenerationRetentionPolicy{}, [][]any{
		expiredBlockedFixtureRow(now, "gen-old", 100*24*time.Hour, false, true, false, false, false),
	})
	generation, ok := byID["gen-old"]
	if !ok {
		t.Fatal("report omits gen-old, want 100-day-old history expired under the default ceiling")
	}
	if generation.Evaluation.BlockedReason != GenerationRetentionBlockedLiveWork {
		t.Fatalf("BlockedReason = %q, want %q", generation.Evaluation.BlockedReason, GenerationRetentionBlockedLiveWork)
	}
}

// TestSelectExpiredGenerationsRejectsContradictoryPolicy proves the report
// fails closed on an explicit hard ceiling below the soft window, like the
// prune does, before reading any store state.
func TestSelectExpiredGenerationsRejectsContradictoryPolicy(t *testing.T) {
	t.Parallel()

	database := &generationRetentionFakeDB{}
	store := NewGenerationRetentionStore(database)
	_, err := store.SelectExpiredGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		HardMaxSupersededAge:     24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	})
	if err == nil || !strings.Contains(err.Error(), "hard max superseded age") {
		t.Fatalf("SelectExpiredGenerations() error = %v, want hard-ceiling contradiction rejection", err)
	}
	for _, statement := range database.predeleteStatements {
		if strings.Contains(statement, generationRetentionExpiredReportMarker) {
			t.Fatal("report query ran despite contradictory policy, want fail-closed before any store read")
		}
	}
}
