// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// cassetteRelPath is the committed nested-directory cassette (the same fixture
// the R-5 offline tier replays), relative to this package directory
// (go/internal/replay/schedulereplay).
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

// TestScheduleReplayOrderInvariantSnapshot is the core acceptance: the same set
// of recorded work items, delivered through the deterministic in-memory work
// source in at least three scripted orders (in-order, adversarial reverse, and a
// duplicate-delivery order), drained through the real reducer service loop,
// converges on a byte-identical canonical graph snapshot. The snapshot is the
// order-independent graph truth (the offline B-12 analog).
func TestScheduleReplayOrderInvariantSnapshot(t *testing.T) {
	t.Parallel()

	items := loadItems(t)

	orders := map[string][]schedulereplay.WorkItem{
		"in-order":        schedulereplay.ScheduleInOrder(items),
		"reverse":         schedulereplay.ScheduleReverse(items),
		"rotated":         schedulereplay.ScheduleRotated(items, 2),
		"with-duplicates": schedulereplay.ScheduleWithDuplicates(items),
	}

	var baselineName string
	var baseline []byte
	for name, scheduleItems := range orders {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		snap, err := schedulereplay.RunSchedule(ctx, schedulereplay.Config{
			Items:   scheduleItems,
			Workers: 1,
			Apply:   schedulereplay.ApplyCanonical,
		})
		cancel()
		if err != nil {
			t.Fatalf("RunSchedule(%s): %v", name, err)
		}
		if len(snap) == 0 {
			t.Fatalf("RunSchedule(%s): empty snapshot", name)
		}
		if baseline == nil {
			baselineName, baseline = name, snap
			continue
		}
		if !bytes.Equal(baseline, snap) {
			t.Fatalf("snapshot for order %q differs from order %q:\n%s\n---\n%s",
				name, baselineName, baseline, snap)
		}
	}
}

// TestScheduleReplayConcurrentBatchInvariant exercises the real reducer batch
// claim path (BatchWorkSource.ClaimBatch with concurrent workers) and asserts
// the final snapshot still equals the deterministic sequential one. Genuine
// concurrency on the shared conflict domain must not change the converged graph
// truth.
func TestScheduleReplayConcurrentBatchInvariant(t *testing.T) {
	t.Parallel()

	items := loadItems(t)

	// Each run gets its own deadline so a slow sequential run cannot eat into the
	// concurrent run's budget.
	seqCtx, seqCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer seqCancel()
	seqSnap, err := schedulereplay.RunSchedule(seqCtx, schedulereplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   schedulereplay.ApplyCanonical,
	})
	if err != nil {
		t.Fatalf("sequential RunSchedule: %v", err)
	}

	concCtx, concCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer concCancel()
	concSnap, batchCalls, err := schedulereplay.RunScheduleReport(concCtx, schedulereplay.Config{
		Items:   schedulereplay.ScheduleReverse(items),
		Workers: 4,
		Apply:   schedulereplay.ApplyCanonical,
	})
	if err != nil {
		t.Fatalf("concurrent RunSchedule: %v", err)
	}
	if batchCalls == 0 {
		t.Fatal("concurrent run never invoked ClaimBatch; the in-memory BatchWorkSource batch path was not exercised")
	}
	if !bytes.Equal(seqSnap, concSnap) {
		t.Fatalf("concurrent snapshot differs from sequential:\n%s\n---\n%s", seqSnap, concSnap)
	}
}

// TestScheduleReplayCatchesOrderSensitiveBug is the teeth proof: a deliberately
// order-sensitive applier (it drops a CONTAINS edge whenever the parent node has
// not yet been applied) produces DIFFERENT snapshots for in-order vs reverse
// delivery. The order-invariance check the gate relies on must flag that
// divergence — if these were equal, the gate could not catch the #4019
// parent-before-child class.
func TestScheduleReplayCatchesOrderSensitiveBug(t *testing.T) {
	t.Parallel()

	items := loadItems(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inOrder, err := schedulereplay.RunSchedule(ctx, schedulereplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   orderSensitiveApply,
	})
	if err != nil {
		t.Fatalf("in-order buggy RunSchedule: %v", err)
	}
	reverse, err := schedulereplay.RunSchedule(ctx, schedulereplay.Config{
		Items:   schedulereplay.ScheduleReverse(items),
		Workers: 1,
		Apply:   orderSensitiveApply,
	})
	if err != nil {
		t.Fatalf("reverse buggy RunSchedule: %v", err)
	}

	if bytes.Equal(inOrder, reverse) {
		t.Fatal("order-sensitive applier produced identical snapshots; the harness cannot catch ordering bugs (teeth missing)")
	}
}

// orderSensitiveApply is a deliberately broken applier used only to prove the
// harness detects ordering sensitivity. It upserts nodes but drops any CONTAINS
// edge whose parent (From) node has not already been applied — the classic
// child-before-parent dropped-edge bug.
func orderSensitiveApply(g *schedulereplay.Graph, item schedulereplay.WorkItem) {
	for _, n := range item.Nodes {
		g.UpsertNode(n)
	}
	for _, e := range item.Edges {
		if !g.HasNode(e.From) {
			continue // bug: silently drop the edge when the parent is not present yet
		}
		g.UpsertEdge(e)
	}
}
