// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9776 PostgreSQL 18.6 full-bootstrap proof that least-recently-planned slot progress survives an operator changing scan_interval on an enabled instance: raising it keeps the rotation moving and the compare-and-set moving rows, lowering it reports the true oldest-slot age instead of decades; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// progIntervalWindow is one reconcile window at a given scan interval: the
// window start the coordinator truncated its clock to, and the interval-relative
// bucket index the clock-slot statement still takes as its rotation bucket.
type progIntervalWindow struct {
	interval time.Duration
	start    time.Time
}

func (w progIntervalWindow) bucket() int64 {
	return w.start.UnixNano() / int64(w.interval)
}

func (w progIntervalWindow) selector() workflow.OwnedTargetSlotSelector {
	selector := progLiveSelector(0)
	selector.WindowStartUnix = w.start.Unix()
	return selector
}

func (w progIntervalWindow) advance(slots []int) workflow.OwnedTargetSlotAdvance {
	advance := progLiveAdvanceFor(slots, 0)
	advance.WindowStartUnix = w.start.Unix()
	return advance
}

// progIntervalStep reads the window, advances its covered slots through the
// store, and returns the page with the advance result.
func progIntervalStep(
	ctx context.Context,
	t *testing.T,
	facts *FactStore,
	control *WorkflowControlStore,
	w progIntervalWindow,
) (workflow.OwnedPackageDependencyTargetPage, workflow.SlotAdvanceResult) {
	t.Helper()
	page := readSlotLiveWindowFilter(ctx, t, facts, workflow.OwnedPackageDependencyTargetFilter{
		RotationBucket: w.bucket(), SlotProgress: w.selector(),
	})
	result, err := control.AdvanceOwnedTargetSlotProgress(ctx, w.advance(page.Selection.AdvanceSlots))
	if err != nil {
		t.Fatalf("advance window %s (interval %s): %v", w.start.Format(time.RFC3339), w.interval, err)
	}
	return page, result
}

// TestOwnedTargetSlotProgressSurvivesScanIntervalChangeLive pins the second
// review's P1 for #9776. Progress was keyed on unix time divided by the scan
// interval, so doubling the interval halved every new key below every stored
// one: the compare-and-set moved nothing, the same slots were planned forever,
// and the age gauge read zero. Halving it doubled the keys and reported a
// decades-old slot. Progress now records the window start time, which means
// the same thing under any interval.
func TestOwnedTargetSlotProgressSurvivesScanIntervalChangeLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_prog_iv_9776", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, slotLiveFixture(slotLiveMajorityTargets, slotLiveMinorityTargets)); err != nil {
		t.Fatalf("seed owned-package slot fixture: %v", err)
	}
	facts := NewFactStore(SQLDB{DB: db})
	control := NewWorkflowControlStore(SQLDB{DB: db})
	total := slotLiveMajorityTargets + slotLiveMinorityTargets
	groups := int(workflow.OwnedPackageTargetSlotCount(int64(total), slotLiveTargetLimit))
	origin := time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)
	const warmWindows = 40 // more than one rotation of `groups` windows

	t.Run("raising scan_interval keeps the rotation moving", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		for i := 0; i < warmWindows; i++ {
			w := progIntervalWindow{interval: 30 * time.Minute, start: origin.Add(time.Duration(i) * 30 * time.Minute)}
			if _, result := progIntervalStep(ctx, t, facts, control, w); result.LostRace() {
				t.Fatalf("warm window %d lost the race: %+v", i, result)
			}
		}
		// The operator doubles the interval. The 30 minute windows ended at
		// 19:30, the next window opens at 20:00, and group (warmWindows mod
		// groups) is the least recently planned.
		next := origin.Add(time.Duration(warmWindows) * 30 * time.Minute)
		var previous []int
		for j := 0; j < 6; j++ {
			w := progIntervalWindow{interval: time.Hour, start: next.Add(time.Duration(j) * time.Hour)}
			page, result := progIntervalStep(ctx, t, facts, control, w)
			wantGroup := (warmWindows + j) % groups
			if page.Selection.Mode != workflow.SlotSelectionOldest ||
				!slices.Equal(page.Selection.PickedSlots, progLiveGroup(wantGroup, groups)) {
				t.Fatalf("hour window %d: mode %q picked %v, want oldest group %d %v (rotation pinned)",
					j, page.Selection.Mode, page.Selection.PickedSlots, wantGroup, progLiveGroup(wantGroup, groups))
			}
			if result.Moved != len(page.Selection.AdvanceSlots) || len(page.Selection.AdvanceSlots) == 0 {
				t.Fatalf("hour window %d: compare-and-set moved %d of %d rows (a stalled rotation moves none)",
					j, result.Moved, len(page.Selection.AdvanceSlots))
			}
			if slices.Equal(previous, page.Selection.PickedSlots) {
				t.Fatalf("hour window %d re-planned the previous slots %v", j, previous)
			}
			previous = page.Selection.PickedSlots
			if j == 0 {
				// Group 8 was last planned in 30 minute window 8, at 04:00; the
				// new window opens at 20:00.
				if want := 16 * time.Hour; !page.Selection.Warm() || page.Selection.OldestSlotAge != want {
					t.Fatalf("first hour window: warm=%v age=%v, want warm and %v", page.Selection.Warm(), page.Selection.OldestSlotAge, want)
				}
			}
		}
	})

	t.Run("lowering scan_interval reports the true oldest slot age", func(t *testing.T) {
		progLiveReset(ctx, t, db)
		for i := 0; i < warmWindows; i++ {
			w := progIntervalWindow{interval: time.Hour, start: origin.Add(time.Duration(i) * time.Hour)}
			if _, result := progIntervalStep(ctx, t, facts, control, w); result.LostRace() {
				t.Fatalf("warm window %d lost the race: %+v", i, result)
			}
		}
		// Group 8 was last planned in hour window 8 (08:00); the operator halves
		// the interval and the next window opens at hour 40.
		w := progIntervalWindow{interval: 30 * time.Minute, start: origin.Add(time.Duration(warmWindows) * time.Hour)}
		page, result := progIntervalStep(ctx, t, facts, control, w)
		if want := 32 * time.Hour; !page.Selection.Warm() || page.Selection.OldestSlotAge != want {
			t.Fatalf("warm=%v age=%v, want warm and %v (a doubled bucket index reads decades)",
				page.Selection.Warm(), page.Selection.OldestSlotAge, want)
		}
		if page.Selection.Mode != workflow.SlotSelectionOldest || result.Moved != len(page.Selection.AdvanceSlots) {
			t.Fatalf("mode %q moved %d of %d, want oldest and every slot moved",
				page.Selection.Mode, result.Moved, len(page.Selection.AdvanceSlots))
		}
	})
}

// progLiveGroup returns the virtual slots of the n-th group of a rotation of
// groups windows: the slots a cold table hands out in window order.
func progLiveGroup(n, groups int) []int {
	per := workflow.OwnedPackageVirtualSlotCount / groups
	return progLiveSeq(n*per, (n+1)*per)
}
