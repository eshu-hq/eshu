// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// slotProgressConfigWithInterval is the slot-progress configuration with an
// explicit scan_interval, the knob an operator turns to widen or narrow the
// reconcile window.
func slotProgressConfigWithInterval(interval string) string {
	return `{"scan_interval":"` + interval + `","derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"slot_progress":true}}`
}

// slotProgressWindowAt runs one reconcile at now under the given scan_interval
// and returns the selector the reader received and the advance the store
// recorded.
func slotProgressWindowAt(
	t *testing.T,
	now time.Time,
	interval string,
) (workflow.OwnedTargetSlotSelector, workflow.OwnedTargetSlotAdvance) {
	t.Helper()
	configuration := slotProgressConfigWithInterval(interval)
	store := newSlotProgressStore(now, -1, configuration)
	reader := &fakeOwnedPackageTargetReader{
		targets: makeOwnedNPMTargets(3),
		window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1, SlotSize: 3, RowsRead: 3},
		selection: workflow.OwnedTargetSlotSelection{
			Mode: workflow.SlotSelectionOldest, PickedSlots: []int{3}, AdvanceSlots: []int{3},
		},
	}
	runOneReconcile(t, slotProgressService(now, configuration, store, reader, &fakeMetrics{}, nil))
	if len(reader.requests) != 1 || len(store.withItems) != 1 {
		t.Fatalf("interval %s: %d reads and %d advances, want 1 and 1", interval, len(reader.requests), len(store.withItems))
	}
	return reader.requests[0].SlotProgress, store.withItems[0]
}

// TestServiceSlotProgressWindowStartIsIntervalInvariant pins the second
// review's P1 (#9776) at the coordinator seam. Progress used to be stored as
// unix time divided by the scan interval, so widening the interval put every
// new key below every stored key. The coordinator now hands the reader and the
// advance the start time of the window, which only moves forward in time.
func TestServiceSlotProgressWindowStartIsIntervalInvariant(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.November, 2, 10, 15, 0, 0, time.UTC)
	t.Run("raising scan_interval never moves the window start backward", func(t *testing.T) {
		t.Parallel()
		_, narrow := slotProgressWindowAt(t, base, "1h")
		selector, wide := slotProgressWindowAt(t, base.Add(2*time.Hour), "2h")
		if wide.WindowStartUnix <= narrow.WindowStartUnix {
			t.Fatalf("2h window start %d is not after the earlier 1h window start %d", wide.WindowStartUnix, narrow.WindowStartUnix)
		}
		if want := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC).Unix(); wide.WindowStartUnix != want || selector.WindowStartUnix != want {
			t.Fatalf("advance window start %d, selector window start %d, want both %d", wide.WindowStartUnix, selector.WindowStartUnix, want)
		}
	})
	t.Run("lowering scan_interval never moves the window start backward", func(t *testing.T) {
		t.Parallel()
		_, wide := slotProgressWindowAt(t, base, "2h")
		selector, narrow := slotProgressWindowAt(t, base.Add(2*time.Hour), "1h")
		if narrow.WindowStartUnix <= wide.WindowStartUnix {
			t.Fatalf("1h window start %d is not after the earlier 2h window start %d", narrow.WindowStartUnix, wide.WindowStartUnix)
		}
		if want := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC).Unix(); narrow.WindowStartUnix != want || selector.WindowStartUnix != want {
			t.Fatalf("advance window start %d, selector window start %d, want both %d", narrow.WindowStartUnix, selector.WindowStartUnix, want)
		}
	})
}

// TestServiceSlotProgressRepeatTickWritesNothing pins the third review note
// (#9776). A repeat tick inside one window reads same_bucket_reuse, and every
// row it picked is already at this window start, so the compare-and-set could
// only move zero rows. The coordinator must not take row locks or open an
// advisory-locked transaction for it, yet it still reports its mode so the
// counter shows the rotation is being visited.
func TestServiceSlotProgressRepeatTickWritesNothing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		reader *fakeOwnedPackageTargetReader
	}{
		{"repeat tick with work", &fakeOwnedPackageTargetReader{
			targets: makeOwnedNPMTargets(2),
			window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 2, SlotCount: 1, SlotSize: 2, RowsRead: 2},
			selection: workflow.OwnedTargetSlotSelection{
				Mode: workflow.SlotSelectionSameBucketReuse, PickedSlots: []int{5}, AdvanceSlots: []int{5},
			},
		}},
		{"repeat tick of an empty pick", &fakeOwnedPackageTargetReader{
			window: workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1024},
			selection: workflow.OwnedTargetSlotSelection{
				Mode: workflow.SlotSelectionSameBucketReuse, PickedSlots: []int{17}, AdvanceSlots: []int{17},
			},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newSlotProgressStore(now, -1, slotProgressOn)
			metrics := &fakeMetrics{}
			runOneReconcile(t, slotProgressService(now, slotProgressOn, store, tc.reader, metrics, nil))
			if len(store.withItems) != 0 || len(store.only) != 0 {
				t.Fatalf("a repeat tick asked for %d admission advances and %d advance-only transactions, want none",
					len(store.withItems), len(store.only))
			}
			if got := slotOutcomes(metrics); len(got) != 1 || got[0] != workflow.SlotSelectionSameBucketReuse {
				t.Fatalf("outcomes = %v, want only %q", got, workflow.SlotSelectionSameBucketReuse)
			}
		})
	}
}
