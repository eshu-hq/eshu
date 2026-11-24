// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The faults, the duplicate delivery and the write-stop assertions of the impact family (#10880 PR-C2; arbiter-impact-
// ifa section 2). They run on the PR-C2 head only: the per-row fencing guards were removed there, and these proofs replace
// them. Every cell runs against an ARMED database, so a pass that wrote a finding fact would fail with SQLSTATE 55000.
//
// The faults use the REAL queue with a short REAL lease and real elapsed time. They do not inject the queue's clock: the
// claim writes claim_until from the Go clock while the owed marker's lease fence compares it with the database clock, so a
// fake clock would create a lease state production cannot have.

// familyShortLease is the lease of the fault cells. It is long enough for a pass of the family's cassettes (a few
// milliseconds of handler work against a small database) to finish well inside it, and short enough that a dropped claim
// is reclaimed within a few seconds. Shape: the three supply-chain-demo cassettes; the QA-ratio fixture run uses a lease
// longer than its passes instead.
const familyShortLease = 2 * time.Second

// requireArmedAndNoFindingFacts is the write-stop assertion every cell ends with: the trigger still reads enabled and no
// finding fact exists in fact_records.
func requireArmedAndNoFindingFacts(t *testing.T, f *impactFamily) {
	t.Helper()
	if state := writeStopTriggerState(t, f.ctx, f.db); state != "O" {
		t.Fatalf("the reject trigger reads %q after the run, want O (enabled)", state)
	}
	if n := writeStopFactCount(t, f.ctx, f.db, writeStopKind); n != 0 {
		t.Fatalf("%d finding facts exist in fact_records after the run, want 0: a pass wrote a finding fact", n)
	}
}

// parkingIssuer returns the issued token, then holds the pass until released: the pass has its token and has loaded and
// written nothing.
type parkingIssuer struct {
	inner   supplychaincore.SupplyChainImpactFencingTokenIssuer
	once    sync.Once
	parked  chan struct{}
	release chan struct{}
}

func newParkingIssuer() *parkingIssuer {
	return &parkingIssuer{parked: make(chan struct{}), release: make(chan struct{})}
}

func (p *parkingIssuer) wrap(inner supplychaincore.SupplyChainImpactFencingTokenIssuer) supplychaincore.SupplyChainImpactFencingTokenIssuer {
	p.inner = inner
	return p
}

func (p *parkingIssuer) NextSupplyChainImpactFencingToken(ctx context.Context) (int64, error) {
	token, err := p.inner.NextSupplyChainImpactFencingToken(ctx)
	if err != nil {
		return 0, err
	}
	p.once.Do(func() {
		close(p.parked)
		<-p.release
	})
	return token, nil
}

// TestSupplyChainImpactSupportsFamilyKillAfterClaimLive is fault one: a worker really claims an impact row and is gone (it
// never executes, acks or heartbeats). The real lease expires, another worker reclaims the row and finishes it. The digest
// equals the fault-free baseline, no row is dead-lettered, the dropped intent ran exactly once (the killed worker never
// executed it) and so did every other, and the write stop held.
func TestSupplyChainImpactSupportsFamilyKillAfterClaimLive(t *testing.T) {
	baseline, _ := familyBaseline(t)
	f := newImpactFamily(t)

	killed := f.queue("family-killed", familyShortLease)
	intent, claimed, err := killed.Claim(f.ctx)
	if err != nil || !claimed {
		t.Fatalf("the killed worker's claim = %v, %v, want a claimed intent", claimed, err)
	}

	f.drain(2, familyShortLease, nil)
	f.resweep()
	f.requireFaultFree()
	if got := f.digest(); got != baseline {
		t.Fatalf("the digest after a kill-after-claim fault is %s, want the fault-free baseline %s", got, baseline)
	}
	f.mu.Lock()
	runs := f.executions[intent.IntentID]
	f.mu.Unlock()
	if runs != 1 {
		t.Fatalf("the dropped intent %s was executed %d times, want exactly 1 (by the worker that reclaimed it)", intent.IntentID, runs)
	}
	requireArmedAndNoFindingFacts(t, f)
}

// TestSupplyChainImpactSupportsFamilyLeaseExpiresMidHandlerLive is fault two: a worker holds a pass after its fencing token
// is issued and before it loads or writes anything, its real lease expires, another worker reclaims the same row and
// finishes it, and then the stale pass is released. The stale pass is rejected by the admission row (its token is the older
// of the two) and changes zero rows, asserted by row version, not by outcome: the transaction id of every supports, winners
// and retracted-keys row is the same before and after. The faulted intent ran exactly twice, every other once, and the
// digest equals the baseline. This replaces the per-row fencing guards PR-C2 removed.
func TestSupplyChainImpactSupportsFamilyLeaseExpiresMidHandlerLive(t *testing.T) {
	baseline, _ := familyBaseline(t)
	f := newImpactFamily(t)

	parker := newParkingIssuer()
	stale := f.newWorker("family-stale", familyShortLease, parker.wrap)
	type outcome struct {
		intentID string
		err      error
	}
	finished := make(chan outcome, 1)
	go func() {
		intent, claimed, err := stale.queue.Claim(f.ctx)
		if err != nil || !claimed {
			finished <- outcome{err: errors.Join(errors.New("the stale worker claimed nothing"), err)}
			return
		}
		finished <- outcome{intentID: intent.IntentID, err: stale.execute(intent)}
	}()
	select {
	case <-parker.parked:
	case <-time.After(30 * time.Second):
		t.Fatal("the stale worker never reached its fencing token")
	}

	// Another worker finishes everything, including the stale worker's row once its real lease has expired.
	f.drain(2, familyShortLease, nil)
	f.resweep()
	before := f.derivedRowVersions()
	digestBefore := f.digest()

	close(parker.release)
	var out outcome
	select {
	case out = <-finished:
	case <-time.After(60 * time.Second):
		t.Fatal("the released stale pass never returned")
	}
	if out.intentID == "" {
		t.Fatalf("the stale worker did not run: %v", out.err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(out.err, &classified) || classified.FailureClass() != supplychaincore.SupplyChainImpactWriteSupersededFailureClass {
		t.Fatalf("the stale pass returned %v, want a %s refusal from the admission row", out.err, supplychaincore.SupplyChainImpactWriteSupersededFailureClass)
	}
	if differing := rowVersionsEqual(before, f.derivedRowVersions()); len(differing) != 0 {
		t.Fatalf("the stale pass wrote row versions in %v, want none: it must change zero rows", differing)
	}
	f.resweep()
	if got := f.digest(); got != digestBefore || got != baseline {
		t.Fatalf("the digest after the stale pass is %s (before it %s), want the baseline %s", got, digestBefore, baseline)
	}
	if n := f.deadLetters(); n != 0 {
		t.Fatalf("%d impact work items are dead-lettered, want 0", n)
	}
	f.mu.Lock()
	staleRuns := f.executions[out.intentID]
	extra := 0
	for id, n := range f.executions {
		if id != out.intentID && n != 1 {
			extra++
		}
	}
	f.mu.Unlock()
	if staleRuns != 2 || extra != 0 {
		t.Fatalf("the faulted intent ran %d times (want 2: stale and fresh) and %d other intents did not run exactly once", staleRuns, extra)
	}
	requireArmedAndNoFindingFacts(t, f)
}

// TestSupplyChainImpactSupportsFamilyDuplicateDeliveryWritesNoRowVersionLive redelivers an intent that already succeeded: the
// same intent value runs through the handler again. The supports, the winners and the retracted keys are not rewritten (the
// write-on-change guard and the retraction's empty result leave every row version in place) and the digest is unchanged.
func TestSupplyChainImpactSupportsFamilyDuplicateDeliveryWritesNoRowVersionLive(t *testing.T) {
	f := newImpactFamily(t)
	f.drain(1, time.Minute, nil)
	f.resweep()
	f.requireFaultFree()
	digest := f.digest()
	before := f.derivedRowVersions()

	f.mu.Lock()
	ids := make([]string, 0, len(f.claimed))
	for id := range f.claimed {
		ids = append(ids, id)
	}
	f.mu.Unlock()
	sort.Strings(ids)
	redelivered := f.newWorker("family-redelivery", time.Minute, nil)
	for _, id := range ids {
		f.mu.Lock()
		intent := f.claimed[id]
		f.mu.Unlock()
		if _, err := redelivered.handler.Handle(f.ctx, intent); err != nil {
			var classified interface{ Retryable() bool }
			if !errors.As(err, &classified) {
				t.Fatalf("redelivery of %s failed with a non-retryable error: %v", id, err)
			}
		}
	}
	if differing := rowVersionsEqual(before, f.derivedRowVersions()); len(differing) != 0 {
		t.Fatalf("a redelivered, already succeeded intent wrote row versions in %v, want none", differing)
	}
	f.resweep()
	if got := f.digest(); got != digest {
		t.Fatalf("the digest changed from %s to %s after redelivery", digest, got)
	}
	requireArmedAndNoFindingFacts(t, f)
}

// TestSupplyChainImpactSupportsFamilySeededRedsLive proves the family can fail: a dropped support changes the digest, and a
// planted write of a finding fact is refused by the armed trigger with the pinned error. Without these two the family's
// equal digests and zero finding facts would be indistinguishable from a vacuous pass.
func TestSupplyChainImpactSupportsFamilySeededRedsLive(t *testing.T) {
	f := newImpactFamily(t)
	f.drain(1, time.Minute, nil)
	f.resweep()
	digest := f.digest()

	var dropped string
	if err := f.db.QueryRowContext(f.ctx, `SELECT fact_id FROM supply_chain_impact_finding_supports ORDER BY fact_id LIMIT 1`).Scan(&dropped); err != nil {
		t.Fatalf("pick a support to drop: %v", err)
	}
	if _, err := f.db.ExecContext(f.ctx, `DELETE FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, dropped); err != nil {
		t.Fatalf("drop a support: %v", err)
	}
	var mutated string
	if err := f.db.QueryRowContext(f.ctx, familyDigestSQL).Scan(&mutated); err != nil {
		t.Fatalf("digest after the drop: %v", err)
	}
	if mutated == digest {
		t.Fatal("dropping a support did not change the digest: the digest cannot bite")
	}

	_, err := f.db.ExecContext(f.ctx, writeStopInsert, "family-planted-finding", "scope:family", "generation:family", writeStopKind)
	requireWriteStop(t, err)
}
