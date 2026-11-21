// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stagedaudit "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/staged/audit"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersResweepAuditSweepKeyedTickSkippedDuringVerifyLive
// is the #11131 proof (d): while a range verify holds the resweep lock, a
// keyed tick returns Skipped and queues nothing; after the range commits,
// the next keyed tick commits and consumes both the queued mark and the
// range's repairs.
func TestSupplyChainImpactWinnersResweepAuditSweepKeyedTickSkippedDuringVerifyLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit sweep keyed tick during a range verify")
	auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)
	// A queued mark far from the first range, so the keyed tick has work.
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', 'pkg:npm/p07', 'repo-0', '', 'CVE-2026-59', 'GHSA-59')`)

	var (
		mu              sync.Mutex
		claimAt, commit time.Time
		during          error
		skipped         bool
	)
	timed := verifyTimingDB{
		inner: postgres.SQLDB{DB: h.db}, mu: &mu, claimAt: &claimAt, commit: &commit,
		onClaim: func() {
			r, err := h.store.ResweepWinnersKeyed(h.ctx)
			during, skipped = err, r.Skipped
		},
	}
	store := postgres.NewSupplyChainImpactWinnersStore(timed)
	sweep, err := store.ReadWinnersAuditSweep(h.ctx)
	if err != nil {
		t.Fatalf("read sweep: %v", err)
	}
	rng, err := postgres.ReadWinnersAuditRange(h.ctx, postgres.SQLDB{DB: h.db}, nil, 1)
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	out, err := store.VerifyWinnersAuditRange(h.ctx, postgres.WinnersAuditRangeVerify{SweepID: sweep.SweepID, Range: rng})
	if err != nil || !out.Advanced || !out.Locked || out.Repairs < 1 {
		t.Fatalf("range verify = %+v, %v; want a locked, advanced range with a repair", out, err)
	}
	if during != nil || !skipped {
		t.Fatalf("keyed tick during the verify = skipped %v, %v; want Skipped", skipped, during)
	}
	r := h.keyed()
	if r.Skipped || r.Identifiers < 2 {
		t.Fatalf("keyed tick after the range commit = %+v, want it to commit the mark and the repairs", r)
	}
	t.Logf("range verify held the lock %.3fs; keyed tick after commit consumed %d identifiers", commit.Sub(claimAt).Seconds(), r.Identifiers)
}

// TestSupplyChainImpactWinnersResweepAuditSweepAdvancesUnderFullKeyedDutyLive
// is the #11131 proof (h), theory measurement M5: a keyed-tick stand-in holds
// the resweep lock back to back by try-lock (about 100 percent duty), and
// every range of a full sweep still advances within three attempts: two
// try-lock misses, then the waiting claim, which PostgreSQL grants at the
// next release before the holder's next try-lock.
func TestSupplyChainImpactWinnersResweepAuditSweepAdvancesUnderFullKeyedDutyLive(t *testing.T) {
	h := newKeyedHarness(t, "#11131 audit sweep under full keyed duty")
	auditRangeSeed(t, h, auditRangeFixtureIdentifiers)
	auditRangePerturb(t, h)
	// Stale keys in many ranges, so most ranges need the lock.
	h.exec(`UPDATE supply_chain_impact_canonical_winners SET priority_score = priority_score + 1
WHERE abs(hashtext(canonical_key)) % 3 = 0`)

	ctx, stop := context.WithCancel(h.ctx)
	var held, total atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		loopStart := time.Now()
		defer func() { total.Store(int64(time.Since(loopStart))) }()
		for ctx.Err() == nil {
			tx, err := h.db.BeginTx(ctx, nil)
			if err != nil {
				return
			}
			var got bool
			if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('supply_chain_impact_winners'), hashtext('resweep'))`).Scan(&got); err != nil {
				_ = tx.Rollback()
				return
			}
			if got {
				start := time.Now()
				time.Sleep(250 * time.Millisecond)
				held.Add(int64(time.Since(start)))
			}
			_ = tx.Commit()
			if !got {
				// The stand-in never idles while it holds; on a miss it
				// retries at once, like a keyed tick that re-ticks.
				time.Sleep(time.Millisecond)
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)

	a, tel := newLiveSweepAuditor(h, 50)
	attempts, worst, locked, ranges := 0, 0, 0, 0
	for pass := 0; pass < 500; pass++ {
		got, err := a.AuditWinners(h.ctx)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		attempts++
		if got.Outcome != stagedaudit.OutcomeAdvanced && got.Outcome != stagedaudit.OutcomeCompleted {
			continue
		}
		ranges++
		worst = max(worst, attempts)
		if attempts > 1 {
			locked++
		}
		attempts = 0
		if !got.More {
			break
		}
	}
	stop()
	wg.Wait()
	duty := float64(held.Load()) / float64(total.Load())
	t.Logf("M5: %d ranges, %d needed more than one attempt, worst %d attempts; keyed stand-in duty %.1f%%; lock_busy %d",
		ranges, locked, worst, 100*duty, tel.Deferrals("lock_busy"))
	// The stand-in holds whenever the sweep does not, so its share is what
	// the sweep's own verifies leave it.
	if duty < 0.5 {
		t.Fatalf("keyed stand-in duty %.1f%%, want the lock held back to back for the proof to mean anything", 100*duty)
	}
	if worst > 3 {
		t.Fatalf("a range needed %d attempts under full keyed duty, want at most 3", worst)
	}
	if locked == 0 {
		t.Fatal("no range contended for the lock: the proof did not exercise the waiting claim")
	}
}
