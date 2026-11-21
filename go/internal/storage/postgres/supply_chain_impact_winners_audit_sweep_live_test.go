// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	stagedaudit "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/staged/audit"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// liveAuditFleet runs every sweep statement on the disposable database: the
// built auditor with the real range statements, NULL probe, cursor row and
// writer verify, minus the standby routing.
type liveAuditFleet struct{ db *sql.DB }

func (liveAuditFleet) PinMember(context.Context) (string, error) { return "live", nil }
func (f liveAuditFleet) RunOnMember(ctx context.Context, _ string, fn func(context.Context, db.Queryer) error) error {
	return fn(ctx, postgres.SQLDB{DB: f.db})
}
func (liveAuditFleet) ReplayLag(context.Context, string) (time.Duration, error) { return 0, nil }

func newLiveSweepAuditor(h *keyedHarness, budget int) (*stagedaudit.StagedAuditor, *pgaccess.StagedAuditTelemetry) {
	tel := pgaccess.NewStagedAuditTelemetry()
	return &stagedaudit.StagedAuditor{
		Fleet: liveAuditFleet{db: h.db}, Store: h.store, Telemetry: tel, RangeBudget: budget,
		// No keyed tick runs here unless a test starts one, so the keyed
		// yield after a locked range ends on this interval.
		KeyedInterval: 200 * time.Millisecond, YieldPoll: 10 * time.Millisecond,
	}, tel
}

// sweepUntil runs passes until one reports no More, returning every pass.
func sweepUntil(t *testing.T, ctx context.Context, a *stagedaudit.StagedAuditor, maxPasses int) []supplychaincore.SupplyChainImpactWinnersAudit {
	t.Helper()
	var passes []supplychaincore.SupplyChainImpactWinnersAudit
	for len(passes) < maxPasses {
		got, err := a.AuditWinners(ctx)
		if err != nil {
			t.Fatalf("pass %d: %v", len(passes), err)
		}
		passes = append(passes, got)
		if !got.More {
			return passes
		}
	}
	t.Fatalf("sweep did not end in %d passes", maxPasses)
	return nil
}

// TestSupplyChainImpactWinnersResweepAuditSweepRepairsWithinOneSweepLive is
// the #11131 proof (g) on the built path: seeded stale, missing and orphan
// stored winners are all queued by one sweep at a small budget, and the
// keyed ticks after it leave the table equal to a fresh recompute. The sweep
// completes (the persisted row records it) with no budget deferral.
func TestSupplyChainImpactWinnersResweepAuditSweepRepairsWithinOneSweepLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit sweep repairs")
	auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)
	stale := auditSingleStatementCandidates(t, h.ctx, h.db)

	a, tel := newLiveSweepAuditor(h, 50)
	passes := sweepUntil(t, h.ctx, a, 1000)
	final := passes[len(passes)-1]
	if final.Partial || final.Outcome != stagedaudit.OutcomeCompleted {
		t.Fatalf("final pass = %+v, want a completed sweep", final)
	}
	var repairs int64
	for _, p := range passes {
		repairs += p.Repairs
	}
	if repairs != int64(len(stale)) {
		t.Fatalf("sweep queued %d repairs, want one per stale identifier (%d)", repairs, len(stale))
	}
	if n := tel.Deferrals(pgaccess.StagedAuditDeferBudget); n != 0 {
		t.Fatalf("budget deferrals = %d, want 0", n)
	}
	sweep, err := h.store.ReadWinnersAuditSweep(h.ctx)
	if err != nil {
		t.Fatalf("read sweep: %v", err)
	}
	if sweep.LastCompletedAt.IsZero() || sweep.LastRanges != int64(len(passes)) || sweep.LastRepairs != repairs || sweep.Cursor != nil {
		t.Fatalf("persisted sweep = %+v, want a completion of %d ranges and %d repairs with the cursor reset", sweep, len(passes), repairs)
	}
	for i := 0; i < 20 && h.dirtyDepth()+h.stagingDepth() > 0; i++ {
		h.keyed()
	}
	h.assertParity("after one audit sweep and its keyed repairs")
	if left := auditSingleStatementCandidates(t, h.ctx, h.db); len(left) != 0 {
		t.Fatalf("single-statement audit still finds %d stale identifiers after the sweep's repairs", len(left))
	}
}

// TestSupplyChainImpactWinnersResweepAuditSweepResumesAfterRollLive is the
// #11131 proof (e): a sweep interrupted after three ranges resumes in a new
// process from the persisted cursor, and the two halves recompute every live
// fact exactly once.
func TestSupplyChainImpactWinnersResweepAuditSweepResumesAfterRollLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit sweep resume")
	liveFacts := auditRangeSeed(t, h, auditRangeFixtureIdentifiers)

	first, firstTel := newLiveSweepAuditor(h, 50)
	for i := 0; i < 3; i++ {
		if got, err := first.AuditWinners(h.ctx); err != nil || got.Outcome != stagedaudit.OutcomeAdvanced {
			t.Fatalf("range %d before the roll = %+v, %v", i, got, err)
		}
	}
	mid, err := h.store.ReadWinnersAuditSweep(h.ctx)
	if err != nil || mid.Cursor == nil || mid.RangesDone != 3 {
		t.Fatalf("persisted sweep before the roll = %+v, %v; want a cursor after 3 ranges", mid, err)
	}
	// The roll: a fresh auditor with no process memory of the sweep.
	second, secondTel := newLiveSweepAuditor(h, 50)
	passes := sweepUntil(t, h.ctx, second, 1000)
	if passes[len(passes)-1].Outcome != stagedaudit.OutcomeCompleted {
		t.Fatalf("resumed sweep ended %+v, want completed", passes[len(passes)-1])
	}
	done, err := h.store.ReadWinnersAuditSweep(h.ctx)
	if err != nil {
		t.Fatalf("read sweep: %v", err)
	}
	if done.LastRanges != int64(3+len(passes)) || done.LastFacts != int64(liveFacts) {
		t.Fatalf("completed sweep = %d ranges, %d facts; want %d ranges and each of the %d live facts once",
			done.LastRanges, done.LastFacts, 3+len(passes), liveFacts)
	}
	if firstTel.Ranges() != 3 || secondTel.Ranges() != int64(len(passes)) {
		t.Fatalf("ranges by process = %d + %d, want 3 + %d", firstTel.Ranges(), secondTel.Ranges(), len(passes))
	}
}

// TestSupplyChainImpactWinnersResweepAuditSweepGuardsLive is the #11131
// proof (f) on live Postgres: the NULL probe reads clean on the fixture and
// finds a source fact with no repository_id (an operator fact with no
// package is exempt); a range over a fact with '|' in its package reports
// the pipe; and the built auditor defers on each.
func TestSupplyChainImpactWinnersResweepAuditSweepGuardsLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit sweep guards")
	auditRangeSeed(t, h, 10)
	reader := postgres.SQLDB{DB: h.db}
	if keys, err := postgres.ProbeWinnersAuditNullKeys(h.ctx, reader); err != nil || keys.Any() {
		t.Fatalf("NULL probe on the clean fixture = %+v, %v; want all false (the no-package operator fact is exempt)", keys, err)
	}
	if rng, err := postgres.ReadWinnersAuditRange(h.ctx, reader, nil, 100000); err != nil || rng.Pipe {
		t.Fatalf("range over the clean fixture = pipe %v, %v; want no pipe", rng.Pipe, err)
	}
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	scope := "vuln-intel://11131/s0"
	gen := scope + "/active"
	insert := func(id, payload string) {
		h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), false, $5::jsonb)`, id, scope, gen, kind, payload)
	}

	insert("null-repo", `{"cve_id":"CVE-N","advisory_id":"GHSA-N","package_id":"pkg:npm/p00","subject_digest":""}`)
	if keys, err := postgres.ProbeWinnersAuditNullKeys(h.ctx, reader); err != nil || !keys.RepositoryID {
		t.Fatalf("NULL probe with a NULL repository_id = %+v, %v; want RepositoryID", keys, err)
	}
	a, tel := newLiveSweepAuditor(h, 50)
	if got, err := a.AuditWinners(h.ctx); err != nil || !got.Deferred || tel.Deferrals(pgaccess.StagedAuditDeferNullKey) != 1 {
		t.Fatalf("sweep start with a NULL key = %+v, %v; want a null_key deferral", got, err)
	}
	h.exec(`DELETE FROM fact_records WHERE fact_id = 'null-repo'`)

	insert("pipe", `{"cve_id":"CVE-P","advisory_id":"GHSA-P","package_id":"pkg:npm/p00|x","repository_id":"repo-0","subject_digest":""}`)
	if rng, err := postgres.ReadWinnersAuditRange(h.ctx, reader, nil, 100000); err != nil || !rng.Pipe {
		t.Fatalf("range over a '|' package = pipe %v, %v; want the pipe guard", rng.Pipe, err)
	}
	whole, wholeTel := newLiveSweepAuditor(h, 100000)
	if got, err := whole.AuditWinners(h.ctx); err != nil || !got.Deferred || wholeTel.Deferrals(pgaccess.StagedAuditDeferPipe) != 1 {
		t.Fatalf("range with a '|' identifier = %+v, %v; want a pipe deferral", got, err)
	}
}

// TestSupplyChainImpactWinnersResweepAuditSweepCursorRaceLive is the #11131
// proof (c): two instances verifying the same range advance the cursor
// exactly once. Run serially, the second finds the cursor moved and rolls
// its repairs back; run concurrently, exactly one of two advances; and two
// instances sweeping the whole corpus at once commit each range once and
// queue each repair once.
func TestSupplyChainImpactWinnersResweepAuditSweepCursorRaceLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit sweep cursor race")
	auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)
	stale := auditSingleStatementCandidates(t, h.ctx, h.db)
	reader := postgres.SQLDB{DB: h.db}

	sweep, err := h.store.ReadWinnersAuditSweep(h.ctx)
	if err != nil {
		t.Fatalf("read sweep: %v", err)
	}
	rng, err := postgres.ReadWinnersAuditRange(h.ctx, reader, nil, 1)
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	// The first identifier is the orphan before every fact, so the range
	// carries a candidate and its verify queues a repair.
	req := postgres.WinnersAuditRangeVerify{SweepID: sweep.SweepID, Range: rng}
	won, err := h.store.VerifyWinnersAuditRange(h.ctx, req)
	if err != nil || !won.Advanced || won.Repairs < 1 {
		t.Fatalf("first verify = %+v, %v; want advanced with at least the orphan's repair", won, err)
	}
	queued := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_staging WHERE source = 'audit'`)
	lost, err := h.store.VerifyWinnersAuditRange(h.ctx, req)
	if err != nil || !lost.CursorRace || lost.Advanced {
		t.Fatalf("second verify of the same range = %+v, %v; want cursor_race", lost, err)
	}
	if after := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_staging WHERE source = 'audit'`); after != queued {
		t.Fatalf("audit staging rows %d after the lost race, want %d: the loser's repairs must roll back", after, queued)
	}

	next, err := postgres.ReadWinnersAuditRange(h.ctx, reader, rng.Hi, 50)
	if err != nil {
		t.Fatalf("read next range: %v", err)
	}
	var wg sync.WaitGroup
	outcomes := make([]postgres.WinnersAuditRangeOutcome, 2)
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := postgres.WinnersAuditRangeVerify{SweepID: sweep.SweepID, Range: next, WaitForLock: true}
			out, err := h.store.VerifyWinnersAuditRange(h.ctx, req)
			if err != nil {
				t.Errorf("concurrent verify %d: %v", i, err)
			}
			outcomes[i] = out
		}(i)
	}
	wg.Wait()
	if outcomes[0].Advanced == outcomes[1].Advanced {
		t.Fatalf("concurrent verifies of one range = %+v / %+v, want exactly one advance", outcomes[0], outcomes[1])
	}

	// Two instances sweep the rest at once.
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_staging`)
	h.exec(`UPDATE supply_chain_impact_winners_audit_sweep SET cursor_package_id = NULL, cursor_repository_id = NULL,
  cursor_subject_digest = NULL, cursor_c0 = NULL, cursor_advisory_id = NULL, ranges_done = 0, sweep_started_at = NULL,
  sweep_facts = 0, sweep_candidates = 0, sweep_repairs = 0, sweep_discards = 0, sweep_id = sweep_id + 1`)
	var advanced [2]int64
	for i := range advanced {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, tel := newLiveSweepAuditor(h, 50)
			for pass := 0; pass < 2000; pass++ {
				got, err := a.AuditWinners(h.ctx)
				if err != nil {
					t.Errorf("instance %d pass %d: %v", i, pass, err)
					return
				}
				if !got.More {
					break
				}
			}
			advanced[i] = tel.Ranges()
		}(i)
	}
	wg.Wait()
	serial := auditSweepAll(t, h.ctx, h.db, 50, postgres.WinnersAuditFirstRangeSQLForTest, postgres.WinnersAuditNextRangeSQLForTest)
	if total := advanced[0] + advanced[1]; total < int64(serial.ranges) {
		t.Fatalf("two instances committed %d + %d ranges, want at least the %d ranges of one sweep", advanced[0], advanced[1], serial.ranges)
	}
	dups := h.count(`SELECT count(*) FROM (SELECT 1 FROM supply_chain_impact_winners_dirty_staging WHERE source = 'audit'
  GROUP BY package_id, repository_id, subject_digest, c0, advisory_id HAVING count(*) > 1) AS d`)
	rows := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_staging WHERE source = 'audit'`)
	if dups != 0 || rows != len(stale) {
		t.Fatalf("audit staging after two concurrent sweeps: %d rows, %d duplicated; want each of the %d stale identifiers once", rows, dups, len(stale))
	}
	t.Logf("concurrent sweep: instance ranges %d + %d (one sweep is %d ranges)", advanced[0], advanced[1], serial.ranges)
}
