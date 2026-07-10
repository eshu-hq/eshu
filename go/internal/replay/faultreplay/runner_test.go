// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// cassetteRelPath is the same committed nested-directory cassette
// schedulereplay's own acceptance tests replay, reused here (not a new
// fixture) so a fault run and the fault-free baseline it must converge with
// draw from identical inputs.
var cassetteRelPath = filepath.Join("..", "..", "..", "..", "testdata", "cassettes", "replayoffline", "nested-directory-tree.json")

func loadItems(t *testing.T) []schedulereplay.WorkItem {
	t.Helper()
	items, err := schedulereplay.LoadWorkItems(cassetteRelPath)
	if err != nil {
		t.Fatalf("load work items from cassette %s: %v", cassetteRelPath, err)
	}
	if len(items) < 4 {
		t.Fatalf("want at least 4 work items (repo + 3 dirs), got %d", len(items))
	}
	return items
}

func baselineSnapshot(t *testing.T, items []schedulereplay.WorkItem) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snap, err := schedulereplay.RunSchedule(ctx, schedulereplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
	})
	if err != nil {
		t.Fatalf("baseline RunSchedule: %v", err)
	}
	if len(snap) == 0 {
		t.Fatal("baseline RunSchedule: empty snapshot")
	}
	return snap
}

// TestFaultReplayKillWorkerAfterClaimConverges is the core Layer-4 acceptance
// for kill-worker-after-claim: the scripted redelivery still converges on the
// byte-identical fault-free baseline snapshot.
func TestFaultReplayKillWorkerAfterClaimConverges(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	baseline := baselineSnapshot(t, items)

	afterClaims := 2
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindKillWorkerAfterClaim,
				Trigger: faultreplay.Trigger{AfterClaims: &afterClaims},
			}},
		},
	})
	if err != nil {
		t.Fatalf("RunFault(kill-worker-after-claim): %v", err)
	}
	if len(report.FailedIntentIDs) != 0 {
		t.Fatalf("FailedIntentIDs = %v, want none", report.FailedIntentIDs)
	}
	if !bytes.Equal(baseline, report.Snapshot) {
		t.Fatalf("kill-worker-after-claim snapshot differs from fault-free baseline:\n%s\n---\n%s", report.Snapshot, baseline)
	}
}

// TestFaultReplayExpireLeaseMidHandlerConverges is the core Layer-4
// acceptance for expire-lease-mid-handler: two workers are genuinely
// concurrently in-flight on the same intent (proven by the rendezvous in
// source.go/executor.go), and the double-apply still converges on the
// byte-identical baseline because ApplyCanonical is idempotent (T4).
func TestFaultReplayExpireLeaseMidHandlerConverges(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	baseline := baselineSnapshot(t, items)

	// Target the last scripted item: the trickiest timing, since the work
	// source's inner schedule is exhausted by the time the duplicate needs to
	// be armed.
	targetID := items[len(items)-1].IntentID

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 4,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindExpireLeaseMidHandler,
				Trigger: faultreplay.Trigger{IntentID: &targetID},
			}},
		},
	})
	if err != nil {
		t.Fatalf("RunFault(expire-lease-mid-handler): %v", err)
	}
	if len(report.FailedIntentIDs) != 0 {
		t.Fatalf("FailedIntentIDs = %v, want none", report.FailedIntentIDs)
	}
	if !bytes.Equal(baseline, report.Snapshot) {
		t.Fatalf("expire-lease-mid-handler snapshot differs from fault-free baseline:\n%s\n---\n%s", report.Snapshot, baseline)
	}
}

// TestFaultReplayFailGraphWriteOnceThenSucceedConverges is the core Layer-4
// acceptance for fail-graph-write-once-then-succeed, both lanes: the
// transient failure recovers (in place for executor-retry, via redelivery for
// queue-retry) and the run converges on the byte-identical baseline.
func TestFaultReplayFailGraphWriteOnceThenSucceedConverges(t *testing.T) {
	t.Parallel()

	for _, lane := range []string{faultreplay.LaneExecutorRetry, faultreplay.LaneQueueRetry} {
		lane := lane
		t.Run(lane, func(t *testing.T) {
			t.Parallel()

			items := loadItems(t)
			baseline := baselineSnapshot(t, items)

			ordinal := 1
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			report, err := faultreplay.RunFault(ctx, faultreplay.Config{
				Items:   schedulereplay.ScheduleInOrder(items),
				Workers: 1,
				Apply:   schedulereplay.ApplyCanonical,
				Script: faultreplay.Script{
					Version: faultreplay.CurrentVersion,
					Faults: []faultreplay.FaultOp{{
						Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
						Trigger: faultreplay.Trigger{StatementOrdinal: &ordinal},
						Target:  faultreplay.Target{Lane: lane},
					}},
				},
			})
			if err != nil {
				t.Fatalf("RunFault(fail-graph-write-once-then-succeed, lane=%s): %v", lane, err)
			}
			if len(report.FailedIntentIDs) != 0 {
				t.Fatalf("FailedIntentIDs = %v, want none (the intent must recover)", report.FailedIntentIDs)
			}
			if !bytes.Equal(baseline, report.Snapshot) {
				t.Fatalf("lane=%s snapshot differs from fault-free baseline:\n%s\n---\n%s", lane, report.Snapshot, baseline)
			}
		})
	}
}

// TestFaultReplayFailTerminalDeadLettersExactlyOne is the acceptance for
// fail-terminal: exactly the scripted intent lands in the terminal failed
// set, and only that one -- every other intent still succeeds.
func TestFaultReplayFailTerminalDeadLettersExactlyOne(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	targetID := items[0].IntentID // the repository item; every directory depends on it

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindFailTerminal,
				Trigger: faultreplay.Trigger{IntentID: &targetID},
			}},
		},
	})
	if err != nil {
		t.Fatalf("RunFault(fail-terminal): %v", err)
	}
	if len(report.FailedIntentIDs) != 1 || report.FailedIntentIDs[0] != targetID {
		t.Fatalf("FailedIntentIDs = %v, want exactly [%q]", report.FailedIntentIDs, targetID)
	}
	if report.Acked != int64(len(items)-1) {
		t.Fatalf("Acked = %d, want %d (every intent except the scripted terminal failure)", report.Acked, len(items)-1)
	}
	if len(report.Snapshot) == 0 {
		t.Fatal("Snapshot is empty; a fail-terminal run must still drain and snapshot the rest of the graph")
	}
}

// TestFaultReplayFailsWhenContextCanceledBeforeDrain proves the fault runner
// refuses to report a partial drain: a pre-canceled context yields an error,
// never a green empty snapshot, mirroring
// schedulereplay.TestScheduleReplayFailsWhenContextCanceledBeforeDrain.
func TestFaultReplayFailsWhenContextCanceledBeforeDrain(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before the run starts

	report, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
	})
	if err == nil {
		t.Fatalf("expected an error when the context is canceled before drain, got nil snapshot of %d bytes (would mask a non-draining replay)", len(report.Snapshot))
	}
}

// TestFaultReplayFailsOnInertExecutorRetryFault is the P1 regression: an
// executor-retry-lane fail-graph-write-once-then-succeed fault whose
// statement_ordinal never matches any Execute call (out of range for this
// schedule) never fires. This lane contributes nothing to the drain-total
// accounting (see extraDrainCount's doc comment), so the run drains cleanly
// on the normal items alone and, before this fix, RunFault would snapshot the
// fault-free graph and report success -- the Layer 4 acceptance would pass
// with an inert, unproven fault. RunFault MUST instead return an error naming
// the fault that never fired.
func TestFaultReplayFailsOnInertExecutorRetryFault(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	// callOrdinal only ever reaches len(items) for this Workers=1 schedule, so
	// this ordinal can never match.
	ordinal := len(items) + 1000

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
				Trigger: faultreplay.Trigger{StatementOrdinal: &ordinal},
				Target:  faultreplay.Target{Lane: faultreplay.LaneExecutorRetry},
			}},
		},
	})
	if err == nil {
		t.Fatalf("RunFault(inert executor-retry fault) = nil error, snapshot len=%d; want an error (measured-inert false-green)", len(report.Snapshot))
	}
	if !strings.Contains(err.Error(), "never fired") {
		t.Fatalf("error = %q, want it to mention the fault never fired", err.Error())
	}

	// Control: the identical fault with an in-range ordinal must still fire and
	// pass, proving the check does not reject every executor-retry script.
	valid := 1
	report, err = faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
				Trigger: faultreplay.Trigger{StatementOrdinal: &valid},
				Target:  faultreplay.Target{Lane: faultreplay.LaneExecutorRetry},
			}},
		},
	})
	if err != nil {
		t.Fatalf("RunFault(valid executor-retry fault): %v", err)
	}
	if len(report.Snapshot) == 0 {
		t.Fatal("RunFault(valid executor-retry fault): empty snapshot")
	}
}

// TestFaultReplayFailsOnInertFailTerminalFault proves the same class of bug
// for fail-terminal: an intent_id that never matches any scheduled intent
// never fires (fail-terminal also contributes nothing to the drain-total
// accounting), so the run would otherwise drain cleanly and report every
// intent acked -- a green pass for a script that asserted nothing.
func TestFaultReplayFailsOnInertFailTerminalFault(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	bogusID := "intent-id-that-does-not-exist-in-the-schedule"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindFailTerminal,
				Trigger: faultreplay.Trigger{IntentID: &bogusID},
			}},
		},
	})
	if err == nil {
		t.Fatal("RunFault(inert fail-terminal fault) = nil error, want an error (measured-inert false-green)")
	}
	if !strings.Contains(err.Error(), "never fired") {
		t.Fatalf("error = %q, want it to mention the fault never fired", err.Error())
	}
}

// TestFaultReplayFailedIntentIDsDedupedOnDuplicateTerminalDelivery proves
// Report.FailedIntentIDs is a durable SET: when the same fail-terminal-
// targeted intent is delivered more than once (here, via a duplicate
// schedule -- schedulereplay.ScheduleWithDuplicates re-delivers the first and
// last items -- a fault-injected redelivery targeting the same intent has the
// identical effect), each Fail must not add a second copy of the same
// dead-letter ID.
func TestFaultReplayFailedIntentIDsDedupedOnDuplicateTerminalDelivery(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	targetID := items[0].IntentID // ScheduleWithDuplicates re-delivers items[0]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleWithDuplicates(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindFailTerminal,
				Trigger: faultreplay.Trigger{IntentID: &targetID},
			}},
		},
	})
	if err != nil {
		t.Fatalf("RunFault(fail-terminal, duplicate schedule): %v", err)
	}
	if len(report.FailedIntentIDs) != 1 || report.FailedIntentIDs[0] != targetID {
		t.Fatalf("FailedIntentIDs = %v, want exactly one entry [%q] (must be a deduped set)", report.FailedIntentIDs, targetID)
	}
}

// TestFaultReplayRejectsMidHandlerWithSingleWorker proves Config.validate
// refuses (rather than hangs on) an expire-lease-mid-handler script paired
// with Workers < 2.
func TestFaultReplayRejectsMidHandlerWithSingleWorker(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	targetID := items[0].IntentID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
		Script: faultreplay.Script{
			Version: faultreplay.CurrentVersion,
			Faults: []faultreplay.FaultOp{{
				Kind:    faultreplay.KindExpireLeaseMidHandler,
				Trigger: faultreplay.Trigger{IntentID: &targetID},
			}},
		},
	})
	if err == nil {
		t.Fatal("expected an error for expire-lease-mid-handler with Workers=1, got nil")
	}
}
