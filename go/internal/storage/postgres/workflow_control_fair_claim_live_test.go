// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8469 step 3 rotation proofs on real PostgreSQL: a single-claimer lottery with fixed salts grants exactly the independently simulated salted-draw sequence (the statement honors the salt end to end), and mixed priority tiers drain strictly tier by tier with rotation only inside a tier; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// lotteryWinner predicts the rotation draw for one claim: the eligible key
// with the smallest sha256(key || salt) hex digest, exactly the ordered
// CTE's rule, over the caller-supplied per-key queues. It is the independent
// model the live proof checks the statement against.
func lotteryWinner(queues map[string][]string, salt string) string {
	winner := ""
	best := ""
	for key, queue := range queues {
		if len(queue) == 0 {
			continue
		}
		sum := sha256.Sum256([]byte(key + salt))
		draw := hex.EncodeToString(sum[:])
		if winner == "" || draw < best {
			winner, best = key, draw
		}
	}
	return winner
}

// TestWorkflowFairClaimLotteryMatchesSimulatedDraw proves the statement
// honors the rotation salt end to end: forty sequential single-claimer
// claims with fixed salts grant exactly the sequence an independent Go
// simulation of the salted draw predicts. A static order would serve one
// key forty times here; the pinned distribution below shows the rotation.
func TestWorkflowFairClaimLotteryMatchesSimulatedDraw(t *testing.T) {
	_, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	keys := []string{"fair-draw:a", "fair-draw:b", "fair-draw:c", "fair-draw:d", "fair-draw:e"}
	var backlogs []claimDepthKeyBacklog
	for _, key := range keys {
		backlogs = append(backlogs, claimDepthKeyBacklog{FairnessKey: key, Count: 10})
	}
	seedClaimDepthBacklog(t, store, ctx, "fair-draw", base, backlogs)

	queues := make(map[string][]string, len(keys))
	for _, key := range keys {
		for i := 0; i < 10; i++ {
			queues[key] = append(queues[key], fmt.Sprintf("fair-draw:%s:%04d", key, i))
		}
	}
	const claims = 40
	counts := make(map[string]int)
	for i := 0; i < claims; i++ {
		salt := fmt.Sprintf("fair-lottery-%d", i)
		want := lotteryWinner(queues, salt)
		if want == "" {
			t.Fatalf("simulation found no eligible key at claim %d", i)
		}
		item, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "claim-depth-instance",
			OwnerID:             "fair-draw-owner",
			ClaimID:             fmt.Sprintf("fair-draw-%d", i),
			RotationSalt:        salt,
		}, base.Add(time.Hour).UTC(), 5*time.Minute)
		if err != nil {
			t.Fatalf("ClaimNextEligible claim %d error = %v, want nil", i, err)
		}
		if !found {
			t.Fatalf("ClaimNextEligible claim %d found = false, want a grant", i)
		}
		if item.FairnessKey != want {
			t.Fatalf("claim %d granted key %q, simulated draw says %q (salt %q)",
				i, item.FairnessKey, want, salt)
		}
		queues[want] = queues[want][1:]
		counts[want]++
	}
	for _, key := range keys {
		t.Logf("lottery distribution key=%s grants=%d", key, counts[key])
	}
}

// TestWorkflowFairClaimPriorityPrecedesRotation proves cross-tier
// precedence is absolute: with a priority-0 burst behind four priority-1
// small keys, the first ten grants are all burst regardless of salt, and
// with the tiers flipped the first ten are all small keys. Rotation only
// rotates inside a tier.
func TestWorkflowFairClaimPriorityPrecedesRotation(t *testing.T) {
	for _, tier := range []struct {
		name          string
		burstPriority int
		smallPriority int
		wantFirstTen  string
	}{
		{name: "burst-first", burstPriority: 0, smallPriority: 1, wantFirstTen: "fair-tier:burst"},
		{name: "smalls-first", burstPriority: 1, smallPriority: 0, wantFirstTen: "fair-tier:small"},
	} {
		t.Run(tier.name, func(t *testing.T) {
			_, store := openWorkflowControlIntegrationStore(t)
			ctx := context.Background()
			base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
			backlogs := []claimDepthKeyBacklog{
				{FairnessKey: "fair-tier:burst", Count: 30, Priority: tier.burstPriority},
			}
			for i := 1; i <= 4; i++ {
				backlogs = append(backlogs, claimDepthKeyBacklog{
					FairnessKey:   fmt.Sprintf("fair-tier:small-%d", i),
					Count:         5,
					CreatedOffset: time.Second,
					Priority:      tier.smallPriority,
				})
			}
			seedClaimDepthBacklog(t, store, ctx, "fair-tier-"+tier.name, base, backlogs)
			for i := 0; i < 10; i++ {
				item, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
					CollectorKind:       scope.CollectorGit,
					CollectorInstanceID: "claim-depth-instance",
					OwnerID:             "fair-tier-owner",
					ClaimID:             fmt.Sprintf("fair-tier-%s-%d", tier.name, i),
					RotationSalt:        fmt.Sprintf("fair-tier-%s-%d", tier.name, i),
				}, base.Add(time.Hour).UTC(), 5*time.Minute)
				if err != nil {
					t.Fatalf("ClaimNextEligible claim %d error = %v, want nil", i, err)
				}
				if !found {
					t.Fatalf("ClaimNextEligible claim %d found = false, want a grant", i)
				}
				var got string
				if item.FairnessKey == "fair-tier:burst" {
					got = "fair-tier:burst"
				} else {
					got = "fair-tier:small"
				}
				if got != tier.wantFirstTen {
					t.Fatalf("claim %d granted %q, want tier %q (priority precedes rotation)",
						i, item.FairnessKey, tier.wantFirstTen)
				}
			}
		})
	}
}

// TestWorkflowFairnessKeyDepthsReportsTopKeysAndDistinctCount proves the
// fairness observer: twelve pending keys under one family report the top
// ten by depth with the family's distinct count of twelve, so the depth
// gauge series stay bounded while the count gauge keeps the denominator.
func TestWorkflowFairnessKeyDepthsReportsTopKeysAndDistinctCount(t *testing.T) {
	_, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	var backlogs []claimDepthKeyBacklog
	for i := 0; i < 12; i++ {
		backlogs = append(backlogs, claimDepthKeyBacklog{
			FairnessKey: fmt.Sprintf("fair-depth:key-%02d", i),
			Count:       i + 1,
		})
	}
	seedClaimDepthBacklog(t, store, ctx, "fair-depth", base, backlogs)

	depths, err := store.WorkflowFairnessKeyDepths(ctx)
	if err != nil {
		t.Fatalf("WorkflowFairnessKeyDepths error = %v, want nil", err)
	}
	family, ok := depths[string(scope.CollectorGit)]["claim-depth-instance"]
	if !ok {
		t.Fatalf("no depth for git/claim-depth-instance in %#v", depths)
	}
	if family.DistinctKeys != 12 {
		t.Fatalf("DistinctKeys = %d, want 12", family.DistinctKeys)
	}
	if len(family.Top) != 10 {
		t.Fatalf("len(Top) = %d, want 10 (top-N bound)", len(family.Top))
	}
	for i, key := range family.Top {
		wantKey := fmt.Sprintf("fair-depth:key-%02d", 11-i)
		if key.Key != wantKey || key.Pending != int64(12-i) {
			t.Fatalf("Top[%d] = %+v, want key %q pending %d", i, key, wantKey, 12-i)
		}
	}
}

// TestWorkflowClaimDepthStaggeredStartCollapsesRetries runs the skewed
// drain with desynchronized claimer starts (claimer i waits i*5ms after
// the barrier). It asserts the same gates as the barrier run and logs the
// empty rate for comparison. Measured: staggering changes nothing, which
// pins the retry rate to the back-to-back duty cycle (concurrent claim
// statements over draw slots), not to start-phase alignment.
func TestWorkflowClaimDepthStaggeredStartHoldsGates(t *testing.T) {
	claimDepthStartStagger = 5 * time.Millisecond
	t.Cleanup(func() { claimDepthStartStagger = 0 })
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	keys := []claimDepthKeyBacklog{{FairnessKey: "claim-depth:large", Count: 500}}
	for _, small := range []string{"small-1", "small-2", "small-3", "small-4"} {
		keys = append(keys, claimDepthKeyBacklog{FairnessKey: "claim-depth:" + small, Count: 20, CreatedOffset: time.Second})
	}
	seeded := seedClaimDepthBacklog(t, store, ctx, "claim-depth-stagger", base, keys)

	result := drainClaimDepthBacklog(t, database, ctx, claimDepthClaimers, poolLimit)

	assertClaimDepthConcurrency(t, result, claimDepthClaimers)
	assertClaimDepthExactlyOnce(t, seeded, result)
	assertClaimDepthMinGrantsPerOwner(t, result, claimDepthClaimers, 10)
	if got := result.ClaimsPerSecond(); got < 119 {
		t.Fatalf("staggered drain throughput = %.1f claims/s, want >= 119 (design floor)", got)
	}
	t.Logf("staggered drain items=%d claimers=%d duration=%s claims_per_second=%.1f empty_attempts=%d",
		len(seeded), claimDepthClaimers, result.Duration, result.ClaimsPerSecond(), result.EmptyAttempts)
}

// assertClaimDepthMinGrantsPerOwner proves every claimer kept granting deep
// into the drain: a claimer that exits on a spurious mid-drain empty while
// eligible work exists leaves its share behind, so a drain carried by one
// claimer while seven idle fails here even when the totals still drain.
func assertClaimDepthMinGrantsPerOwner(t *testing.T, result claimDepthResult, claimers, min int) {
	t.Helper()
	perOwner := make(map[string]int, len(result.Records))
	for _, record := range result.Records {
		perOwner[record.OwnerID]++
	}
	for c := 0; c < claimers; c++ {
		owner := fmt.Sprintf("claim-depth-owner-%d", c)
		if perOwner[owner] < min {
			t.Fatalf("owner %q granted %d items, want >= %d (all %d claimers share the drain)",
				owner, perOwner[owner], min, claimers)
		}
	}
}
