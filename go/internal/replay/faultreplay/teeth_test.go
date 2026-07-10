// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay_test

import (
	"bytes"
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	faultreplay "github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	schedulereplay "github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// TestFaultReplayTeethExpireLeaseCatchesNonIdempotentWrite is the P6 teeth: it
// proves the Layer-4 fault gate is not inert. A deliberately non-idempotent
// write, replayed under expire-lease-mid-handler — which executes the targeted
// intent twice via concurrent redelivery — MUST diverge from the fault-free
// single-execution baseline, and the byte comparison MUST catch it. A
// non-divergent outcome would mean the gate passes vacuously (the P3
// measured-inert lesson), so this test fails loudly in that case. It mirrors
// schedulereplay's TestScheduleReplayCatchesOrderSensitiveBug, one axis over:
// duplication (fault) instead of reordering (schedule).
func TestFaultReplayTeethExpireLeaseCatchesNonIdempotentWrite(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	targetID := items[len(items)-1].IntentID
	expireLease := faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindExpireLeaseMidHandler,
			Trigger: faultreplay.Trigger{IntentID: &targetID},
		}},
	}

	// A non-idempotent applier: each application stamps a monotonically
	// increasing sequence onto the node's props, so applying a node twice leaves
	// a different value than applying it once — exactly the non-idempotent-write
	// class the gate exists to catch. The counter is atomic so -race stays clean
	// even though the runner serializes applies behind the graph mutex.
	var seq atomic.Int64
	nonIdempotent := func(g *schedulereplay.Graph, item schedulereplay.WorkItem) {
		for _, n := range item.Nodes {
			n.Props = map[string]string{"applyseq": strconv.FormatInt(seq.Add(1), 10)}
			g.UpsertNode(n)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fault-free single-execution baseline with the SAME non-idempotent applier.
	// A fault-free run still needs a versioned (empty-faults) script.
	seq.Store(0)
	base, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 4,
		Apply:   nonIdempotent,
		Script:  faultreplay.Script{Version: faultreplay.CurrentVersion},
	})
	if err != nil {
		t.Fatalf("baseline RunFault: %v", err)
	}

	// Same applier under expire-lease-mid-handler: the target intent executes
	// twice, so its stamped applyseq differs from the single-execution baseline.
	seq.Store(0)
	faulted, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 4,
		Apply:   nonIdempotent,
		Script:  expireLease,
	})
	if err != nil {
		t.Fatalf("faulted RunFault: %v", err)
	}

	if bytes.Equal(base.Snapshot, faulted.Snapshot) {
		t.Fatal("TEETH INERT: a non-idempotent write did not diverge under expire-lease-mid-handler double execution; the Layer-4 fault gate would pass vacuously")
	}

	// Control: the idempotent canonical applier MUST converge under the very
	// same fault. If it diverged, the "byte-identical vs fault-free baseline"
	// assertion would be meaningless (everything would look broken).
	canonBaseline := baselineSnapshot(t, items)
	canonFaulted, err := faultreplay.RunFault(ctx, faultreplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 4,
		Apply:   schedulereplay.ApplyCanonical,
		Script:  expireLease,
	})
	if err != nil {
		t.Fatalf("canonical faulted RunFault: %v", err)
	}
	if !bytes.Equal(canonBaseline, canonFaulted.Snapshot) {
		t.Fatal("control failed: idempotent ApplyCanonical must converge under expire-lease-mid-handler")
	}
}
