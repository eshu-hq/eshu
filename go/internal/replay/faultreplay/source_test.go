// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// TestFaultingWorkSourceSatisfiesReducerInterfaces is a compile-time guard
// that the decorator implements both reducer claim interfaces, matching
// schedulereplay's TestScheduledWorkSourceSatisfiesReducerInterfaces.
func TestFaultingWorkSourceSatisfiesReducerInterfaces(t *testing.T) {
	t.Parallel()

	var _ reducer.WorkSource = (*faultreplay.FaultingWorkSource)(nil)
	var _ reducer.BatchWorkSource = (*faultreplay.FaultingWorkSource)(nil)
}

func afterClaimsScript(n int) faultreplay.Script {
	claims := n
	return faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindKillWorkerAfterClaim,
			Trigger: faultreplay.Trigger{AfterClaims: &claims},
		}},
	}
}

// TestFaultingWorkSourceKillWorkerRedeliversAfterNthClaim proves
// kill-worker-after-claim redelivers exactly the intent claimed at the
// scripted ordinal, exactly once, without needing a second worker.
func TestFaultingWorkSourceKillWorkerRedeliversAfterNthClaim(t *testing.T) {
	t.Parallel()

	schedule := []reducer.Intent{{IntentID: "a"}, {IntentID: "b"}, {IntentID: "c"}}
	inner := schedulereplay.NewScheduledWorkSource(schedule)

	src, err := faultreplay.NewFaultingWorkSource(inner, afterClaimsScript(2))
	if err != nil {
		t.Fatalf("NewFaultingWorkSource: %v", err)
	}

	var got []string
	for {
		intent, ok, err := src.Claim(context.Background())
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if !ok {
			if src.Drained() {
				break
			}
			t.Fatal("Claim returned ok=false but source is not drained")
		}
		got = append(got, intent.IntentID)
		if len(got) > 10 {
			t.Fatal("Claim loop did not terminate; redelivery queue never drained")
		}
	}

	want := []string{"a", "b", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("claim order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claim order = %v, want %v", got, want)
		}
	}
	if src.InjectedRedeliveries() != 1 {
		t.Fatalf("InjectedRedeliveries() = %d, want 1 (fault must fire, not silently no-op)", src.InjectedRedeliveries())
	}
}

// TestFaultingWorkSourceKillWorkerAfterLastClaimStillDrains proves the
// redelivery queued after the schedule's very last claim (the trickiest
// timing: inner.Drained() becomes true before the redelivery is popped) is
// still delivered and the source correctly reports not-drained until it is.
func TestFaultingWorkSourceKillWorkerAfterLastClaimStillDrains(t *testing.T) {
	t.Parallel()

	schedule := []reducer.Intent{{IntentID: "a"}, {IntentID: "b"}}
	inner := schedulereplay.NewScheduledWorkSource(schedule)

	src, err := faultreplay.NewFaultingWorkSource(inner, afterClaimsScript(2))
	if err != nil {
		t.Fatalf("NewFaultingWorkSource: %v", err)
	}

	var got []string
	for i := 0; i < 10; i++ {
		intent, ok, err := src.Claim(context.Background())
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if !ok {
			continue
		}
		got = append(got, intent.IntentID)
	}

	want := []string{"a", "b", "b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("claim order = %v, want %v", got, want)
	}
	if !src.Drained() {
		t.Fatal("source should be drained after redelivery is claimed")
	}
}

// TestFaultingWorkSourceMidHandlerRendezvousBlocksUntilClaimed proves
// ArmMidHandlerDuplicate's contract directly: the returned channel stays open
// until a later Claim call actually pops the duplicate, and that duplicate is
// the same intent, delivered again.
func TestFaultingWorkSourceMidHandlerRendezvousBlocksUntilClaimed(t *testing.T) {
	t.Parallel()

	schedule := []reducer.Intent{{IntentID: "a"}}
	inner := schedulereplay.NewScheduledWorkSource(schedule)
	src, err := faultreplay.NewFaultingWorkSource(inner, faultreplay.Script{Version: faultreplay.CurrentVersion})
	if err != nil {
		t.Fatalf("NewFaultingWorkSource: %v", err)
	}

	target := reducer.Intent{IntentID: "a"}
	released := src.ArmMidHandlerDuplicate(target)

	select {
	case <-released:
		t.Fatal("rendezvous released before the duplicate was claimed")
	default:
	}

	intent, ok, err := src.Claim(context.Background())
	if err != nil || !ok {
		t.Fatalf("Claim: intent=%v ok=%v err=%v", intent, ok, err)
	}
	if intent.IntentID != "a" {
		t.Fatalf("Claim delivered %q, want the armed duplicate %q", intent.IntentID, "a")
	}

	select {
	case <-released:
	default:
		t.Fatal("rendezvous did not release after the duplicate was claimed")
	}
	if src.InjectedRedeliveries() != 1 {
		t.Fatalf("InjectedRedeliveries() = %d, want 1", src.InjectedRedeliveries())
	}
}

// TestFaultingWorkSourceRejectsUnsupportedFaultKinds proves construction
// fails loudly for faults this hermetic tier cannot honor
// (restart-backend-between-phase-groups) and for more than one
// expire-lease-mid-handler fault, rather than silently ignoring them.
func TestFaultingWorkSourceRejectsUnsupportedFaultKinds(t *testing.T) {
	t.Parallel()

	afterPhaseGroups := 1
	inner := schedulereplay.NewScheduledWorkSource(nil)
	_, err := faultreplay.NewFaultingWorkSource(inner, faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindRestartBackendBetweenPhaseGroups,
			Trigger: faultreplay.Trigger{AfterPhaseGroups: &afterPhaseGroups},
		}},
	})
	if err == nil {
		t.Fatal("expected an error for restart-backend-between-phase-groups, got nil")
	}

	idA, idB := "a", "b"
	_, err = faultreplay.NewFaultingWorkSource(inner, faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{Kind: faultreplay.KindExpireLeaseMidHandler, Trigger: faultreplay.Trigger{IntentID: &idA}},
			{Kind: faultreplay.KindExpireLeaseMidHandler, Trigger: faultreplay.Trigger{IntentID: &idB}},
		},
	})
	if err == nil {
		t.Fatal("expected an error for more than one expire-lease-mid-handler fault, got nil")
	}
}
