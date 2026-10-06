// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// TestActivationObligationLeaseExpiresMidMaintenanceLive (#7584 D3 step 4,
// item 4): the first owner's maintenance outlives its lease. The deadline
// guard cancels it before the lease ends and nothing is finalized; after
// expiry a second replica claims with token 2 and completes with the
// production maintainer; the first owner's late Finalize is not_owner and
// writes nothing. Across both owners there is exactly one completion and
// exactly one wake.
func TestActivationObligationLeaseExpiresMidMaintenanceLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "act_lease_mid")
	setupComposed(t, ctx, database, composedTgt2)
	first := newComposedConsumer(t, database, "7584-lease-first", 1500*time.Millisecond, 0)
	first.port.before = func(ctx context.Context, _ maintenance.ActivationObligation) error {
		<-ctx.Done() // a pass slower than the lease
		return ctx.Err()
	}
	if _, err := first.runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := first.failures(t, ctx, "maintenance_timeout"); got != 1 {
		t.Fatalf("first owner maintenance_timeout = %d, want 1", got)
	}
	assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "leased", 1)
	requireWoken(t, ctx, database, "tgt-2", false)
	firstWork := obligationOf(first.port.seen[0])

	awaitObligationLeaseExpiry(t, ctx, database, "git:tgt", "tgt-2")
	second := newComposedConsumer(t, database, "7584-lease-second", time.Minute, 0)
	if _, err := second.runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	second.requireNoFailures(t, ctx)
	assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "completed", 2)
	if got := second.port.callsFor("tgt-2"); got != 1 {
		t.Fatalf("second owner callbacks = %d, want 1", got)
	}
	requireWoken(t, ctx, database, "tgt-2", true)

	before := composedState(t, ctx, database)
	result, err := activation.NewStore(SQLDB{DB: database}).Finalize(ctx, firstWork)
	if err != nil || result.Outcome != activation.OutcomeNotOwner {
		t.Fatalf("first owner's late Finalize = %+v err=%v, want not_owner", result, err)
	}
	requireComposedState(t, "after the first owner's late Finalize", composedState(t, ctx, database), before)

	firstRM, secondRM := first.metrics(t, ctx), second.metrics(t, ctx)
	completed := targetedCounter(firstRM, "eshu_dp_activation_obligation_finalize_total", "outcome", "completed") +
		targetedCounter(secondRM, "eshu_dp_activation_obligation_finalize_total", "outcome", "completed")
	woken := targetedCounter(firstRM, "eshu_dp_activation_obligation_woken_total", "", "") +
		targetedCounter(secondRM, "eshu_dp_activation_obligation_woken_total", "", "")
	if completed != 1 || woken != 1 {
		t.Fatalf("completions = %d, wakes = %d across both owners, want exactly 1 and 1", completed, woken)
	}
}

// TestActivationObligationRestartBeforePhasePublicationLive (#7584 D3 step 4,
// item 5): the consumer process dies after the partition-scoped pass
// committed its evidence and before the phase publication (the context is
// cancelled at the phase write through a statement hook). The pass fails
// closed: the evidence is durable, no phase or memo, nothing reopened,
// nothing woken, and the obligation stays leased. After the lease a fresh
// replica completes it, ending in exactly the state one clean cycle leaves.
func TestActivationObligationRestartBeforePhasePublicationLive(t *testing.T) {
	refCtx, refDB := openActivationObligationProofDB(t, "act_restart_ref")
	setupComposed(t, refCtx, refDB, composedTgt2)
	clean := newComposedConsumer(t, refDB, "7584-restart-reference", time.Minute, 0)
	if _, err := clean.runner.RunOnce(refCtx); err != nil {
		t.Fatal(err)
	}
	clean.requireNoFailures(t, refCtx)
	reference := composedState(t, refCtx, refDB)

	ctx, database := openActivationObligationProofDB(t, "act_restart_phase")
	setupComposed(t, ctx, database, composedTgt2)
	before := composedState(t, ctx, database)
	first := newComposedConsumer(t, database, "7584-restart-first", 1500*time.Millisecond, 0)
	runCtx, crash := context.WithCancel(ctx)
	defer crash()
	crashed := false
	first.locks.onExec = func(_ context.Context, query string) error {
		if strings.Contains(query, "INSERT INTO graph_projection_phase_state") {
			crashed = true
			crash() // the process dies here: evidence committed, phase not
			return context.Canceled
		}
		return nil
	}
	if _, err := first.runner.RunOnce(runCtx); err != nil {
		t.Fatal(err)
	}
	if !crashed {
		t.Fatal("the partition-scoped pass never reached its phase publication")
	}
	want := replaceTuple(before, "obligation|git:tgt|tgt-2|pending", "obligation|git:tgt|tgt-2|leased")
	newEvidence := 0
	for _, tuple := range reference {
		if strings.HasPrefix(tuple, "evidence|tgt-2|") {
			want = append(want, tuple)
			newEvidence++
		}
	}
	if newEvidence == 0 {
		t.Fatal("the clean cycle wrote no tgt-2 evidence; the fixture proves nothing")
	}
	requireComposedState(t, "durable state after a crash between evidence and phase",
		composedState(t, ctx, database), sortedTuples(want))

	awaitObligationLeaseExpiry(t, ctx, database, "git:tgt", "tgt-2")
	second := newComposedConsumer(t, database, "7584-restart-second", time.Minute, 0)
	if _, err := second.runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	second.requireNoFailures(t, ctx)
	assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "completed", 2)
	requireComposedState(t, "after the next cycle completes it", composedState(t, ctx, database), reference)
}
