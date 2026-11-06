// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
)

const (
	testFenceEvidenceCommit = "1111111111111111111111111111111111111111"
	testFenceScope          = "go-reachability://repository:r_00000000"
)

// fakeGoEvidenceFencer records fences. affected overrides the count it reports,
// which a real store can return short when a successor won the race.
type fakeGoEvidenceFencer struct {
	calls      int
	observedAt time.Time
	fences     []scannercomponent.StaleEvidenceFence
	affected   *int
	err        error
	onFence    func()
}

func (f *fakeGoEvidenceFencer) FenceStaleGoEvidence(
	_ context.Context,
	fences []scannercomponent.StaleEvidenceFence,
	observedAt time.Time,
) (scannercomponent.StaleEvidenceFenceResult, error) {
	f.calls++
	f.observedAt = observedAt
	f.fences = append(f.fences, fences...)
	if f.onFence != nil {
		f.onFence()
	}
	if f.affected != nil {
		return scannercomponent.StaleEvidenceFenceResult{Fenced: *f.affected}, f.err
	}
	return scannercomponent.StaleEvidenceFenceResult{Fenced: len(fences), HoldersReopened: 2 * len(fences)}, f.err
}

// fenceFleet returns two repositories at testGoCommit. The first holds evidence
// of an older commit it left age before now.
func fenceFleet(now time.Time, age time.Duration) []goFleetRepository {
	fleet := goFleet(2)
	fleet[0].HasGoEvidence = true
	fleet[0].EvidenceGenerationID = "gen-x"
	fleet[0].EvidenceCommitSHA = testFenceEvidenceCommit
	fleet[0].EvidenceLeftAt = now.Add(-age)
	return fleet
}

func fenceInstanceService(
	t *testing.T,
	now *time.Time,
	store *fakeStore,
	fleet []goFleetRepository,
	fencer *fakeGoEvidenceFencer,
) Service {
	t.Helper()
	service := goModulesService(t, now, store, &fakeGoModuleRepositoryReader{fleet: fleet}, `{"mode":"broad"}`)
	service.GoEvidenceFencer = fencer
	return service
}

// TestGoModulesFenceRunsBeforePlanningAndStillPlans is decision 4.1 and 1.3: the
// fence runs on the page before the plan is admitted, and the repository is still
// planned, because only a claim can land the successor.
func TestGoModulesFenceRunsBeforePlanningAndStillPlans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	fencer := &fakeGoEvidenceFencer{}
	runsAtFence := -1
	fencer.onFence = func() { runsAtFence = len(store.createdRuns) }
	service := fenceInstanceService(t, &now, store, fenceFleet(now, 3*time.Hour), fencer)
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	want := scannercomponent.StaleEvidenceFence{ScopeID: testFenceScope, GenerationID: "gen-x"}
	if len(fencer.fences) != 1 || fencer.fences[0] != want {
		t.Fatalf("fences = %+v, want [%+v]", fencer.fences, want)
	}
	if runsAtFence != 0 {
		t.Fatalf("runs at fence time = %d, want 0: the fence must run before admission", runsAtFence)
	}
	if _, ok := enqueuedScopes(store)[testFenceScope]; !ok {
		t.Fatalf("enqueued = %v, want the fenced repository still planned", keysOf(enqueuedScopes(store)))
	}
}

// TestGoModulesFenceUsesTheDeriveInterval proves the bound uses the derive block's
// scan_interval, the value the rotation uses, not the reconcile interval. Skew of 25h
// is inside two 24h rotations, and far outside two 1h ticks.
func TestGoModulesFenceUsesTheDeriveInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	const dailyBlock = `{"derive_from_go_modules":{"enabled":true,"scan_interval":"24h"}}`
	fencer := &fakeGoEvidenceFencer{}
	service := fenceInstanceService(t, &now, &fakeStore{}, fenceFleet(now, 25*time.Hour), fencer)
	instance := goModulesInstance(now, goModulesActivationConfiguration(dailyBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(fencer.fences) != 0 {
		t.Fatalf("fences = %+v, want none inside two rotation periods", fencer.fences)
	}
}

// TestGoModulesFenceBoundUsesThePagesSlotCount is the large-fleet case of Amendment 1.
// With 1000 candidates each repository is visited once per four intervals, so skew of
// three intervals is healthy rotation and must not fence, while skew past eight must.
func TestGoModulesFenceBoundUsesThePagesSlotCount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	skewed := func(age time.Duration) []goFleetRepository {
		fleet := goFleet(1000)
		for i := range fleet {
			fleet[i].HasGoEvidence = true
			fleet[i].EvidenceGenerationID = fmt.Sprintf("gen-%d", i)
			fleet[i].EvidenceCommitSHA = testFenceEvidenceCommit
			fleet[i].EvidenceLeftAt = now.Add(-age)
		}
		return fleet
	}
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	inside := &fakeGoEvidenceFencer{}
	if err := scheduleGoModules(t, fenceInstanceService(t, &now, &fakeStore{}, skewed(3*time.Hour), inside), now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(inside.fences) != 0 {
		t.Fatalf("fences = %d, want none inside two rotations of a four-slot fleet", len(inside.fences))
	}

	past := &fakeGoEvidenceFencer{}
	if err := scheduleGoModules(t, fenceInstanceService(t, &now, &fakeStore{}, skewed(9*time.Hour), past), now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(past.fences) == 0 || len(past.fences) > scannercomponent.GoModulePageSize {
		t.Fatalf("fences = %d, want one page of skewed repositories", len(past.fences))
	}
	if !past.observedAt.Equal(now) {
		t.Fatalf("observedAt = %v, want the tick's clock %v", past.observedAt, now)
	}
}

// TestGoModulesFenceSkipsAnAlreadyPlannedBucket proves a repeat pass stays one
// primary-key probe: it reads no candidates, so it fences nothing.
func TestGoModulesFenceSkipsAnAlreadyPlannedBucket(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: fenceFleet(now, 3*time.Hour)}
	store := &fakeStore{}
	fencer := &fakeGoEvidenceFencer{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	service.GoEvidenceFencer = fencer
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("first pass error = %v", err)
	}
	reader.planned = map[string]bool{reader.filters[0].RunID: true}
	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("repeat pass error = %v", err)
	}
	if fencer.calls != 1 {
		t.Fatalf("fencer calls = %d, want 1: a repeat pass must not fence", fencer.calls)
	}
}

// TestGoModulesFenceFailureLeavesTheBucketRetryable proves a failed fence creates
// no run. A run would make the next pass stop at the planned probe, so the fence
// would not be retried until the next rotation. The partial count still reaches
// the metric, because those generations were retracted.
func TestGoModulesFenceFailureLeavesTheBucketRetryable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	boom := errors.New("lock timeout")
	partial := 1
	fencer := &fakeGoEvidenceFencer{err: boom, affected: &partial}
	counter := &fakeActivationDerivedScopeCounter{}
	service := fenceInstanceService(t, &now, store, fenceFleet(now, 3*time.Hour), fencer)
	service.ActivationDerivedScopesFenced = counter
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	err := scheduleGoModules(t, service, now, instance)
	if !errors.Is(err, boom) {
		t.Fatalf("schedule error = %v, want the fence error", err)
	}
	if len(store.createdRuns) != 0 {
		t.Fatalf("runs = %d, want 0 after a failed fence", len(store.createdRuns))
	}
	if len(counter.adds) != 1 || counter.adds[0].value != 1 {
		t.Fatalf("counter adds = %+v, want the one retracted generation", counter.adds)
	}
}

// TestGoModulesFenceRequiresAFencer fails closed: an activation that plans Go
// scopes with no fencer would leave skewed evidence active with no bound.
func TestGoModulesFenceRequiresAFencer(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	service := goModulesService(t, &now, &fakeStore{}, &fakeGoModuleRepositoryReader{fleet: goFleet(1)}, `{"mode":"broad"}`)
	service.GoEvidenceFencer = nil
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	err := scheduleGoModules(t, service, now, instance)
	if err == nil || !strings.Contains(err.Error(), "fencer") {
		t.Fatalf("schedule error = %v, want a missing-fencer error", err)
	}
}

// TestGoModulesFenceRetractsAnUnusableRemoteOnceTheBoundPasses is the acceptance
// case of decision 3.2. An unusable remote is skipped by name on the first visit
// and keeps its evidence inside the bound. Past the bound the fence retracts it,
// though the repository still cannot be planned.
func TestGoModulesFenceRetractsAnUnusableRemoteOnceTheBoundPasses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		age        time.Duration
		wantFences int
	}{
		{name: "inside the bound keeps the evidence", age: time.Hour},
		{name: "past the bound retracts it", age: 3 * time.Hour, wantFences: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
			fleet := fenceFleet(now, tc.age)
			fleet[0].RemoteURL = ""
			store := &fakeStore{}
			fencer := &fakeGoEvidenceFencer{}
			var logs bytes.Buffer
			service := fenceInstanceService(t, &now, store, fleet, fencer)
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

			if err := scheduleGoModules(t, service, now, instance); err != nil {
				t.Fatalf("schedule error = %v", err)
			}
			if len(fencer.fences) != tc.wantFences {
				t.Fatalf("fences = %+v, want %d", fencer.fences, tc.wantFences)
			}
			if want := `"skipped_unsupported_remote_with_evidence":1`; !strings.Contains(logs.String(), want) {
				t.Fatalf("logs = %s, want %s", logs.String(), want)
			}
			if _, planned := enqueuedScopes(store)[testFenceScope]; planned {
				t.Fatal("a repository with no usable remote was planned")
			}
		})
	}
}

// TestGoModulesFenceTelemetryHoldsNoRepositoryValue pins decision 4.7. The counter
// has exactly its three labels and counts generations actually retracted, not
// candidates, and neither it nor the log line holds a repository value.
func TestGoModulesFenceTelemetryHoldsNoRepositoryValue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	fleet := fenceFleet(now, 3*time.Hour)
	fleet[1].HasGoEvidence = true
	fleet[1].EvidenceGenerationID = "gen-no-commit"
	counter := &fakeActivationDerivedScopeCounter{}
	var logs bytes.Buffer
	service := fenceInstanceService(t, &now, &fakeStore{}, fleet, &fakeGoEvidenceFencer{})
	service.ActivationDerivedScopesFenced = counter
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(counter.adds) != 1 || counter.adds[0].value != 1 {
		t.Fatalf("counter adds = %+v, want one add of 1", counter.adds)
	}
	labels := counter.adds[0].attributes
	if len(labels) != 3 || labels["collector_kind"] != "scanner_worker" ||
		labels["ecosystem"] != "go" || labels["reason"] != "stale_evidence" {
		t.Fatalf("counter labels = %v", labels)
	}
	for _, want := range []string{
		`"fenced_stale_evidence":1`, `"fence_holders_reopened":2`,
		`"fence_candidates_skipped_no_commit":1`, `"fence_history_pruned":0`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs = %s, want %s", logs.String(), want)
		}
	}
	encoded, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"repository:r_", "github.com", "go-reachability://", "gen-x", testFenceEvidenceCommit} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("counter labels %s carry %q", encoded, leak)
		}
		if strings.Contains(logs.String(), leak) {
			t.Fatalf("logs %s carry %q", logs.String(), leak)
		}
	}
}

// TestGoModulesFenceCountsOnlyGenerationsItRetracted proves a fence that lost the
// race to a successor counts nothing: the metric means evidence was retracted.
func TestGoModulesFenceCountsOnlyGenerationsItRetracted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	none := 0
	counter := &fakeActivationDerivedScopeCounter{}
	var logs bytes.Buffer
	service := fenceInstanceService(t, &now, &fakeStore{}, fenceFleet(now, 3*time.Hour), &fakeGoEvidenceFencer{affected: &none})
	service.ActivationDerivedScopesFenced = counter
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(counter.adds) != 0 {
		t.Fatalf("counter adds = %+v, want none", counter.adds)
	}
	if want := `"fenced_stale_evidence":0`; !strings.Contains(logs.String(), want) {
		t.Fatalf("logs = %s, want %s", logs.String(), want)
	}
}
