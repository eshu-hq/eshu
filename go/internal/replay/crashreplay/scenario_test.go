// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crashreplay_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/crashreplay"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// cassetteRelPath is the committed nested-directory cassette (the same fixture
// the R-5 offline tier and R-13 schedule replay use), relative to this package
// directory (go/internal/replay/crashreplay).
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

// TestCrashRecoveryMatchesNoCrashSnapshot is the core acceptance: a scenario
// crashed at every controlled checkpoint — a clean boundary between items
// (CrashBeforeClaim) and the dirty post-lease-pre-complete window
// (CrashAfterApply) — recovers from durable state and converges on the
// byte-identical canonical graph the same scenario produces with no crash. The
// recovery must never double-complete a work item.
func TestCrashRecoveryMatchesNoCrashSnapshot(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	n := len(items)

	baseCtx, baseCancel := context.WithTimeout(context.Background(), 30*time.Second)
	baseline, err := crashreplay.RunToCompletion(baseCtx, crashreplay.Config{Items: schedulereplay.ScheduleInOrder(items)})
	baseCancel()
	if err != nil {
		t.Fatalf("RunToCompletion baseline: %v", err)
	}
	if len(baseline) == 0 {
		t.Fatal("RunToCompletion baseline produced an empty snapshot")
	}

	cases := []struct {
		name  string
		crash crashreplay.CrashPoint
	}{
		{"before-claim@1", crashreplay.CrashPoint{Kind: crashreplay.CrashBeforeClaim, After: 1}},
		{"before-claim@mid", crashreplay.CrashPoint{Kind: crashreplay.CrashBeforeClaim, After: n / 2}},
		{"after-apply@0", crashreplay.CrashPoint{Kind: crashreplay.CrashAfterApply, After: 0}},
		{"after-apply@mid", crashreplay.CrashPoint{Kind: crashreplay.CrashAfterApply, After: n / 2}},
		{"after-apply@last", crashreplay.CrashPoint{Kind: crashreplay.CrashAfterApply, After: n - 1}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out, err := crashreplay.RunWithCrash(ctx, crashreplay.Config{Items: schedulereplay.ScheduleInOrder(items)}, tc.crash)
			if err != nil {
				t.Fatalf("RunWithCrash(%s): %v", tc.name, err)
			}
			if !out.Report.Crashed {
				t.Fatalf("RunWithCrash(%s): crash never triggered; the scenario did not exercise recovery", tc.name)
			}
			if out.Report.DoubleAcks != 0 {
				t.Fatalf("RunWithCrash(%s): %d work item(s) completed twice; recovery is not idempotent", tc.name, out.Report.DoubleAcks)
			}
			if !bytes.Equal(baseline, out.Snapshot) {
				t.Fatalf("RunWithCrash(%s): recovered snapshot differs from the no-crash snapshot:\n%s\n---\n%s",
					tc.name, baseline, out.Snapshot)
			}
		})
	}
}

// TestCrashAfterApplyReclaimsUnderAdvancedFencingToken proves the dirty window
// is genuinely recovered: an item projected to the graph but not yet completed
// when the crash hits is left holding a durable lease, and recovery reclaims it
// only after its lease expires on the simulated clock — under a strictly higher
// fencing token (attempt count). The idempotent re-projection leaves the graph
// identical to the no-crash truth.
func TestCrashAfterApplyReclaimsUnderAdvancedFencingToken(t *testing.T) {
	t.Parallel()

	items := loadItems(t)

	baseCtx, baseCancel := context.WithTimeout(context.Background(), 30*time.Second)
	baseline, err := crashreplay.RunToCompletion(baseCtx, crashreplay.Config{Items: schedulereplay.ScheduleInOrder(items)})
	baseCancel()
	if err != nil {
		t.Fatalf("RunToCompletion baseline: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := crashreplay.RunWithCrash(ctx, crashreplay.Config{Items: schedulereplay.ScheduleInOrder(items)},
		crashreplay.CrashPoint{Kind: crashreplay.CrashAfterApply, After: 2})
	if err != nil {
		t.Fatalf("RunWithCrash: %v", err)
	}
	if out.Report.ReclaimedAfterCrash < 1 {
		t.Fatalf("expected at least one item reclaimed after the crash, got %d", out.Report.ReclaimedAfterCrash)
	}
	if out.Report.MaxAttempt < 2 {
		t.Fatalf("expected the reclaimed item's fencing token (attempt count) to advance to >= 2, got max attempt %d",
			out.Report.MaxAttempt)
	}
	if out.Report.DoubleAcks != 0 {
		t.Fatalf("recovery double-completed %d item(s)", out.Report.DoubleAcks)
	}
	if !bytes.Equal(baseline, out.Snapshot) {
		t.Fatalf("recovered snapshot differs from no-crash snapshot:\n%s\n---\n%s", baseline, out.Snapshot)
	}
}

// TestCrashBeforeClaimSkipsCompletedWork proves the clean-boundary case never
// redoes durably completed work: with N items already acked before the crash,
// recovery claims only the remainder and no item is completed twice.
func TestCrashBeforeClaimSkipsCompletedWork(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	const completed = 2

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := crashreplay.RunWithCrash(ctx, crashreplay.Config{Items: schedulereplay.ScheduleInOrder(items)},
		crashreplay.CrashPoint{Kind: crashreplay.CrashBeforeClaim, After: completed})
	if err != nil {
		t.Fatalf("RunWithCrash: %v", err)
	}
	if out.Report.PreCrashAcks != completed {
		t.Fatalf("expected %d items completed before the crash, got %d", completed, out.Report.PreCrashAcks)
	}
	if out.Report.RecoveryAcks != len(items)-completed {
		t.Fatalf("expected recovery to complete the remaining %d items, got %d",
			len(items)-completed, out.Report.RecoveryAcks)
	}
	if out.Report.DoubleAcks != 0 {
		t.Fatalf("recovery double-completed %d item(s) that were already durable", out.Report.DoubleAcks)
	}
}

// TestCrashReplayCatchesDuplicateProjection is the teeth proof: a deliberately
// non-idempotent applier (it emits a fresh marker node on every application)
// produces a DIFFERENT snapshot when the dirty window forces a re-projection
// than it does with no crash. If the harness could not observe that divergence,
// it could not catch a non-idempotent recovery — the exact bug class R-14 exists
// to guard.
func TestCrashReplayCatchesDuplicateProjection(t *testing.T) {
	t.Parallel()

	items := loadItems(t)

	baseCtx, baseCancel := context.WithTimeout(context.Background(), 30*time.Second)
	baseline, err := crashreplay.RunToCompletion(baseCtx, crashreplay.Config{
		Items: schedulereplay.ScheduleInOrder(items),
		Apply: newCountingApplier(),
	})
	baseCancel()
	if err != nil {
		t.Fatalf("RunToCompletion baseline (counting applier): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := crashreplay.RunWithCrash(ctx, crashreplay.Config{
		Items: schedulereplay.ScheduleInOrder(items),
		Apply: newCountingApplier(),
	}, crashreplay.CrashPoint{Kind: crashreplay.CrashAfterApply, After: 1})
	if err != nil {
		t.Fatalf("RunWithCrash (counting applier): %v", err)
	}
	if bytes.Equal(baseline, out.Snapshot) {
		t.Fatal("non-idempotent applier produced an identical snapshot after a crash; the harness cannot catch duplicate projection (teeth missing)")
	}
}

// TestRunWithCrashFailsWhenCrashNeverTriggers proves the harness fails loudly
// when the configured crash point is unreachable (e.g. it would fire after the
// schedule has already drained), rather than silently reporting a green no-crash
// run as if recovery had been exercised.
func TestRunWithCrashFailsWhenCrashNeverTriggers(t *testing.T) {
	t.Parallel()

	items := loadItems(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := crashreplay.RunWithCrash(ctx, crashreplay.Config{Items: schedulereplay.ScheduleInOrder(items)},
		crashreplay.CrashPoint{Kind: crashreplay.CrashAfterApply, After: len(items) + 5})
	if err == nil {
		t.Fatalf("expected an error when the crash point is unreachable, got crashed=%v of %d snapshot bytes",
			out.Report.Crashed, len(out.Snapshot))
	}
}

// newCountingApplier returns a deliberately non-idempotent applier: it upserts
// each work item's real nodes and edges, then records a unique marker node per
// application of an item. Applying an item once yields one marker; a recovery
// that re-applies an already-projected item yields a second marker, so the
// canonical snapshot reveals the duplicate projection. Each call returns a fresh
// applier with its own per-item counter so independent runs do not share state.
func newCountingApplier() schedulereplay.Applier {
	applied := map[string]int{}
	return func(g *schedulereplay.Graph, item schedulereplay.WorkItem) {
		schedulereplay.ApplyCanonical(g, item)
		applied[item.IntentID]++
		g.UpsertNode(schedulereplay.Node{
			Label: "ApplyMarker",
			ID:    fmt.Sprintf("%s#%s", item.IntentID, strconv.Itoa(applied[item.IntentID])),
		})
	}
}
