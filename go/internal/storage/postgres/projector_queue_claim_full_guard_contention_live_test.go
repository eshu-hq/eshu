// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"sort"
	"testing"
	"time"
)

// TestProjectorClaimFullGuardContention is the #7473 contention benchmark on
// the #7115 harness: sixteen workers claim 512 single-generation scopes per
// round, five interleaved rounds per variant, alternating before and after.
// The before variant runs the pre-#7473 text derived from the shipped
// constant; both variants behave identically on this single-generation shape,
// so the benchmark isolates the spare and hold predicates' cost under
// contention. It logs medians as No-Regression Evidence; it asserts only
// correctness (no error, no miss, no deadlock), never timing.
func TestProjectorClaimFullGuardContention(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, markGuardContentionWorkers+8)
	markGuardSeedContention(t, database)
	before, counts := fullGuardClaimBeforeText(claimProjectorWorkQuery)
	for name, count := range counts {
		if count != 1 {
			t.Fatalf("before text reverses %s %d times, want 1", name, count)
		}
	}
	deadlocksBefore := serverDeadlockCount(t, database)
	samples := map[string][]time.Duration{}
	for round := 0; round < markGuardContentionRounds; round++ {
		order := []struct {
			name      string
			statement string
		}{{"before", before}, {"after", claimProjectorWorkQuery}}
		if round%2 == 1 {
			order = []struct {
				name      string
				statement string
			}{{"after", claimProjectorWorkQuery}, {"before", before}}
		}
		for _, variant := range order {
			markGuardRefillContention(t, database)
			samples[variant.name] = append(samples[variant.name],
				markGuardContentionRound(t, database, variant.statement))
		}
	}
	if delta := serverDeadlockCount(t, database) - deadlocksBefore; delta != 0 {
		t.Fatalf("server recorded %d deadlocks, want 0", delta)
	}
	median := func(v []time.Duration) time.Duration {
		s := append([]time.Duration(nil), v...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return s[len(s)/2]
	}
	mean := func(v []time.Duration) time.Duration {
		var sum time.Duration
		for _, d := range v {
			sum += d
		}
		return sum / time.Duration(len(v))
	}
	b, a := samples["before"], samples["after"]
	bMed, aMed := median(b), median(a)
	bMean, aMean := mean(b), mean(a)
	perClaim := func(d time.Duration) time.Duration { return d / markGuardContentionScopes }
	t.Logf("No-Regression Evidence: contention rounds=%d workers=%d scopes=%d claims=%d "+
		"before_median=%s after_median=%s delta=%+.2f%% before_mean=%s after_mean=%s per_claim_before=%s per_claim_after=%s",
		markGuardContentionRounds, markGuardContentionWorkers, markGuardContentionScopes,
		markGuardContentionScopes*markGuardContentionRounds,
		bMed, aMed, (float64(aMed)/float64(bMed)-1)*100, bMean, aMean,
		perClaim(bMean), perClaim(aMean))
}
