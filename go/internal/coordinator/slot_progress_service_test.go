// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const slotProgressOn = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"slot_progress":true}}`

// slotProgressFakeStore is a Store that also advances owned-target slot
// progress, recording every advance the coordinator asks for. moved is the
// number of rows each advance reports moved (a negative value moves them all).
type slotProgressFakeStore struct {
	*fakeStore
	withItems []workflow.OwnedTargetSlotAdvance
	only      []workflow.OwnedTargetSlotAdvance
	moved     int
}

func (s *slotProgressFakeStore) result(advance workflow.OwnedTargetSlotAdvance) workflow.SlotAdvanceResult {
	moved := s.moved
	if moved < 0 {
		moved = len(advance.Slots)
	}
	return workflow.SlotAdvanceResult{Requested: len(advance.Slots), Moved: moved}
}

func (s *slotProgressFakeStore) CreateRunWithWorkItemsAdvancingSlotProgress(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
	advance workflow.OwnedTargetSlotAdvance,
) (workflow.RunAdmission, workflow.SlotAdvanceResult, error) {
	s.withItems = append(s.withItems, advance)
	admission, err := s.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	return admission, s.result(advance), err
}

func (s *slotProgressFakeStore) AdvanceOwnedTargetSlotProgress(
	_ context.Context,
	advance workflow.OwnedTargetSlotAdvance,
) (workflow.SlotAdvanceResult, error) {
	s.only = append(s.only, advance)
	return s.result(advance), nil
}

func slotProgressService(
	now time.Time,
	configuration string,
	store Store,
	reader *fakeOwnedPackageTargetReader,
	metrics *fakeMetrics,
	logs *bytes.Buffer,
) Service {
	instance := testServiceVulnerabilityIntelligenceInstance(now)
	instance.Configuration = configuration
	cfg := activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, configuration)
	service := Service{
		Config:                           cfg,
		Store:                            store,
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         reader,
		Metrics:                          metrics,
		Clock:                            func() time.Time { return now },
	}
	if logs != nil {
		service.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	return service
}

func newSlotProgressStore(now time.Time, moved int, configuration string) *slotProgressFakeStore {
	instance := testServiceVulnerabilityIntelligenceInstance(now)
	instance.Configuration = configuration
	return &slotProgressFakeStore{fakeStore: &fakeStore{instances: []workflow.CollectorInstance{instance}}, moved: moved}
}

func runOneReconcile(t *testing.T, service Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func slotOutcomes(metrics *fakeMetrics) []string {
	var outcomes []string
	for _, observation := range metrics.ownedTargetSlotObserved {
		outcomes = append(outcomes, observation.Outcomes...)
	}
	return outcomes
}

// TestServiceSlotProgressFlagOffKeepsTheClockSlot is the rollback proof (#9776):
// without the flag the read gets a zero selector, the plain admission path
// runs, and no slot signal is recorded.
func TestServiceSlotProgressFlagOffKeepsTheClockSlot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	const flagOff = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`
	store := newSlotProgressStore(now, -1, flagOff)
	reader := &fakeOwnedPackageTargetReader{targets: makeOwnedNPMTargets(3), window: workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1, SlotSize: 3, RowsRead: 3}}
	metrics := &fakeMetrics{}
	service := slotProgressService(now,
		flagOff, store, reader, metrics, nil)
	runOneReconcile(t, service)

	if len(reader.requests) != 1 || reader.requests[0].SlotProgress.Enabled() {
		t.Fatalf("read requests = %#v, want one with a disabled slot selector", reader.requests)
	}
	if len(store.withItems)+len(store.only) != 0 {
		t.Fatalf("slot progress advanced %d times with the flag off, want 0", len(store.withItems)+len(store.only))
	}
	if len(metrics.ownedTargetSlotObserved) != 0 {
		t.Fatalf("slot observations = %d with the flag off, want 0", len(metrics.ownedTargetSlotObserved))
	}
	if len(store.enqueuedItems) == 0 {
		t.Fatal("flag off planned no work; the clock path must be unchanged")
	}
}

// TestServiceSlotProgressAdvancesPickedSlotsWithAdmission covers the oldest and
// advanced outcomes and the selector the coordinator hands the reader.
func TestServiceSlotProgressAdvancesPickedSlotsWithAdmission(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	store := newSlotProgressStore(now, -1, slotProgressOn)
	reader := &fakeOwnedPackageTargetReader{
		targets: makeOwnedNPMTargets(3),
		window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1, SlotSize: 3, RowsRead: 3},
		selection: workflow.OwnedTargetSlotSelection{
			Mode: workflow.SlotSelectionOldest, PickedSlots: []int{3, 9}, AdvanceSlots: []int{3, 9},
			ProgressRows: workflow.OwnedPackageVirtualSlotCount, OldestPlannedWindowStartUnix: 10, OldestSlotAge: 90 * time.Minute,
		},
	}
	metrics := &fakeMetrics{}
	service := slotProgressService(now, slotProgressOn, store, reader, metrics, nil)
	runOneReconcile(t, service)

	want := workflow.OwnedTargetSlotSelector{
		CollectorInstanceID: "collector-vulnerability-intelligence",
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		WindowStartUnix:     schedule.DerivedTargetWindowStartUnix(now, time.Hour),
	}
	if got := reader.requests[0].SlotProgress; got != want {
		t.Fatalf("selector = %#v, want %#v", got, want)
	}
	if len(store.withItems) != 1 || len(store.only) != 0 {
		t.Fatalf("advances with items %d, advance-only %d, want 1 and 0", len(store.withItems), len(store.only))
	}
	advance := store.withItems[0]
	if !slices.Equal(advance.Slots, []int{3, 9}) || advance.CollectorInstanceID != want.CollectorInstanceID ||
		advance.DerivationKind != workflow.SlotDerivationKindVulnerability ||
		advance.CollectorKind != scope.CollectorVulnerabilityIntelligence ||
		advance.WindowStartUnix != schedule.DerivedTargetWindowStartUnix(now, time.Hour) {
		t.Fatalf("advance = %#v, want slots [3 9], window start %d, this instance", advance, schedule.DerivedTargetWindowStartUnix(now, time.Hour))
	}
	if got, want := slotOutcomes(metrics), []string{workflow.SlotSelectionOldest, workflow.SlotSelectionAdvanced}; !slices.Equal(got, want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
	last := metrics.ownedTargetSlotObserved[0]
	if !last.AgeKnown || last.OldestSlotAge != 90*time.Minute {
		t.Fatalf("age observation = %#v, want known 90m", last)
	}
}

// TestServiceSlotProgressAdvancesAnEmptyPick is T7 at the coordinator: a pick
// that yields no work item must still advance in a short transaction, and the
// outcome is empty_slot. Without it the empty slot is oldest at every tick.
func TestServiceSlotProgressAdvancesAnEmptyPick(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	store := newSlotProgressStore(now, -1, slotProgressOn)
	reader := &fakeOwnedPackageTargetReader{
		window: workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1024},
		selection: workflow.OwnedTargetSlotSelection{
			Mode: workflow.SlotSelectionOldest, PickedSlots: []int{17}, AdvanceSlots: []int{17},
		},
	}
	metrics := &fakeMetrics{}
	service := slotProgressService(now, slotProgressOn, store, reader, metrics, nil)
	runOneReconcile(t, service)

	if len(store.withItems) != 0 || len(store.only) != 1 || !slices.Equal(store.only[0].Slots, []int{17}) {
		t.Fatalf("advance-only calls = %#v (with items %d), want one advance of slot 17", store.only, len(store.withItems))
	}
	if got, want := slotOutcomes(metrics), []string{workflow.SlotSelectionOldest, workflow.SlotSelectionEmptySlot, workflow.SlotSelectionAdvanced}; !slices.Equal(got, want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
}

// TestServiceSlotProgressOutcomesAcrossRacesAndRepeats pins the P2-1 review
// finding: the reconcile ticker runs many times per bucket, so every repeat
// tick reads same_bucket_reuse and moves zero rows. lost_race and empty_slot
// describe a fresh pick only; a repeat is already labelled by its mode, or an
// operator would see about sixty lost_race per bucket on a healthy instance.
func TestServiceSlotProgressOutcomesAcrossRacesAndRepeats(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	withTargets := func(mode string) *fakeOwnedPackageTargetReader {
		return &fakeOwnedPackageTargetReader{
			targets: makeOwnedNPMTargets(2),
			window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 2, SlotCount: 1, SlotSize: 2, RowsRead: 2},
			selection: workflow.OwnedTargetSlotSelection{
				Mode: mode, PickedSlots: []int{5}, AdvanceSlots: []int{5},
			},
		}
	}
	emptyPick := func(mode string) *fakeOwnedPackageTargetReader {
		return &fakeOwnedPackageTargetReader{
			window: workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1024},
			selection: workflow.OwnedTargetSlotSelection{
				Mode: mode, PickedSlots: []int{17}, AdvanceSlots: []int{17},
			},
		}
	}
	cases := []struct {
		name   string
		moved  int
		reader *fakeOwnedPackageTargetReader
		want   []string
	}{
		{
			"fresh pick that another coordinator advanced first", 0, withTargets(workflow.SlotSelectionOldest),
			[]string{workflow.SlotSelectionOldest, workflow.SlotSelectionLostRace},
		},
		{
			"repeat tick in the same bucket", 0, withTargets(workflow.SlotSelectionSameBucketReuse),
			[]string{workflow.SlotSelectionSameBucketReuse},
		},
		{
			"fresh empty pick that lost the race", 0, emptyPick(workflow.SlotSelectionOldest),
			[]string{workflow.SlotSelectionOldest, workflow.SlotSelectionEmptySlot, workflow.SlotSelectionLostRace},
		},
		{
			"repeat tick of an empty pick", 0, emptyPick(workflow.SlotSelectionSameBucketReuse),
			[]string{workflow.SlotSelectionSameBucketReuse},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newSlotProgressStore(now, tc.moved, slotProgressOn)
			metrics := &fakeMetrics{}
			runOneReconcile(t, slotProgressService(now, slotProgressOn, store, tc.reader, metrics, nil))
			if got := slotOutcomes(metrics); !slices.Equal(got, tc.want) {
				t.Fatalf("outcomes = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServiceSlotProgressReportsTruncationAndWarmingAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	store := newSlotProgressStore(now, -1, slotProgressOn)
	reader := &fakeOwnedPackageTargetReader{
		targets: makeOwnedNPMTargets(3),
		window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 9, SlotCount: 1, SlotSize: 9, RowsRead: 3},
		selection: workflow.OwnedTargetSlotSelection{
			Mode: workflow.SlotSelectionOldest, PickedSlots: []int{3}, AdvanceSlots: []int{3}, HeadTruncated: true,
			ProgressRows: 10, // 10 of 1,024 slots planned: the first rotation is still warming
		},
	}
	metrics := &fakeMetrics{}
	service := slotProgressService(now, slotProgressOn, store, reader, metrics, nil)
	runOneReconcile(t, service)

	if !slices.Contains(slotOutcomes(metrics), workflow.SlotSelectionTruncated) {
		t.Fatalf("outcomes = %v, want %q for an oversize head slot", slotOutcomes(metrics), workflow.SlotSelectionTruncated)
	}
	if metrics.ownedTargetSlotObserved[0].AgeKnown {
		t.Fatal("age reported as known while only 10 of 1,024 slots have been planned")
	}
}

// TestServiceSlotProgressClockFallbackSignalsAndDoesNotAdvance is the P > V
// case (5x and 10x at target_limit 100): the read keeps the clock slot, nothing
// advances, and the operator sees the outcome and the status reason.
func TestServiceSlotProgressClockFallbackSignalsAndDoesNotAdvance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	store := newSlotProgressStore(now, -1, slotProgressOn)
	reader := &fakeOwnedPackageTargetReader{
		targets: makeOwnedNPMTargets(3),
		window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 163630, SlotCount: 4096, SlotSize: 40, RowsRead: 3},
		selection: workflow.OwnedTargetSlotSelection{
			Mode: workflow.SlotSelectionClockFallback,
		},
	}
	metrics := &fakeMetrics{}
	var logs bytes.Buffer
	service := slotProgressService(now, slotProgressOn, store, reader, metrics, &logs)
	runOneReconcile(t, service)

	if len(store.withItems)+len(store.only) != 0 {
		t.Fatal("clock fallback advanced slot progress; it must not")
	}
	if got, want := slotOutcomes(metrics), []string{workflow.SlotSelectionClockFallback}; !slices.Equal(got, want) {
		t.Fatalf("outcomes = %v, want %v", got, want)
	}
	if !strings.Contains(logs.String(), `"reason":"owned_target_progress_clock_fallback"`) {
		t.Fatalf("no clock-fallback status reason in logs: %s", logs.String())
	}
}

func TestServiceSlotProgressWarnsWhenOldestSlotExceedsMaxStaleness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	store := newSlotProgressStore(now, -1, slotProgressOn)
	reader := &fakeOwnedPackageTargetReader{
		targets: makeOwnedNPMTargets(3),
		window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1, SlotSize: 3, RowsRead: 3},
		selection: workflow.OwnedTargetSlotSelection{
			Mode: workflow.SlotSelectionOldest, PickedSlots: []int{3}, AdvanceSlots: []int{3},
			ProgressRows: workflow.OwnedPackageVirtualSlotCount, OldestPlannedWindowStartUnix: 1, OldestSlotAge: 30 * time.Hour,
		},
	}
	metrics := &fakeMetrics{}
	var logs bytes.Buffer
	service := slotProgressService(now, slotProgressOn, store, reader, metrics, &logs)
	runOneReconcile(t, service)

	if !strings.Contains(logs.String(), `"reason":"owned_target_slot_age_exceeds_max_staleness"`) {
		t.Fatalf("no slot-age breach warning with a 30h oldest slot against the 24h default bound: %s", logs.String())
	}
}

// TestServiceSlotProgressRequiresAProgressStore fails loudly, not silently
// back to the clock, when the flag is on but the store cannot advance progress.
func TestServiceSlotProgressRequiresAProgressStore(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 2, 15, 0, 0, 0, time.UTC)
	instance := testServiceVulnerabilityIntelligenceInstance(now)
	reader := &fakeOwnedPackageTargetReader{
		targets: makeOwnedNPMTargets(3),
		window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 3, SlotCount: 1, SlotSize: 3, RowsRead: 3},
		selection: workflow.OwnedTargetSlotSelection{
			Mode: workflow.SlotSelectionOldest, PickedSlots: []int{3}, AdvanceSlots: []int{3},
		},
	}
	service := slotProgressService(now, slotProgressOn, &fakeStore{instances: []workflow.CollectorInstance{instance}}, reader, &fakeMetrics{}, nil)
	err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{
		func() workflow.CollectorInstance { i := instance; i.Configuration = slotProgressOn; return i }(),
	})
	if err == nil || !strings.Contains(err.Error(), "slot progress") {
		t.Fatalf("error = %v, want one naming slot progress", err)
	}
}
