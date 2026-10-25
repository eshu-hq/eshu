// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8725 forced-serialization RED proof on real PostgreSQL: serializing claim grants collapses attempt-window overlap to one, proving the overlap signal responds to contention instead of sitting at a fixed value
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"
	"time"
)

// TestWorkflowClaimPacingForcedSerialTripsOverlapGate is the negative control
// for the #8725 overlap gate: eight enrolled claimers behind the test-only
// grant mutex grant strictly one at a time, so the attempt-window max overlap
// must collapse and trip the gate. If this test ever passes the gate (overlap
// at or above half the enrolled claimers), the gate no longer detects
// serialization. It runs only with a live Postgres (skips without
// ESHU_POSTGRES_DSN) and takes about as long as the idle drain it mirrors.
func TestWorkflowClaimPacingForcedSerialTripsOverlapGate(t *testing.T) {
	claimDepthSerializeGrants = true
	defer func() { claimDepthSerializeGrants = false }()
	database, store, ctx, poolLimit := openClaimDepthHarness(t, claimDepthClaimers)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seeded := seedClaimDepthBacklog(t, store, ctx, "pacing-forced-serial", base, []claimDepthKeyBacklog{
		{FairnessKey: "pacing:forced-large", Count: 500},
		{FairnessKey: "pacing:forced-small-1", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "pacing:forced-small-2", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "pacing:forced-small-3", Count: 20, CreatedOffset: time.Second},
		{FairnessKey: "pacing:forced-small-4", Count: 20, CreatedOffset: time.Second},
	})
	result := drainClaimDepthBacklog(t, database, ctx, "pacing-forced-serial", claimDepthClaimers, poolLimit)
	assertClaimDepthExactlyOnce(t, seeded, result)
	overlap := claimDepthMaxOverlap(result.Records)
	t.Logf("forced-serial drain items=%d claimers=%d duration=%s max grant overlap = %d (gate trips below %d)",
		len(seeded), claimDepthClaimers, result.Duration, overlap, claimDepthClaimers/2)
	if overlap >= claimDepthClaimers/2 {
		t.Fatalf("forced-serial drain max grant overlap = %d, want below %d (half of %d enrolled claimers; the gate must trip under serialization)",
			overlap, claimDepthClaimers/2, claimDepthClaimers)
	}
}
