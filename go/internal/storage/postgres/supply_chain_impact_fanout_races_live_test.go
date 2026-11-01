// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// raceRetryableErr is any transient, counting, retryable pass failure.
type raceRetryableErr struct{}

func (raceRetryableErr) Error() string        { return "injected transient failure" }
func (raceRetryableErr) Retryable() bool      { return true }
func (raceRetryableErr) FailureClass() string { return "fanout_race_injected_transient" }

func (c *convergence) raceClaim(wantScope string) reducercontract.Intent {
	c.t.Helper()
	intent, ok, err := c.queue.Claim(c.ctx)
	if err != nil || !ok {
		c.t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if wantScope != "" && intent.ScopeID != wantScope {
		c.t.Fatalf("claimed %s, want %s", intent.ScopeID, wantScope)
	}
	return intent
}

// raceRescan gives scope a new active generation holding a copy of its non-finding
// facts, with a pending item, like a collector rescan.
func (c *convergence) raceRescan(scope, from, to string) {
	c.t.Helper()
	oldGen, newGen := fanOutGen(scope, from), fanOutGen(scope, to)
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
	} {
		if _, err := c.db.ExecContext(c.ctx, stmt.q, stmt.args...); err != nil {
			c.t.Fatalf("rescan %s: %v", scope, err)
		}
	}
	fanOutSeedItemAt(c.t, c.ctx, c.db, scope, newGen)
}

// The stamp lands on a RUNNING genuine pass (race 1 of the 1.5b review).
// R2: a genuine source pass is stamped by another scope's fan-out while it runs,
// then fails transiently before it commits. Its retry carries the stamp, so it
// is treated as a one-hop pass and never fans out its genuine change.
func TestFanOutRaceStampOnRunningPassKeepsItsRetryFanOutLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	intent := c.raceClaim(convergenceSource)
	outcome, err := c.fanOut.ReopenHolder(c.ctx, convergenceSource, supplychaincore.FanOutOrigin{SourceScopeID: convergenceHolder, SourceGenerationID: "g", SourceIntentID: "i"})
	if err != nil || outcome != supplychaincore.FanOutReplayFlagged {
		t.Fatalf("concurrent reopen of the running source = %q %v", outcome, err)
	}
	if err := c.queue.Fail(c.ctx, intent, raceRetryableErr{}); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at=now()-interval '1 second', next_attempt_at=NULL WHERE work_item_id=$1`, intent.IntentID); err != nil {
		t.Fatal(err)
	}
	c.drain()
	t.Logf("handled=%v copies=%v", c.handled, c.copies())
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// R1: the committing item carries a continuation list and is stamped by another
// scope's fan-out during a drain claim. The next claim honors the stamp and its
// stamp-consuming update also deletes fanout_remaining, so the owed holders are
// never reopened.
func TestFanOutRaceStampDuringDrainKeepsTheContinuationLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	// Claim 1: commit the retraction; its fan-out deferred two holders (emulated: the
	// cap is a package constant).
	noFanOut := c.handler
	noFanOut.FanOut = nil
	claim1 := c.raceClaim(convergenceSource)
	result, err := noFanOut.Handle(c.ctx, claim1)
	if err != nil {
		t.Fatalf("handle claim1: %v", err)
	}
	if err := c.fanOut.UpdateFanOutState(c.ctx, claim1, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{"scope:9695:aaa", convergenceHolder}}); err != nil {
		t.Fatal(err)
	}
	if err := c.queue.Ack(c.ctx, claim1, result); err != nil {
		t.Fatal(err)
	}
	// Claim 2 drains the first entry; meanwhile another scope's fan-out stamps it.
	claim2 := c.raceClaim(convergenceSource)
	if out, err := c.fanOut.ReopenHolder(c.ctx, convergenceSource, supplychaincore.FanOutOrigin{SourceScopeID: "scope:other"}); err != nil || out != supplychaincore.FanOutReplayFlagged {
		t.Fatalf("stamp during drain = %q %v", out, err)
	}
	if err := c.fanOut.UpdateFanOutState(c.ctx, claim2, supplychaincore.FanOutState{ReplaceRemaining: true, Remaining: []string{convergenceHolder}}); err != nil {
		t.Fatal(err)
	}
	if err := c.queue.Ack(c.ctx, claim2, result); err != nil {
		t.Fatal(err)
	}
	payload := fanOutStoredPayload(t, c.db, claim1.IntentID)
	t.Logf("payload before claim 3: origin=%v remaining=%v", payload[supplychaincore.PayloadKeyFanOutOrigin], payload[supplychaincore.PayloadKeyFanOutRemaining])
	c.drain() // claim 3 is the real handler
	payload = fanOutStoredPayload(t, c.db, claim1.IntentID)
	t.Logf("handled=%v copies=%v remaining after=%v", c.handled, c.copies(), payload[supplychaincore.PayloadKeyFanOutRemaining])
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

type raceGateWriter struct {
	inner   supplychaincore.SupplyChainImpactWriter
	loaded  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *raceGateWriter) WriteSupplyChainImpactFindings(ctx context.Context, w supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	g.once.Do(func() { close(g.loaded); <-g.release })
	return g.inner.WriteSupplyChainImpactFindings(ctx, w)
}

// R3: the holder rescans; its new generation is active and its first pass has
// loaded evidence but not committed when the source commits a rejection. The
// holder read joins the active generation, which has no rows yet, so the holder
// is not reopened and its pass commits the pre-rejection copy.
func TestFanOutRaceInFlightFirstPassOfNewGenerationIsFlaggedLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.raceRescan(convergenceHolder, "g1", "g2")
	holderIntent := c.raceClaim(convergenceHolder)
	gate := &raceGateWriter{inner: c.handler.Writer, loaded: make(chan struct{}), release: make(chan struct{})}
	holderHandler := c.handler
	holderHandler.Writer = gate
	done := make(chan error, 1)
	go func() {
		result, err := holderHandler.Handle(c.ctx, holderIntent)
		if err != nil {
			done <- err
			return
		}
		done <- c.queue.Ack(c.ctx, holderIntent, result)
	}()
	<-gate.loaded
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	src := c.raceClaim(convergenceSource)
	result, err := c.handler.Handle(c.ctx, src)
	if err != nil {
		t.Fatalf("source handle: %v", err)
	}
	if err := c.queue.Ack(c.ctx, src, result); err != nil {
		t.Fatal(err)
	}
	st := fanOutItem(t, c.ctx, c.db, holderIntent.IntentID)
	t.Logf("holder item while in flight after source fan-out: %+v; source subsignals fanout_holders=%v", st, result.SubSignals["fanout_holders"])
	close(gate.release)
	if err := <-done; err != nil {
		t.Fatalf("holder pass: %v", err)
	}
	c.drain()
	t.Logf("handled=%v copies=%v", c.handled, c.copies())
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// S: concurrent workers, a toggling source, and holders rescanning in the same
// round. Every round must quiesce (no deadlock) and converge.
func TestFanOutRaceConcurrentStressLive(t *testing.T) {
	c := newConvergence(t, false)
	holders := []string{convergenceHolder}
	for i := 0; i < 8; i++ {
		scope := fmt.Sprintf("scope:9695:epss%d", i)
		fanOutSeedScope(t, c.ctx, c.db, scope, "r0")
		gen := fanOutGen(scope, "r0")
		c.upsert(c.envelope(scope, gen, "fact:9695:epss:"+scope, "vulnerability.epss_score", map[string]any{
			"cve_id": convergenceCVE, "probability": "0.5", "percentile": "0.9", "score_date": "2026-10-31",
		}))
		fanOutSeedItemAt(t, c.ctx, c.db, scope, gen)
		holders = append(holders, scope)
	}
	c.drain()
	gens := map[string]string{convergenceHolder: "g1"}
	for _, h := range holders[1:] {
		gens[h] = "r0"
	}
	const workers, rounds = 10, 16
	var failures []string
	for round := 1; round <= rounds; round++ {
		status, want := "Rejected", "advisory_retracted"
		if round%2 == 0 {
			status, want = "Analyzed", "active"
		}
		// half the holders rescan in the same round as the source change
		for i, h := range holders {
			if (i+round)%2 == 0 {
				next := fmt.Sprintf("r%d", round)
				c.raceRescan(h, gens[h], next)
				gens[h] = next
			}
		}
		c.setSourceStatus(status)
		c.rerun(convergenceSource)
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
					result, herr := c.handler.Handle(c.ctx, intent)
					if herr != nil {
						if ferr := c.queue.Fail(c.ctx, intent, herr); ferr != nil {
							errs <- fmt.Errorf("fail: %v (handle %v)", ferr, herr)
						}
					} else if aerr := c.queue.Ack(c.ctx, intent, result); aerr != nil {
						errs <- fmt.Errorf("ack: %w", aerr)
					}
					inflight.Add(-1)
				}
				errs <- fmt.Errorf("round %d did not quiesce within 60s", round)
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: %v", round, err)
		}
		got := c.copies()
		for _, scope := range append([]string{convergenceSource}, holders...) {
			if got[scope] != want {
				failures = append(failures, fmt.Sprintf("round %d (%s): %s=%q", round, status, scope, got[scope]))
			}
		}
	}
	if len(failures) > 0 {
		t.Fatalf("non-converged copies after quiescence: %v", failures)
	}
}
