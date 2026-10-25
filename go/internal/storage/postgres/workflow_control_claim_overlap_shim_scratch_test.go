// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// SCRATCH SHIM — measures max in-flight grant overlap for a healthy 8-way
// drain vs a forced-serial 1-way drain on real Postgres. Temporary
// experiment for #8725 P1 disposition; DELETE BEFORE COMMIT.

import (
	"sort"
	"testing"
	"time"
)

// shimMaxOverlap sweeps [start, DoneElapsed] windows and returns the maximum
// number in flight at any instant. start picks the window opening: grant
// return (Elapsed) or claim-attempt start (AttemptElapsed).
func shimMaxOverlap(records []claimDepthRecord, start func(claimDepthRecord) time.Duration) int {
	type event struct {
		at    time.Duration
		delta int
	}
	events := make([]event, 0, 2*len(records))
	for _, r := range records {
		s := start(r)
		if r.DoneElapsed <= s {
			continue
		}
		events = append(events, event{at: s, delta: 1}, event{at: r.DoneElapsed, delta: -1})
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].at == events[j].at {
			return events[i].delta < events[j].delta
		}
		return events[i].at < events[j].at
	})
	max, cur := 0, 0
	for _, e := range events {
		cur += e.delta
		if cur > max {
			max = cur
		}
	}
	return max
}

func TestShimClaimOverlapHealthy(t *testing.T) {
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "shim-healthy", base, []claimDepthKeyBacklog{
		{FairnessKey: "shim:large", Count: 500},
		{FairnessKey: "shim:small-1", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "shim:small-2", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "shim:small-3", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "shim:small-4", Count: 20, CreatedOffset: time.Second},
	})
	result := drainClaimDepthBacklog(t, database, ctx, "shim-healthy", claimDepthClaimers, poolLimit)
	assertClaimDepthExactlyOnce(t, seeded, result)
	t.Logf("SHIM healthy: items=%d claimers=%d duration=%s maxOverlapGrant=%d maxOverlapAttempt=%d",
		len(seeded), claimDepthClaimers, result.Duration,
		shimMaxOverlap(result.Records, func(r claimDepthRecord) time.Duration { return r.Elapsed }),
		shimMaxOverlap(result.Records, func(r claimDepthRecord) time.Duration { return r.AttemptElapsed }))
}

func TestShimClaimOverlapSerial(t *testing.T) {
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "shim-serial", base, []claimDepthKeyBacklog{
		{FairnessKey: "shim:large", Count: 500},
		{FairnessKey: "shim:small-1", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "shim:small-2", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "shim:small-3", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "shim:small-4", Count: 20, CreatedOffset: time.Second},
	})
	result := drainClaimDepthBacklog(t, database, ctx, "shim-serial", 1, poolLimit)
	assertClaimDepthExactlyOnce(t, seeded, result)
	t.Logf("SHIM serial: items=%d claimers=1 duration=%s maxOverlapGrant=%d maxOverlapAttempt=%d",
		len(seeded), result.Duration,
		shimMaxOverlap(result.Records, func(r claimDepthRecord) time.Duration { return r.Elapsed }),
		shimMaxOverlap(result.Records, func(r claimDepthRecord) time.Duration { return r.AttemptElapsed }))
}
