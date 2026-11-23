// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10882 latch write interleaving on real PostgreSQL: engagements and releases of one latch row race from independent pools, each winning statement is a real flip of the row, the wins alternate, and the fleet counter and every process's gauge land on the row's final state
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestPacingLatchEngageReleaseInterleavingLive races stale engagements and
// stale releases of one latch row (consults that read the same prior state
// and then wrote in either order) across independent pools, round after
// round. Under READ COMMITTED each winning statement must be a real flip of
// the row as the statement saw it under its lock, so the winning writes in a
// round alternate and leave the row on the state the flip parity predicts;
// across the run the engagements the fleet counted minus the releases it
// won equal the row's final bit, and once every process observes the final
// row the fleet gauge equals it too.
func TestPacingLatchEngageReleaseInterleavingLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "latch_interleave",
		workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
	key := pacingLatchKey{signal: workflow.ClaimPacingSignalReducerSource, domain: "interleave-source"}
	consumers := newLatchConsumers(t, database, latchRaceConsumers, time.Now().UTC())

	const rounds = 60
	state := false
	for round := 0; round < rounds; round++ {
		before := fleetChangedWrites(consumers)
		raceAll(t, consumers, func(c *latchConsumer) error {
			// Even consumers write an engagement, odd consumers a release;
			// each round flips which side the lowest clocks belong to so
			// neither side always arrives first.
			engaged := (indexOfConsumer(consumers, c)+round)%2 == 0
			return c.store.writeClaimPacingLatch(ctx, key, engaged, c.now.Add(time.Duration(round)*time.Second))
		})
		flips := fleetChangedWrites(consumers) - before
		row := rowOrDisengaged(t, database, key)
		if want := state != (flips%2 == 1); row.engaged != want {
			t.Fatalf("round %d: %d row-changing writes from engaged=%v left engaged=%v, want %v",
				round, flips, state, row.engaged, want)
		}
		state = row.engaged
	}

	engageWins, releaseWins := 0, 0
	for _, c := range consumers {
		engageWins += c.spy.wins(true)
		releaseWins += c.spy.wins(false)
	}
	finalBit := 0
	if state {
		finalBit = 1
	}
	if engageWins-releaseWins != finalBit {
		t.Errorf("engagement wins %d minus release wins %d = %d, want the row's final bit %d",
			engageWins, releaseWins, engageWins-releaseWins, finalBit)
	}
	if got := fleetPacingSum(t, consumers, pacingEngagedTotalMetric, "reducer_source"); got != int64(engageWins) {
		t.Errorf("fleet-summed engaged_total = %d, want the %d engagement wins", got, engageWins)
	}

	final := rowOrDisengaged(t, database, key)
	for _, c := range consumers {
		c.store.observeClaimPacingLatch(ctx, key,
			claimPacingLatchRow{engaged: final.engaged, engagedAt: final.engagedAt.Time}, time.Now())
	}
	if got := fleetPacingSum(t, consumers, pacingActiveMetric, "reducer_source"); got != int64(finalBit) {
		t.Errorf("fleet-summed active gauge = %d after every process observed the row, want %d", got, finalBit)
	}
	for i, c := range consumers {
		if got := c.pacingMetric(t, pacingActiveMetric, "reducer_source"); got < 0 || got > 1 {
			t.Errorf("consumer %d active gauge = %d, want 0 or 1", i, got)
		}
	}
}

// indexOfConsumer returns the position of c in consumers.
func indexOfConsumer(consumers []*latchConsumer, c *latchConsumer) int {
	for i, candidate := range consumers {
		if candidate == c {
			return i
		}
	}
	return -1
}

// rowOrDisengaged reads a latch row, treating a row that was never written
// as disengaged.
func rowOrDisengaged(t *testing.T, database *sql.DB, key pacingLatchKey) latchRowSnapshot {
	t.Helper()
	var row latchRowSnapshot
	err := database.QueryRow(`
SELECT engaged, engaged_at, updated_at, xmin::text
FROM workflow_claim_pacing WHERE signal = $1 AND domain = $2`, string(key.signal), key.domain).
		Scan(&row.engaged, &row.engagedAt, &row.updatedAt, &row.xmin)
	if errors.Is(err, sql.ErrNoRows) {
		return latchRowSnapshot{}
	}
	if err != nil {
		t.Fatalf("read latch row %s/%s: %v", key.signal, key.domain, err)
	}
	return row
}
