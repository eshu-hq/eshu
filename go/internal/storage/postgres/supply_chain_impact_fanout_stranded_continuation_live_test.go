// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// strandedRuntime wires the convergence handler into a production-shaped
// reducer.Runtime: the real Postgres generation freshness check in front of
// the real handler, so a drain through it exercises the same
// claim -> GenerationCheck -> Handle -> Ack path the reducer service runs.
func strandedRuntime(t *testing.T, c *convergence) *reducer.Runtime {
	t.Helper()
	registry := reducer.NewRegistry()
	if err := registry.Register(reducer.DomainDefinition{
		Domain:        reducercontract.DomainSupplyChainImpact,
		Summary:       "stranded continuation proof",
		Ownership:     reducer.OwnershipShape{CrossSource: true, CrossScope: true, CanonicalWrite: true},
		TruthContract: truth.Contract{CanonicalKind: "supply_chain_impact_finding", SourceLayers: []truth.Layer{truth.LayerSourceDeclaration}},
		Handler:       c.handler,
	}); err != nil {
		t.Fatalf("register supply chain handler: %v", err)
	}
	rt, err := reducer.NewRuntime(registry)
	if err != nil {
		t.Fatalf("new reducer runtime: %v", err)
	}
	rt.GenerationCheck = postgres.NewGenerationFreshnessCheck(postgres.SQLDB{DB: c.db})
	return rt
}

// strandedDrain plays the reducer service loop (claim, Execute, ack or fail)
// until the queue has nothing claimable. A superseded result is acked, the
// way Service.ackReducerWork handles it.
func strandedDrain(t *testing.T, c *convergence, rt *reducer.Runtime) {
	t.Helper()
	for i := 0; i < 60; i++ {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			return
		}
		c.handled = append(c.handled, fmt.Sprintf("%s#%d", intent.ScopeID, intent.AttemptCount))
		result, err := rt.Execute(c.ctx, intent)
		if err != nil {
			if failErr := c.queue.Fail(c.ctx, intent, err); failErr != nil {
				t.Fatalf("fail %s: %v (execute error: %v)", intent.IntentID, failErr, err)
			}
			if _, execErr := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1 AND status = 'retrying'`, intent.IntentID); execErr != nil {
				t.Fatalf("make retry visible: %v", execErr)
			}
			continue
		}
		if err := c.queue.Ack(c.ctx, intent, result); err != nil {
			t.Fatalf("ack %s: %v", intent.IntentID, err)
		}
	}
	t.Fatalf("queue still had work after 60 claims; handled %v", c.handled)
}

// strandedContinuation commits a source change with the fan-out port unwired,
// then records the unreached holders on the item and acks it, leaving an
// authentic capped-fan-out continuation pending (the R1 emulation in
// supply_chain_impact_fanout_races_live_test.go: the cap is a package
// constant, so the deferred list is written through the production
// UpdateFanOutState seam). It returns the committing item's id.
func strandedContinuation(t *testing.T, c *convergence, remaining []string) string {
	t.Helper()
	noFanOut := c.handler
	noFanOut.FanOut = nil
	claim := c.raceClaim(convergenceSource)
	result, err := noFanOut.Handle(c.ctx, claim)
	if err != nil {
		t.Fatalf("handle committing claim: %v", err)
	}
	if err := c.fanOut.UpdateFanOutState(c.ctx, claim, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: remaining}); err != nil {
		t.Fatalf("record continuation list: %v", err)
	}
	if err := c.queue.Ack(c.ctx, claim, result); err != nil {
		t.Fatalf("ack committing claim: %v", err)
	}
	if got := fanOutItem(t, c.ctx, c.db, claim.IntentID); got.status != "pending" || got.flag {
		t.Fatalf("continuation after ack = %+v, want pending with the replay mark consumed", got)
	}
	return claim.IntentID
}

// TestFanOutRaceStrandedContinuationDrainsAfterNewerGenerationActivatesLive is
// the #10060 acceptance regression over the real queue: a fan-out beyond the
// holder cap, a newer generation activating before the continuation is
// claimed, and the holders left behind. Before the fix the first claim after
// the activation superseded the continuation (terminal), the successor
// generation's pass diffed clean against the committing pass's rows and
// fanned out to nobody, and the holder kept its stale copy.
func TestFanOutRaceStrandedContinuationDrainsAfterNewerGenerationActivatesLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.wantCopies(map[string]string{convergenceSource: "active", convergenceHolder: "active"})

	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	continuationID := strandedContinuation(t, c, []string{convergenceHolder})
	t.Logf("continuation payload after ack: remaining=%v owed=%v",
		fanOutStoredPayload(t, c.db, continuationID)[supplychaincore.PayloadKeyFanOutRemaining],
		fanOutStoredPayload(t, c.db, continuationID)[supplychaincore.PayloadKeyFanOutOwed])

	c.raceRescan(convergenceSource, "g1", "g2")
	strandedDrain(t, c, strandedRuntime(t, c))

	if got := fanOutItem(t, c.ctx, c.db, continuationID); got.status != "succeeded" {
		t.Fatalf("continuation item status = %q, want succeeded: #10060 stranded it behind the newer generation (copies %v, handled %v)", got.status, c.copies(), c.handled)
	}
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestFanOutRacePartiallyDrainedContinuationSurvivesActivationLive
// is the stale-replay-after-partial-projection arm: the first continuation
// chunk already reopened one holder when the newer generation activates, so
// exactly the unreached remainder is at risk. The already-reopened holder
// converges either way; the remainder must still drain.
func TestFanOutRacePartiallyDrainedContinuationSurvivesActivationLive(t *testing.T) {
	c := newConvergence(t, false)
	const holder2 = "scope:9695:epss2"
	fanOutSeedScope(t, c.ctx, c.db, holder2, "g1")
	gen2 := fanOutGen(holder2, "g1")
	c.upsert(c.envelope(holder2, gen2, "fact:9695:epss2", chain.VulnerabilityEPSSScoreFactKind, map[string]any{
		"cve_id": convergenceCVE, "probability": "0.5", "percentile": "0.9", "score_date": "2026-10-31",
	}))
	fanOutSeedItemAt(t, c.ctx, c.db, holder2, gen2)
	c.drain()

	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	continuationID := strandedContinuation(t, c, []string{convergenceHolder, holder2})

	// First continuation chunk: reopen the first holder the way a capped
	// chunk does, carry only the second forward, and ack.
	chunk := c.raceClaim(convergenceSource)
	if outcome, err := c.fanOut.ReopenHolder(c.ctx, convergenceHolder, supplychaincore.FanOutOrigin{SourceScopeID: convergenceSource, SourceGenerationID: fanOutGen(convergenceSource, "g1"), SourceIntentID: chunk.IntentID}); err != nil || outcome != supplychaincore.FanOutReopened {
		t.Fatalf("chunk reopen = %q %v, want reopened", outcome, err)
	}
	if err := c.fanOut.UpdateFanOutState(c.ctx, chunk, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{holder2}}); err != nil {
		t.Fatalf("record partial continuation: %v", err)
	}
	if err := c.queue.Ack(c.ctx, chunk, reducercontract.Result{}); err != nil {
		t.Fatalf("ack chunk: %v", err)
	}

	c.raceRescan(convergenceSource, "g1", "g2")
	strandedDrain(t, c, strandedRuntime(t, c))

	if got := fanOutItem(t, c.ctx, c.db, continuationID); got.status != "succeeded" {
		t.Fatalf("partially drained continuation status = %q, want succeeded: #10060 stranded the remainder (copies %v, handled %v)", got.status, c.copies(), c.handled)
	}
	c.wantCopies(map[string]string{
		convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted", holder2: "advisory_retracted",
	})
}

// strandedActivate flips a scope to a newer active generation with plain SQL
// (no fact copy, no successor item), for queue-layer interleaves that only
// need the generation ordering the claim sweep keys off.
func strandedActivate(ctx context.Context, db *sql.DB, scope, from, to string) error {
	oldGen, newGen := fanOutGen(scope, from), fanOutGen(scope, to)
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`UPDATE scope_generations SET status='superseded', superseded_at=now() WHERE generation_id=$1`, []any{oldGen}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`, []any{newGen, scope}},
		{`UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, []any{scope, newGen}},
	} {
		if _, err := db.ExecContext(ctx, stmt.q, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}

// TestFanOutRaceSweepRechecksDebtCommittedWhileWaitingLive is the
// EvalPlanQual arm of the #10060 proof. The sweep's lock step carries the
// whole supersede predicate and locks candidate rows in work_item_id order
// without SKIP LOCKED: a claim whose snapshot predates a fan-out debt write
// blocks on that row's lock, and the waited lock must recheck the predicate
// on the newest row version. Row-set equivalence cannot show this — the
// debt exists only in the version committed while the claim waited.
func TestFanOutRaceSweepRechecksDebtCommittedWhileWaitingLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	const scope = "scope:stranded-epq"
	fanOutSeedScope(t, ctx, db, scope, "g1")
	id := fanOutSeedItem(t, ctx, db, scope, "g1")
	if err := strandedActivate(ctx, db, scope, "g1", "g2"); err != nil {
		t.Fatalf("activate g2: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `UPDATE fact_work_items SET payload = payload || $2::jsonb WHERE work_item_id = $1`,
		id, `{"`+supplychaincore.PayloadKeyFanOutOwed+`":true,"`+supplychaincore.PayloadKeyFanOutRemaining+`":["scope:holder-x"]}`); err != nil {
		t.Fatalf("write debt in held transaction: %v", err)
	}

	type claimResult struct {
		intent reducercontract.Intent
		ok     bool
		err    error
	}
	done := make(chan claimResult, 1)
	queue := fanOutLiveQueue(db)
	go func() {
		intent, ok, err := queue.Claim(ctx)
		done <- claimResult{intent, ok, err}
	}()
	select {
	case got := <-done:
		t.Fatalf("claim returned (%q, %v, %v) while the debt write held the row lock: the interleave never happened", got.intent.IntentID, got.ok, got.err)
	case <-time.After(500 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit debt write: %v", err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("claim error = %v", got.err)
		}
		if !got.ok || got.intent.IntentID != id {
			status := fanOutItem(t, ctx, db, id).status
			t.Fatalf("claim = (%q, %v), item status %q: debt committed while the sweep waited was not rechecked (#10060)", got.intent.IntentID, got.ok, status)
		}
		if err := queue.Ack(ctx, got.intent, reducercontract.Result{}); err != nil {
			t.Fatalf("ack rechecked claim: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("claim did not return within 15s after the debt write committed")
	}
}

// TestFanOutRaceDebtRetrySurvivesActivationLive
// proves the retry arm of the #10060 matrix on the full schema: a
// continuation attempt that fails transiently keeps its retrying state and
// failure evidence when a newer generation activates, instead of the claim
// sweep overwriting the row as superseded (which would erase both the debt
// and the reason it failed).
func TestFanOutRaceDebtRetrySurvivesActivationLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	const scope = "scope:stranded-retry"
	fanOutSeedScope(t, ctx, db, scope, "g1")
	id := fanOutSeedItem(t, ctx, db, scope, "g1")
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET payload = payload || $2::jsonb WHERE work_item_id = $1`,
		id, `{"`+supplychaincore.PayloadKeyFanOutOwed+`":true,"`+supplychaincore.PayloadKeyFanOutRemaining+`":["scope:holder-a"]}`); err != nil {
		t.Fatalf("seed debt payload: %v", err)
	}
	queue := fanOutLiveQueue(db)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok || intent.IntentID != id {
		t.Fatalf("first claim = %q %v %v, want the continuation claimed", intent.IntentID, ok, err)
	}
	if err := queue.Fail(ctx, intent, raceRetryableErr{}); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	readState := func() (string, string) {
		var status, class string
		if err := db.QueryRowContext(ctx, `SELECT status, COALESCE(failure_class,'') FROM fact_work_items WHERE work_item_id=$1`, id).Scan(&status, &class); err != nil {
			t.Fatalf("read item state: %v", err)
		}
		return status, class
	}
	if status, class := readState(); status != "retrying" || class != "fanout_race_injected_transient" {
		t.Fatalf("after Fail status=%q failure_class=%q, want retrying with the injected class", status, class)
	}
	if err := strandedActivate(ctx, db, scope, "g1", "g2"); err != nil {
		t.Fatalf("activate g2: %v", err)
	}
	// The next claim runs the supersede sweep. Whether it hands back the
	// retrying continuation itself or nothing (backoff), the row must not be
	// terminalized and its failure evidence must survive.
	if _, _, err := queue.Claim(ctx); err != nil {
		t.Fatalf("second Claim() error = %v", err)
	}
	status, class := readState()
	if status == "superseded" {
		t.Fatalf("retrying continuation was superseded across the activation (failure_class=%q): #10060", class)
	}
	if class != "fanout_race_injected_transient" {
		t.Fatalf("failure_class = %q after the sweep, want the transient class preserved on a %q row", class, status)
	}
}

// strandedRescan is raceRescan as an error-returning helper so the test
// goroutine can run it while workers drain: a new active generation holding
// a copy of the scope's non-finding facts, with a pending successor item.
// Unlike raceRescan (only ever called between drains) it runs in one
// transaction, like a projector Ack activation: concurrent passes must
// never see the active pointer flipped ahead of the generation's facts.
func strandedRescan(ctx context.Context, db *sql.DB, scope, from, to string) error {
	oldGen, newGen := fanOutGen(scope, from), fanOutGen(scope, to)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`UPDATE scope_generations SET status='superseded', superseded_at=now() WHERE generation_id=$1`, []any{oldGen}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`, []any{newGen, scope}},
		{`UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, []any{scope, newGen}},
		{`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT fact_id || ':' || $3::text, scope_id, $2, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, false, payload
FROM fact_records WHERE scope_id=$1 AND generation_id=$4 AND fact_kind <> 'reducer_supply_chain_impact_finding'`, []any{scope, newGen, to, oldGen}},
		{`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'scope', $2, 'pending', 0,
  jsonb_build_object('entity_key', $1::text, 'reason', 'fanout proof', 'fact_id', $1::text, 'source_system', 'vuln'),
  now() - interval '1 minute', now() - interval '1 minute')`, []any{fanOutItemID(scope, newGen), scope, newGen}},
	} {
		if _, err := tx.ExecContext(ctx, stmt.q, stmt.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TestFanOutRaceStrandedContinuationConcurrentClaimersConvergeLive interleaves the
// hazard with real concurrency: each round drains a pending continuation
// with four workers claiming through the production runtime on the same
// conflict domain (scope) while the newer generation activates mid-drain.
// Whichever worker wins each claim, and wherever the activation lands
// relative to the sweeps, the continuation must never be superseded with
// debt on it, no claim may deadlock (40P01), and the holder must converge.
// After quiescence, a debt-free duplicate delivery of the already-succeeded
// stale item must be terminalized by the sweep without executing again, and
// a final claim on the drained queue must report no work.
func TestFanOutRaceStrandedContinuationConcurrentClaimersConvergeLive(t *testing.T) {
	const rounds, workers = 3, 4
	for round := 1; round <= rounds; round++ {
		c := newConvergence(t, false)
		c.drain()
		c.setSourceStatus("Rejected")
		c.rerun(convergenceSource)
		continuationID := strandedContinuation(t, c, []string{convergenceHolder})
		rt := strandedRuntime(t, c)

		var inflight atomic.Int64
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		deadline := time.Now().Add(60 * time.Second)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				idle := time.Time{}
				for time.Now().Before(deadline) {
					intent, ok, err := c.queue.Claim(c.ctx)
					if err != nil {
						errs <- err
						return
					}
					if !ok {
						if inflight.Load() == 0 {
							if idle.IsZero() {
								idle = time.Now()
							} else if time.Since(idle) > 1500*time.Millisecond {
								return
							}
						}
						time.Sleep(20 * time.Millisecond)
						continue
					}
					idle = time.Time{}
					inflight.Add(1)
					result, herr := rt.Execute(c.ctx, intent)
					if herr != nil {
						if ferr := c.queue.Fail(c.ctx, intent, herr); ferr != nil {
							errs <- fmt.Errorf("fail: %v (execute %v)", ferr, herr)
						} else if _, uerr := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1 AND status = 'retrying'`, intent.IntentID); uerr != nil {
							errs <- fmt.Errorf("make retry visible: %w", uerr)
						}
					} else if aerr := c.queue.Ack(c.ctx, intent, result); aerr != nil {
						errs <- fmt.Errorf("ack: %w", aerr)
					}
					inflight.Add(-1)
				}
				errs <- fmt.Errorf("round %d did not quiesce within 60s", round)
			}()
		}
		time.Sleep(150 * time.Millisecond)
		if err := strandedRescan(c.ctx, c.db, convergenceSource, "g1", "g2"); err != nil {
			t.Fatalf("round %d rescan during drain: %v", round, err)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: %v", round, err)
		}

		if got := fanOutItem(t, c.ctx, c.db, continuationID); got.status != "succeeded" {
			t.Fatalf("round %d continuation status = %q, want succeeded (copies %v)", round, got.status, c.copies())
		}
		c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})

		// Empty-queue arm: nothing claimable remains.
		if _, ok, err := c.queue.Claim(c.ctx); err != nil || ok {
			t.Fatalf("round %d claim on drained queue = ok %v err %v, want no work", round, ok, err)
		}
		// Duplicate-delivery arm: the debt-free stale item re-pended must be
		// terminalized by the next claim sweep, not executed again.
		if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET status='pending', attempt_count=0, lease_owner=NULL, claim_until=NULL, visible_at=now(), next_attempt_at=NULL WHERE work_item_id=$1`, continuationID); err != nil {
			t.Fatalf("round %d re-pend duplicate: %v", round, err)
		}
		if _, ok, err := c.queue.Claim(c.ctx); err != nil || ok {
			t.Fatalf("round %d duplicate claim = ok %v err %v, want the sweep to retire it unclaimed", round, ok, err)
		}
		if got := fanOutItem(t, c.ctx, c.db, continuationID); got.status != "superseded" {
			t.Fatalf("round %d duplicate stale item status = %q, want superseded once its debt is gone", round, got.status)
		}
	}
}
