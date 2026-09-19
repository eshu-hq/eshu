// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"slices"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// ledgerStub is an in-memory crossscope.ReadinessWaitLedger.
type ledgerStub struct {
	rows    map[string]crossscope.ReadinessWait
	upserts int
	clears  int
}

func (l *ledgerStub) GetReadinessWait(_ context.Context, scopeID string, domain reducercontract.Domain) (crossscope.ReadinessWait, bool, error) {
	row, ok := l.rows[scopeID+"|"+string(domain)]
	return row, ok, nil
}

func (l *ledgerStub) UpsertReadinessWait(_ context.Context, wait crossscope.ReadinessWait) error {
	l.upserts++
	key := wait.ScopeID + "|" + string(wait.Domain)
	if existing, ok := l.rows[key]; ok {
		// The store's anchor_epoch fence: drop a lower epoch, keep the
		// earlier anchor at an equal epoch.
		if wait.AnchorEpoch < existing.AnchorEpoch {
			return nil
		}
		if wait.AnchorEpoch == existing.AnchorEpoch && existing.FirstDeferredAt.Before(wait.FirstDeferredAt) {
			wait.FirstDeferredAt = existing.FirstDeferredAt
		}
	}
	wait.ClearedAt = time.Time{}
	l.rows[key] = wait
	return nil
}

func (l *ledgerStub) ClearReadinessWait(_ context.Context, wait crossscope.ReadinessWait) error {
	l.clears++
	key := wait.ScopeID + "|" + string(wait.Domain)
	existing, ok := l.rows[key]
	if !ok || existing.AnchorEpoch != wait.AnchorEpoch {
		return nil
	}
	l.rows[key] = crossscope.ReadinessWait{
		ScopeID: wait.ScopeID, Domain: wait.Domain, FirstDeferredAt: wait.ClearedAt,
		AnchorEpoch: existing.AnchorEpoch + 1, CommittedGenerationID: wait.CommittedGenerationID,
		CommittedCycleStartedAt: wait.CommittedCycleStartedAt, ClearedAt: wait.ClearedAt, UpdatedAt: wait.ClearedAt,
	}
	return nil
}

// countingLookup reports existing anchors and counts calls.
type countingLookup struct {
	existing map[Anchor]struct{}
	calls    [][]Anchor
}

func (c *countingLookup) ExistingAnchors(_ context.Context, anchors []Anchor) (map[Anchor]struct{}, error) {
	c.calls = append(c.calls, slices.Clone(anchors))
	return c.existing, nil
}

func waitTestIntent(generation string, cycle time.Time) reducercontract.Intent {
	return reducercontract.Intent{
		ScopeID: "aws:123456789012:us-east-1:iam", GenerationID: generation,
		Domain:         reducercontract.DomainWorkloadCloudRelationshipMaterialization,
		CycleStartedAt: cycle, EnqueuedAt: cycle,
	}
}

func TestAnchorKeyRoundTrips(t *testing.T) {
	t.Parallel()
	for _, anchor := range []Anchor{prod, stage, {WorkloadID: "workload:a@b", Environment: "e|f"}} {
		got, ok := ParseAnchorKey(AnchorKey(anchor))
		if !ok || got != anchor {
			t.Fatalf("ParseAnchorKey(AnchorKey(%v)) = %v, %v", anchor, got, ok)
		}
	}
	if _, ok := ParseAnchorKey("no-separator"); ok {
		t.Fatal("ParseAnchorKey accepted a key without the separator")
	}
}

// TestWaitCommitsFirstThenPollsCheaply is §4.5 item 7 (USES): the first
// evaluation commits and defers, an unchanged poll asks only about the missing
// anchor and commits nothing, and a poll after the instance appears falls back
// to the full evaluation, which commits and clears the ledger.
func TestWaitCommitsFirstThenPollsCheaply(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	lookup := &countingLookup{existing: map[Anchor]struct{}{prod: {}}}
	ledger := &ledgerStub{rows: map[string]crossscope.ReadinessWait{}}
	wait := Wait{Lookup: lookup, Ledger: ledger, Now: func() time.Time { return clock }}
	intent := waitTestIntent("gen-1", clock)

	existing, found, err := wait.Read(context.Background(), intent)
	if err != nil || found {
		t.Fatalf("Read() = found %v err %v, want no row", found, err)
	}
	if _, handled, _ := wait.Poll(context.Background(), intent, existing, found); handled {
		t.Fatal("Poll handled an evaluation with no ledger row")
	}
	eval, err := wait.Evaluate(context.Background(), intent, existing, found, []Anchor{stage, prod})
	if err != nil || !eval.Decision.Commit || !eval.Decision.Defer || !slices.Equal(eval.Missing, []Anchor{stage}) {
		t.Fatalf("first Evaluate = %+v (err %v), want commit, defer, missing [stage]", eval, err)
	}
	if err := wait.Finish(context.Background(), intent, eval); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	clock = clock.Add(30 * time.Second)
	existing, found, _ = wait.Read(context.Background(), intent)
	polled, handled, err := wait.Poll(context.Background(), intent, existing, found)
	if err != nil || !handled || polled.Decision.Commit || !polled.Decision.Defer {
		t.Fatalf("unchanged Poll = %+v handled %v err %v, want a defer with no commit", polled, handled, err)
	}
	if last := lookup.calls[len(lookup.calls)-1]; !slices.Equal(last, []Anchor{stage}) {
		t.Fatalf("poll looked up %v, want only the missing anchor", last)
	}
	if ledger.upserts != 1 {
		t.Fatalf("ledger upserts = %d, want 1 (the unchanged poll writes nothing)", ledger.upserts)
	}

	lookup.existing = map[Anchor]struct{}{prod: {}, stage: {}}
	if _, handled, _ := wait.Poll(context.Background(), intent, existing, found); handled {
		t.Fatal("Poll handled a resolved anchor, want the full-evaluation fallback")
	}
	eval, err = wait.Evaluate(context.Background(), intent, existing, found, []Anchor{stage, prod})
	if err != nil || !eval.Decision.Commit || eval.Decision.Defer || !eval.Decision.Clear {
		t.Fatalf("resolved Evaluate = %+v (err %v), want commit and clear", eval, err)
	}
	if err := wait.Finish(context.Background(), intent, eval); err != nil || ledger.clears != 1 {
		t.Fatalf("Finish() err %v clears %d, want the row cleared", err, ledger.clears)
	}
}

// TestWaitSettlesAcrossSupersedingGeneration is the R2-F1 closing shape for
// USES: gen-2 keeps gen-1's anchor, commits at its first claim, and settles at
// anchor+MaxWait; gen-3 with the same missing set commits without deferring.
func TestWaitSettlesAcrossSupersedingGeneration(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	clock := start
	ledger := &ledgerStub{rows: map[string]crossscope.ReadinessWait{}}
	wait := Wait{
		Lookup: &countingLookup{existing: map[Anchor]struct{}{prod: {}}}, Ledger: ledger,
		MaxWait: 10 * time.Minute, Now: func() time.Time { return clock },
	}
	run := func(intent reducercontract.Intent) Evaluation {
		t.Helper()
		existing, found, err := wait.Read(context.Background(), intent)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if eval, handled, err := wait.Poll(context.Background(), intent, existing, found); err != nil || handled {
			if err != nil {
				t.Fatalf("Poll: %v", err)
			}
			if err := wait.Finish(context.Background(), intent, eval); err != nil {
				t.Fatalf("Finish after poll: %v", err)
			}
			return eval
		}
		eval, err := wait.Evaluate(context.Background(), intent, existing, found, []Anchor{prod, stage})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if err := wait.Finish(context.Background(), intent, eval); err != nil {
			t.Fatalf("Finish: %v", err)
		}
		return eval
	}

	run(waitTestIntent("gen-1", start))
	clock = start.Add(6 * time.Minute)
	gen2 := waitTestIntent("gen-2", clock)
	if eval := run(gen2); !eval.Decision.Commit || !eval.Decision.Defer {
		t.Fatalf("gen-2 first claim = %+v, want commit then defer", eval.Decision)
	}
	clock = start.Add(10 * time.Minute)
	if eval := run(gen2); eval.Decision.Commit || eval.Decision.Defer || eval.Decision.Outcome != crossscope.ReadinessWaitAbandoned {
		t.Fatalf("gen-2 at anchor+MaxWait = %+v, want settle without re-commit", eval.Decision)
	}
	clock = start.Add(40 * time.Minute)
	if eval := run(waitTestIntent("gen-3", clock)); !eval.Decision.Commit || eval.Decision.Defer ||
		eval.Decision.Outcome != crossscope.ReadinessWaitSettledMissing {
		t.Fatalf("gen-3 = %+v, want commit as settled_missing", eval.Decision)
	}
}

// TestWaitWithoutLookupAlwaysCommits keeps the nil-lookup test wiring: no
// graph read, no ledger read, commit every time.
func TestWaitWithoutLookupAlwaysCommits(t *testing.T) {
	t.Parallel()
	wait := Wait{}
	intent := waitTestIntent("gen-1", time.Now())
	existing, found, err := wait.Read(context.Background(), intent)
	if err != nil || found {
		t.Fatalf("Read() with nil lookup = found %v err %v", found, err)
	}
	eval, err := wait.Evaluate(context.Background(), intent, existing, found, []Anchor{prod})
	if err != nil || !eval.Decision.Commit || eval.Decision.Defer || len(eval.Missing) != 0 {
		t.Fatalf("Evaluate() with nil lookup = %+v err %v, want an unconditional commit", eval, err)
	}
}
