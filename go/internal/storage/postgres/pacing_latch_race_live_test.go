// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10882 latch transition accounting on real PostgreSQL: N independent pools consult an over-threshold source (and an over-threshold depth domain) at once and exactly one statement moves the shared latch row, one engagement is counted fleet-wide, engaged_at is the winner's, a steady-state consult writes nothing, and after release every process's active gauge reads 0
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const latchRaceConsumers = 12

// assertOneTransition checks the accounting after one racing engagement:
// exactly one statement moved the row, one engagement is counted in the
// fleet, the gauge holds the one engagement, and engaged_at is the
// winning consult's clock.
func assertOneTransition(t *testing.T, consumers []*latchConsumer, signal string, row latchRowSnapshot) {
	t.Helper()
	if got := fleetChangedWrites(consumers); got != 1 {
		t.Errorf("latch writes that changed a row = %d across %d racing consults, want 1", got, len(consumers))
	}
	if got := fleetPacingSum(t, consumers, pacingEngagedTotalMetric, signal); got != 1 {
		t.Errorf("fleet-summed engaged_total = %d, want 1", got)
	}
	if got := fleetPacingSum(t, consumers, pacingActiveMetric, signal); got != 1 {
		t.Errorf("fleet-summed active gauge = %d while engaged, want 1", got)
	}
	if !row.engaged {
		t.Fatal("latch row engaged = false after the race, want true")
	}
	var winners int
	for _, c := range consumers {
		if c.pacingMetric(t, pacingEngagedTotalMetric, signal) == 1 {
			winners++
			if !row.engagedAt.Valid || !row.engagedAt.Time.Equal(c.now) {
				t.Errorf("engaged_at = %v, want the winning consult's clock %v", row.engagedAt.Time, c.now)
			}
		}
	}
	if winners != 1 {
		t.Errorf("consumers that counted the engagement = %d, want 1", winners)
	}
}

// assertEveryGaugeZero checks the post-release contract: each process's own
// gauge reads 0, whether or not it wrote the release.
func assertEveryGaugeZero(t *testing.T, consumers []*latchConsumer, signal string) {
	t.Helper()
	for i, c := range consumers {
		if got := c.pacingMetric(t, pacingActiveMetric, signal); got != 0 {
			t.Errorf("consumer %d active gauge = %d after release, want 0", i, got)
		}
	}
}

// TestSourceBackpressureLatchRaceCountsOneTransitionLive races N independent
// pools through CheckSourceBackpressureForSource against a source past its
// engage age, then races the release, then consults again in steady state.
func TestSourceBackpressureLatchRaceCountsOneTransitionLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "latch_race_source",
		workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
	const (
		source  = "race-source"
		scopeID = "latch-race-scope"
	)
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open seed connection: %v", err)
	}
	defer func() { _ = sqlConn.Close() }()
	seedNow := time.Now().UTC()
	seedTakeableShape(t, ctx, reducerClaimBenchmarkConn{conn: sqlConn}, seedNow,
		scopeID, source, "supply_chain_impact", "pending", "", 30, 30*24*time.Hour)

	cfg := sourceBackpressureTestConfig()
	consumers := newLatchConsumers(t, database, latchRaceConsumers, seedNow)

	raceAll(t, consumers, func(c *latchConsumer) error {
		decision, err := c.store.CheckSourceBackpressureForSource(ctx, source, cfg, c.now)
		if err == nil && !decision.Slowed {
			return errors.New("source not slowed under a 30-day-old backlog")
		}
		return err
	})
	engaged := readLatchRow(t, database, "reducer_source", source)
	assertOneTransition(t, consumers, "reducer_source", engaged)

	// Steady state: every process consults again; nothing may write.
	raceAll(t, consumers, func(c *latchConsumer) error {
		_, err := c.store.CheckSourceBackpressureForSource(ctx, source, cfg, c.now.Add(time.Second))
		return err
	})
	steady := readLatchRow(t, database, "reducer_source", source)
	if steady.xmin != engaged.xmin || !steady.updatedAt.Equal(engaged.updatedAt) {
		t.Errorf("steady-state consults rewrote the latch row: xmin %s -> %s, updated_at %v -> %v",
			engaged.xmin, steady.xmin, engaged.updatedAt, steady.updatedAt)
	}

	// Release: the backlog clears and every process sees it at once.
	if _, err := database.ExecContext(ctx, `DELETE FROM fact_work_items WHERE scope_id = $1`, scopeID); err != nil {
		t.Fatalf("clear backlog: %v", err)
	}
	raceAll(t, consumers, func(c *latchConsumer) error {
		decision, err := c.store.CheckSourceBackpressureForSource(ctx, source, cfg, c.now.Add(time.Minute))
		if err == nil && decision.Slowed {
			return errors.New("source still slowed after the backlog cleared")
		}
		return err
	})
	if got := fleetChangedWrites(consumers); got != 2 {
		t.Errorf("latch writes that changed a row after release = %d, want 2 (one engage, one release)", got)
	}
	released := readLatchRow(t, database, "reducer_source", source)
	if released.engaged {
		t.Fatal("latch row engaged = true after release, want false")
	}

	// A process that did not write the release drops its gauge when it next
	// observes the disengaged row; the observation must not rewrite the row.
	raceAll(t, consumers, func(c *latchConsumer) error {
		_, err := c.store.CheckSourceBackpressureForSource(ctx, source, cfg, c.now.Add(time.Minute+time.Second))
		return err
	})
	assertEveryGaugeZero(t, consumers, "reducer_source")
	if got := fleetPacingSum(t, consumers, pacingEngagedTotalMetric, "reducer_source"); got != 1 {
		t.Errorf("fleet-summed engaged_total after release = %d, want 1", got)
	}
	final := readLatchRow(t, database, "reducer_source", source)
	if final.xmin != released.xmin || !final.updatedAt.Equal(released.updatedAt) {
		t.Errorf("post-release observation rewrote the latch row: xmin %s -> %s", released.xmin, final.xmin)
	}
}

// TestWorkflowClaimPacingLatchRaceCountsOneTransitionLive is the same proof
// for the #8470 depth latch: N pools consult a family past its domain high
// watermark, then the backlog drains below the low watermark.
func TestWorkflowClaimPacingLatchRaceCountsOneTransitionLive(t *testing.T) {
	database, store, ctx, _ := openClaimDepthHarness(t, 1)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	seedClaimDepthBacklog(t, store, ctx, "latch-race-depth", base,
		[]claimDepthKeyBacklog{{FairnessKey: "latch:hot", Count: 60}})
	cfg := testClaimPacingConfig()
	selector := workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "claim-depth-instance",
		OwnerID:             "latch-race-owner",
		ClaimID:             "latch-race-claim",
	}
	domainKey := string(selector.CollectorKind) + "/" + selector.CollectorInstanceID
	consumers := newLatchConsumers(t, database, latchRaceConsumers, time.Now().UTC())

	raceAll(t, consumers, func(c *latchConsumer) error {
		decision, err := c.store.CheckClaimPacing(ctx, selector, cfg, c.now)
		if err == nil && !decision.Paced {
			return errors.New("family not paced with 60 pending over domain high 50")
		}
		return err
	})
	engaged := readLatchRow(t, database, "domain", domainKey)
	assertOneTransition(t, consumers, "domain", engaged)

	raceAll(t, consumers, func(c *latchConsumer) error {
		_, err := c.store.CheckClaimPacing(ctx, selector, cfg, c.now.Add(time.Second))
		return err
	})
	steady := readLatchRow(t, database, "domain", domainKey)
	if steady.xmin != engaged.xmin || !steady.updatedAt.Equal(engaged.updatedAt) {
		t.Errorf("steady-state consults rewrote the latch row: xmin %s -> %s", engaged.xmin, steady.xmin)
	}

	completeClaimDepthItems(t, store, ctx, "latch-race-owner", 55)
	raceAll(t, consumers, func(c *latchConsumer) error {
		decision, err := c.store.CheckClaimPacing(ctx, selector, cfg, c.now.Add(2*time.Second))
		if err == nil && decision.Paced {
			return errors.New("family still paced with 5 pending under domain low 20")
		}
		return err
	})
	released := readLatchRow(t, database, "domain", domainKey)
	if released.engaged {
		t.Fatal("latch row engaged = true after release, want false")
	}
	if got := fleetChangedWrites(consumers); got != 2 {
		t.Errorf("latch writes that changed a row = %d, want 2 (one engage, one release)", got)
	}
	raceAll(t, consumers, func(c *latchConsumer) error {
		_, err := c.store.CheckClaimPacing(ctx, selector, cfg, c.now.Add(3*time.Second))
		return err
	})
	assertEveryGaugeZero(t, consumers, "domain")
	if got := fleetPacingSum(t, consumers, pacingEngagedTotalMetric, "domain"); got != 1 {
		t.Errorf("fleet-summed engaged_total after release = %d, want 1", got)
	}
}
