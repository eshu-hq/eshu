// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// Composition proofs for the cve_id suppression fan-out (#10019): the
// ruling-required pre-activation and coalesced-chain cases, and the >256
// holder continuation that survives a newer generation. The fixtures live
// in supply_chain_impact_fanout_suppression_cveid_live_test.go.

const (
	// supCVE2 is the second upstream CVE for the net-zero proof: its
	// distro record carries the distro id while only its upstream list
	// names the CVE, like supCVEID.
	supCVE2    = "CVE-2023-99999"
	supDistro2 = "DEBIAN-CVE-2023-99999"
	supAdv     = "scope:10019:adv"
)

// seedSupHolderSecondCVE adds the second distro record and affected package
// to the OS holder's generation, so it derives one finding per upstream CVE.
func seedSupHolderSecondCVE(t *testing.T, c *convergence) {
	t.Helper()
	gen := fanOutGen(supHolder, "g1")
	c.upsert(
		c.envelope(supHolder, gen, "fact:10019:record2", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": supDistro2, "cve_id": supDistro2, "source": "osv",
			"upstream":     []string{supCVE2},
			"published_at": "2023-09-15T04:15:10Z",
		}),
		c.envelope(supHolder, gen, "fact:10019:affected2", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": supDistro2, "cve_id": supDistro2,
			"package_id": "os://debian/curl", "package_name": "curl", "ecosystem": "os",
			"purl":           "pkg:deb/debian/curl?arch=source&distro=bookworm",
			"fixed_versions": []string{"7.88.1-10+deb12u4"},
			"affected_ranges": []any{map[string]any{
				"events":   []any{map[string]any{"introduced": "0"}, map[string]any{"fixed": "7.88.1-10+deb12u4"}},
				"releases": []string{"debian:12"}, "type": "ECOSYSTEM",
			}},
			"source": "osv",
		}),
	)
}

// supStatesByCVE returns the holder scope's active finding state per CVE.
func supStatesByCVE(t *testing.T, c *convergence, holder string, cves ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, cve := range cves {
		var state string
		if err := c.db.QueryRowContext(c.ctx, `
SELECT COALESCE(f.payload->>'suppression_state','') FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1 AND f.payload->>'cve_id' = $2`, holder, cve).Scan(&state); err != nil {
			t.Fatalf("read %s state: %v", cve, err)
		}
		out[cve] = state
	}
	return out
}

func supAdmissionCount(t *testing.T, c *convergence, scope, gen string) int {
	t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `SELECT count(*) FROM supply_chain_impact_write_admission WHERE scope_id = $1 AND generation_id = $2`, scope, gen).Scan(&n); err != nil {
		t.Fatalf("count admissions: %v", err)
	}
	return n
}

// TestSuppressionFanOutPreActivationDefersLive is the ruling-required
// pre-activation proof: executing the claim on the pending operator
// generation, before first activation, fails in the runtime guard with the
// non-counting deferral (no handler, no admission, no reopened holder) and
// leaves the item retrying with a frozen attempt count. After activation
// the same item runs exactly once and the fan-out carries the suppression
// ids.
func TestSuppressionFanOutPreActivationDefersLive(t *testing.T) {
	c := newConvergence(t, false)
	seedSupScanScope(t, c)
	seedSupHolder(t, c)
	c.drain()

	gen := seedSupOperatorMutation(t, c, "suppression-10019-pre", supCVEID, "compensating control")
	itemID := fanOutSeedItemAt(t, c.ctx, c.db, supOpScope, gen)
	rt := strandedRuntime(t, c)

	intent, ok, claimErr := c.queue.Claim(c.ctx)
	if claimErr != nil || !ok {
		t.Fatalf("claim pending operator item = %v %v", ok, claimErr)
	}
	_, execErr := rt.Execute(c.ctx, intent)
	if !isSupNotYetActive(t, execErr) {
		t.Fatalf("Execute() error = %v, want the non-counting generation deferral", execErr)
	}
	if failErr := c.queue.Fail(c.ctx, intent, execErr); failErr != nil {
		t.Fatalf("fail deferred item: %v", failErr)
	}
	if got := supAdmissionCount(t, c, supOpScope, gen); got != 0 {
		t.Fatalf("admission rows = %d before activation, want none", got)
	}
	if got := fanOutItem(t, c.ctx, c.db, itemID); got.status != "retrying" || got.attempts != 1 {
		t.Fatalf("operator item = %+v, want retrying with a frozen attempt count", got)
	}
	if got := supCopies(t, c)[supHolder]; got != string(supplychaincore.SupplyChainSuppressionStateActive) {
		t.Fatalf("holder state before activation = %q, want active", got)
	}

	activateAdvisorySuppressionGeneration(t, c.ctx, c.db, gen)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1 AND status = 'retrying'`, itemID); err != nil {
		t.Fatalf("make deferred item visible: %v", err)
	}
	strandedDrain(t, c, rt)

	operatorRuns := 0
	for _, h := range c.handled {
		if len(h) >= len(supOpScope) && h[:len(supOpScope)] == supOpScope {
			operatorRuns++
		}
	}
	if operatorRuns != 1 {
		t.Fatalf("operator runs after activation = %d, want exactly one (handled %v)", operatorRuns, c.handled)
	}
	if got := supCopies(t, c)[supHolder]; got != "accepted_risk" {
		t.Fatalf("holder state after activation = %q, want accepted_risk", got)
	}
	if got := fanOutItem(t, c.ctx, c.db, itemID); got.status != "succeeded" {
		t.Fatalf("operator item = %+v, want succeeded", got)
	}
}

// isSupNotYetActive reports whether the runtime refused the claim in the
// generation freshness guard, before any handler ran.
func isSupNotYetActive(t *testing.T, err error) bool {
	t.Helper()
	if err == nil {
		return false
	}
	var notActive reducercontract.GenerationNotYetActiveError
	return errors.As(err, &notActive)
}

// supActivateOperatorGeneration activates the operator generation the way a
// published rescan does: the previous active generation is superseded first,
// so the scope never holds two active generations.
func supActivateOperatorGeneration(t *testing.T, c *convergence, gen string) {
	t.Helper()
	if _, err := c.db.ExecContext(c.ctx, `UPDATE scope_generations SET status='superseded', superseded_at=now() WHERE scope_id = $1 AND status = 'active' AND generation_id != $2`, supOpScope, gen); err != nil {
		t.Fatalf("supersede previous operator generation: %v", err)
	}
	activateAdvisorySuppressionGeneration(t, c.ctx, c.db, gen)
}

// TestSuppressionFanOutCoalescedChainReopensBothHoldersLive is the
// ruling-required coalesced-chain and net-zero proof. Generations A (S1
// covers CVE-1), B (S1 re-scoped to CVE-2), and C (S1 back to CVE-1) form a
// chain whose endpoint diff is empty: A and C carry identical facts. B's
// item is superseded before it runs, so C's pass must walk the pairs and
// reopen both holders — CVE-1's, whose suppression B removed, and CVE-2's,
// whose suppression B added. The holder also runs while B is active, so it
// reads the mid-chain state first and the pass on C must move it back.
func TestSuppressionFanOutCoalescedChainReopensBothHoldersLive(t *testing.T) {
	c := newConvergence(t, false)
	seedSupScanScope(t, c)
	seedSupHolder(t, c)
	seedSupHolderSecondCVE(t, c)
	c.drain()
	states := supStatesByCVE(t, c, supHolder, supCVEID, supCVE2)
	if states[supCVEID] != "active" || states[supCVE2] != "active" {
		t.Fatalf("holder states before the chain = %v, want both active", states)
	}

	const sid = "suppression-10019-coalesced"
	genA := seedSupOperatorMutation(t, c, sid, supCVEID, "compensating control")
	supActivateOperatorGeneration(t, c, genA)
	fanOutSeedItemAt(t, c.ctx, c.db, supOpScope, genA)
	c.drain()
	states = supStatesByCVE(t, c, supHolder, supCVEID, supCVE2)
	if states[supCVEID] != "accepted_risk" || states[supCVE2] != "active" {
		t.Fatalf("holder states after A = %v, want CVE-1 suppressed and CVE-2 active", states)
	}

	genB := seedSupOperatorMutation(t, c, sid, supCVE2, "compensating control")
	if genB == genA {
		t.Fatal("the re-scope edit did not mint a generation: the chain needs A, B, and C")
	}
	supActivateOperatorGeneration(t, c, genB)
	// The holder runs while B is the active generation but B's reducer item
	// does not exist yet, so the drain below runs only the holder. Seeding
	// the item after this run keeps the choreography honest: B's item is
	// pending on a superseded generation with no debt when C activates, and
	// the sweep outcome is identical either way.
	c.rerun(supHolder)
	c.drain()
	states = supStatesByCVE(t, c, supHolder, supCVEID, supCVE2)
	if states[supCVEID] != "active" || states[supCVE2] != "accepted_risk" {
		t.Fatalf("holder states during B = %v, want the mid-chain flip", states)
	}

	itemB := fanOutSeedItemAt(t, c.ctx, c.db, supOpScope, genB)
	genC := seedSupOperatorMutation(t, c, sid, supCVEID, "compensating control")
	if genC == genB {
		t.Fatal("the re-scope edit did not mint a generation: the chain needs A, B, and C")
	}
	supActivateOperatorGeneration(t, c, genC)
	fanOutSeedItemAt(t, c.ctx, c.db, supOpScope, genC)

	intent, ok, err := c.queue.Claim(c.ctx)
	if err != nil || !ok {
		t.Fatalf("claim after C activates = %v %v, want the C pass", ok, err)
	}
	if intent.GenerationID != genC {
		t.Fatalf("claimed generation = %s, want C: the sweep must supersede B's item", intent.GenerationID)
	}
	if got := fanOutItem(t, c.ctx, c.db, itemB); got.status != "superseded" {
		t.Fatalf("B's item = %+v, want superseded without ever running", got)
	}
	result, err := c.handler.Handle(c.ctx, intent)
	if err != nil {
		t.Fatalf("Handle(C) error = %v", err)
	}
	if got := result.SubSignals["suppression_diff_cve_ids"]; got != 2 {
		t.Fatalf("suppression_diff_cve_ids = %v, want 2: the walk must span the owed range", got)
	}
	if got := result.SubSignals["fanout_holders"]; got != 1 {
		t.Fatalf("fanout_holders = %v, want the one holder scope", got)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		t.Fatalf("ack C: %v", err)
	}
	c.drain()
	states = supStatesByCVE(t, c, supHolder, supCVEID, supCVE2)
	if states[supCVEID] != "accepted_risk" || states[supCVE2] != "active" {
		t.Fatalf("holder states after C = %v, want CVE-1 suppressed and CVE-2 active again", states)
	}
}

// The criterion-5 mass proof: more holders than the per-claim cap, a newer
// operator generation before the continuation is claimed, and a concurrent
// drain. Suppression ids are the only fan-out source here, so every reopen
// below comes from the diff.

const supMassHolders = 260

func supMassScope(i int) string { return fmt.Sprintf("scope:10019:m%d", i) }

// seedSupAdvisoryRecord holds the distro record no holder owns: every mass
// holder loads it cross-scope by advisory id.
func seedSupAdvisoryRecord(t *testing.T, c *convergence) {
	t.Helper()
	fanOutSeedScope(t, c.ctx, c.db, supAdv, "g1")
	c.upsert(
		c.envelope(supAdv, fanOutGen(supAdv, "g1"), "fact:10019:adv-record", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": supDistroID, "cve_id": supDistroID, "source": "osv",
			"upstream":     []string{supCVEID},
			"published_at": "2023-09-15T04:15:10Z",
		}),
	)
}

// seedSupMassHolder gives one holder its own affected package (the filter
// bootstrap), its queue item, and nothing else: the distro record and the
// installed curl row arrive cross-scope.
func seedSupMassHolder(t *testing.T, c *convergence, i int) {
	t.Helper()
	scope := supMassScope(i)
	fanOutSeedScope(t, c.ctx, c.db, scope, "g1")
	c.upsert(
		c.envelope(scope, fanOutGen(scope, "g1"), fmt.Sprintf("fact:10019:m%d:affected", i), chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": supDistroID, "cve_id": supDistroID,
			"package_id": "os://debian/curl", "package_name": "curl", "ecosystem": "os",
			"purl":           "pkg:deb/debian/curl?arch=source&distro=bookworm",
			"fixed_versions": []string{supCurlFix},
			"affected_ranges": []any{map[string]any{
				"events":   []any{map[string]any{"introduced": "0"}, map[string]any{"fixed": supCurlFix}},
				"releases": []string{"debian:12"}, "type": "ECOSYSTEM",
			}},
			"source": "osv",
		}),
	)
	fanOutSeedItem(t, c.ctx, c.db, scope, "g1")
}

// supDrainN drains like convergence.drain with a higher claim cap, for
// holder counts past the 60-claim loop.
func supDrainN(t *testing.T, c *convergence, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			return
		}
		c.handled = append(c.handled, fmt.Sprintf("%s#%d", intent.ScopeID, intent.AttemptCount))
		result, err := c.handler.Handle(c.ctx, intent)
		if err != nil {
			t.Fatalf("handle %s: %v", intent.ScopeID, err)
		}
		if err := c.queue.Ack(c.ctx, intent, result); err != nil {
			t.Fatalf("ack %s: %v", intent.ScopeID, err)
		}
	}
	t.Fatalf("queue still had work after %d claims; handled %d", limit, len(c.handled))
}

// supConcurrentDrain drains with four workers through the production runtime
// (the stranded guard included), so the debt spare and the advisory locks
// are the only coordination: no serialization step. It returns the wall
// time. It mirrors the stranded race drain: idle workers past the
// quiescence window exit, any worker error fails the test.
func supConcurrentDrain(t *testing.T, c *convergence, rt *reducer.Runtime, workers int) time.Duration {
	t.Helper()
	started := time.Now()
	var inflight atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	deadline := time.Now().Add(120 * time.Second)
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
			errs <- fmt.Errorf("did not quiesce within 120s")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent drain: %v", err)
	}
	return time.Since(started)
}

func supCountScopeRows(t *testing.T, c *convergence, like, cve, state string) int {
	t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id LIKE $1 AND f.payload->>'cve_id' = $2 AND COALESCE(f.payload->>'suppression_state','') = $3`, like, cve, state).Scan(&n); err != nil {
		t.Fatalf("count finding rows: %v", err)
	}
	return n
}

// TestSuppressionFanOutCappedContinuationSurvivesNewerGenerationLive is the
// #10019 criterion-5 proof: 260 OS holders (past the 256 per-claim cap) read
// active, the operator's suppression-only pass names CVE-1 through the diff
// and reopens 256 with 4 left behind, a newer operator generation activates
// before the continuation is claimed, and a 4-worker drain through the
// production runtime (debt spare, advisory locks, no serialization) brings
// every holder to accepted_risk. The representative cost is logged, not
// asserted: wall time needs a quiet host.
func TestSuppressionFanOutCappedContinuationSurvivesNewerGenerationLive(t *testing.T) {
	c := newConvergence(t, false)
	seedSupScanScope(t, c)
	seedSupAdvisoryRecord(t, c)
	for i := 0; i < supMassHolders; i++ {
		seedSupMassHolder(t, c, i)
	}
	firstStart := time.Now()
	supDrainN(t, c, 2000)
	firstDrain := time.Since(firstStart)
	if got := supCountScopeRows(t, c, `scope:10019:m%`, supCVEID, "active"); got != supMassHolders {
		t.Fatalf("active holder rows = %d, want %d", got, supMassHolders)
	}

	seedSupOperatorCVEID(t, c)
	intent, ok, err := c.queue.Claim(c.ctx)
	if err != nil || !ok {
		t.Fatalf("claim operator pass = %v %v, want the suppression-only pass", ok, err)
	}
	result, err := c.handler.Handle(c.ctx, intent)
	if err != nil {
		t.Fatalf("Handle(gen1) error = %v", err)
	}
	if got := result.SubSignals["suppression_diff_cve_ids"]; got != 1 {
		t.Fatalf("suppression_diff_cve_ids = %v, want 1: the diff must name the suppression CVE", got)
	}
	if got := result.SubSignals["fanout_holders"]; got != supMassHolders {
		t.Fatalf("fanout_holders = %v, want %d", got, supMassHolders)
	}
	if got := result.SubSignals["fanout_reopened"]; got != 256 {
		t.Fatalf("fanout_reopened = %v, want the 256 per-claim cap", got)
	}
	if got := result.SubSignals["fanout_deferred"]; got != supMassHolders-256 {
		t.Fatalf("fanout_deferred = %v, want the %d holders past the cap", got, supMassHolders-256)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		t.Fatalf("ack gen1: %v", err)
	}
	var owed string
	var remaining int
	if err := c.db.QueryRowContext(c.ctx, `SELECT COALESCE(payload->>'fanout_owed',''), COALESCE(jsonb_array_length(payload->'fanout_remaining'),0) FROM fact_work_items WHERE work_item_id = $1`, intent.IntentID).Scan(&owed, &remaining); err != nil {
		t.Fatalf("read continuation: %v", err)
	}
	if owed != "true" || remaining != supMassHolders-256 {
		t.Fatalf("continuation owed=%q remaining=%d, want true/%d", owed, remaining, supMassHolders-256)
	}

	gen2 := seedSupOperatorMutation(t, c, "suppression-10019-cveid", supCVEID, "control revalidated")
	supActivateOperatorGeneration(t, c, gen2)
	gen2Item := fanOutSeedItemAt(t, c.ctx, c.db, supOpScope, gen2)

	rt := strandedRuntime(t, c)
	concurrentDrain := supConcurrentDrain(t, c, rt, 4)

	var succeeded int
	if err := c.db.QueryRowContext(c.ctx, `SELECT count(*) FROM fact_work_items WHERE scope_id LIKE 'scope:10019:m%' AND status = 'succeeded'`).Scan(&succeeded); err != nil {
		t.Fatalf("count holder items: %v", err)
	}
	if succeeded != supMassHolders {
		t.Fatalf("succeeded holder items = %d, want %d", succeeded, supMassHolders)
	}
	if got := fanOutItem(t, c.ctx, c.db, intent.IntentID); got.status != "succeeded" {
		t.Fatalf("gen1 continuation item = %+v, want succeeded", got)
	}
	if got := fanOutItem(t, c.ctx, c.db, gen2Item); got.status != "succeeded" {
		t.Fatalf("gen2 item = %+v, want succeeded", got)
	}
	if got := supCountScopeRows(t, c, `scope:10019:m%`, supCVEID, "accepted_risk"); got != supMassHolders {
		t.Fatalf("accepted_risk holder rows = %d, want %d", got, supMassHolders)
	}
	if _, ok, err := c.queue.Claim(c.ctx); err != nil || ok {
		t.Fatalf("claim on drained queue = ok %v err %v, want no work", ok, err)
	}
	t.Logf("suppression fan-out at %d holders: first drain %v, concurrent drain %v (4 workers), reopened 256 + deferred %d",
		supMassHolders, firstDrain.Round(time.Second), concurrentDrain.Round(time.Second), supMassHolders-256)
}
