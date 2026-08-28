// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScannerWorkerWorkPlannerPlansSBOMGenerationTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 4, 20, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "scanner-worker-sbom",
		CollectorKind: scope.CollectorScannerWorker,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"analyzer":"sbom_generation","sbom_targets":[{
			"scope_id":"scanner-worker://repository/repository-corpus",
			"root_path":"/fixtures/repository-corpus",
			"subject_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111"
		}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := WorkPlanner{}.PlanScannerWorkerWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260604T200000Z",
	})
	if err != nil {
		t.Fatalf("PlanScannerWorkerWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorScannerWorker); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.CollectorKind, scope.CollectorScannerWorker; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := item.SourceSystem, string(scope.CollectorScannerWorker); got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "scanner-worker://repository/repository-corpus"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if !strings.HasPrefix(item.GenerationID, "scanner_worker:") {
		t.Fatalf("GenerationID = %q, want scanner_worker prefix", item.GenerationID)
	}
	if got, want := item.FairnessKey, "scanner_worker:scanner-worker-sbom:repository"; got != want {
		t.Fatalf("FairnessKey = %q, want %q", got, want)
	}
	if strings.Contains(run.RequestedScopeSet, "/fixtures/repository-corpus") {
		t.Fatalf("RequestedScopeSet leaked runtime-local root path: %s", run.RequestedScopeSet)
	}
	var requested struct {
		Analyzer string `json:"analyzer"`
		Targets  []struct {
			ScopeID    string `json:"scope_id"`
			TargetKind string `json:"target_kind"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := requested.Analyzer, "sbom_generation"; got != want {
		t.Fatalf("RequestedScopeSet analyzer = %q, want %q", got, want)
	}
	if got, want := requested.Targets[0].TargetKind, "repository"; got != want {
		t.Fatalf("RequestedScopeSet target_kind = %q, want %q", got, want)
	}
}

func TestScannerWorkerWorkPlannerPreservesTargetOrderAndStabilizesScopeMetadata(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 4, 20, 0, 0, 0, time.FixedZone("test-offset", -7*60*60))
	instance := workflow.CollectorInstance{
		InstanceID:    "scanner-worker-ordered",
		CollectorKind: scope.CollectorScannerWorker,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"analyzer":"sbom_generation","sbom_targets":[
			{"scope_id":"scanner-worker://repository/zeta","root_path":"/private/zeta"},
			{"scope_id":"scanner-worker://repository/alpha","root_path":"/private/alpha"}
		]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := WorkPlanner{}.PlanScannerWorkerWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260604T200000Z",
	})
	if err != nil {
		t.Fatalf("PlanScannerWorkerWork() error = %v, want nil", err)
	}
	wantRunID := "scanner_worker:scanner-worker-ordered:schedule:continuous-20260604T200000Z"
	if got := run.RunID; got != wantRunID {
		t.Fatalf("RunID = %q, want exact pre-move value %q", got, wantRunID)
	}
	wantRequestedScopeSet := `{"collector_instance_id":"scanner-worker-ordered","analyzer":"sbom_generation","targets":[{"scope_id":"scanner-worker://repository/alpha","target_kind":"repository"},{"scope_id":"scanner-worker://repository/zeta","target_kind":"repository"}]}`
	if got := run.RequestedScopeSet; got != wantRequestedScopeSet {
		t.Fatalf("RequestedScopeSet = %q, want exact pre-move value %q", got, wantRequestedScopeSet)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorScannerWorker); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := run.Status, workflow.RunStatusCollectionPending; got != want {
		t.Fatalf("run Status = %q, want %q", got, want)
	}
	wantTimestamp := observedAt.UTC()
	if !run.CreatedAt.Equal(wantTimestamp) || run.CreatedAt.Location() != time.UTC {
		t.Fatalf("run CreatedAt = %v (%v), want UTC %v", run.CreatedAt, run.CreatedAt.Location(), wantTimestamp)
	}
	if !run.UpdatedAt.Equal(wantTimestamp) || run.UpdatedAt.Location() != time.UTC {
		t.Fatalf("run UpdatedAt = %v (%v), want UTC %v", run.UpdatedAt, run.UpdatedAt.Location(), wantTimestamp)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	configuredScopes := []string{
		"scanner-worker://repository/zeta",
		"scanner-worker://repository/alpha",
	}
	for index, scopeID := range configuredScopes {
		item := items[index]
		generationID := "scanner_worker:" + facts.StableID("ScannerWorkerWorkflowGeneration", map[string]any{
			"analyzer":    "sbom_generation",
			"instance_id": instance.InstanceID,
			"plan_key":    "continuous-20260604T200000Z",
			"scope_id":    scopeID,
		})
		workItemID := "scanner_worker:" + instance.InstanceID + ":" + generationID
		if got := item.WorkItemID; got != workItemID {
			t.Fatalf("items[%d].WorkItemID = %q, want %q", index, got, workItemID)
		}
		if got := item.RunID; got != wantRunID {
			t.Fatalf("items[%d].RunID = %q, want %q", index, got, wantRunID)
		}
		if got, want := item.CollectorKind, scope.CollectorScannerWorker; got != want {
			t.Fatalf("items[%d].CollectorKind = %q, want %q", index, got, want)
		}
		if got, want := item.CollectorInstanceID, instance.InstanceID; got != want {
			t.Fatalf("items[%d].CollectorInstanceID = %q, want %q", index, got, want)
		}
		if got, want := item.SourceSystem, string(scope.CollectorScannerWorker); got != want {
			t.Fatalf("items[%d].SourceSystem = %q, want %q", index, got, want)
		}
		if got := item.ScopeID; got != scopeID {
			t.Fatalf("items[%d].ScopeID = %q, want configured order %q", index, got, scopeID)
		}
		if got := item.AcceptanceUnitID; got != scopeID {
			t.Fatalf("items[%d].AcceptanceUnitID = %q, want %q", index, got, scopeID)
		}
		if got := item.GenerationID; got != generationID {
			t.Fatalf("items[%d].GenerationID = %q, want %q", index, got, generationID)
		}
		if got := item.SourceRunID; got != generationID {
			t.Fatalf("items[%d].SourceRunID = %q, want %q", index, got, generationID)
		}
		if got, want := item.FairnessKey, "scanner_worker:scanner-worker-ordered:repository"; got != want {
			t.Fatalf("items[%d].FairnessKey = %q, want %q", index, got, want)
		}
		if got, want := item.Status, workflow.WorkItemStatusPending; got != want {
			t.Fatalf("items[%d].Status = %q, want %q", index, got, want)
		}
		if !item.CreatedAt.Equal(wantTimestamp) || item.CreatedAt.Location() != time.UTC {
			t.Fatalf("items[%d].CreatedAt = %v (%v), want UTC %v", index, item.CreatedAt, item.CreatedAt.Location(), wantTimestamp)
		}
		if !item.UpdatedAt.Equal(wantTimestamp) || item.UpdatedAt.Location() != time.UTC {
			t.Fatalf("items[%d].UpdatedAt = %v (%v), want UTC %v", index, item.UpdatedAt, item.UpdatedAt.Location(), wantTimestamp)
		}
	}
	repeatedRun, repeatedItems, err := WorkPlanner{}.PlanScannerWorkerWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260604T200000Z",
	})
	if err != nil {
		t.Fatalf("repeated PlanScannerWorkerWork() error = %v, want nil", err)
	}
	if got, want := repeatedRun.RunID, run.RunID; got != want {
		t.Fatalf("repeated RunID = %q, want stable %q", got, want)
	}
	for index := range items {
		if got, want := repeatedItems[index].WorkItemID, items[index].WorkItemID; got != want {
			t.Fatalf("repeated items[%d].WorkItemID = %q, want stable %q", index, got, want)
		}
		if got, want := repeatedItems[index].GenerationID, items[index].GenerationID; got != want {
			t.Fatalf("repeated items[%d].GenerationID = %q, want stable %q", index, got, want)
		}
	}
}

func TestScannerWorkerWorkPlannerRejectsDuplicateTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 4, 20, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "scanner-worker-sbom",
		CollectorKind: scope.CollectorScannerWorker,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"analyzer":"sbom_generation","sbom_targets":[
			{"scope_id":"scanner-worker://repository/repository-corpus","root_path":"/corpus/one"},
			{"scope_id":"scanner-worker://repository/repository-corpus","root_path":"/corpus/two"}
		]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, _, err := WorkPlanner{}.PlanScannerWorkerWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260604T200000Z",
	})
	if err == nil {
		t.Fatal("PlanScannerWorkerWork() error = nil, want duplicate target rejection")
	}
	if got, want := err.Error(), `duplicate scanner-worker target scope_id "scanner-worker://repository/repository-corpus"`; !strings.Contains(got, want) {
		t.Fatalf("PlanScannerWorkerWork() error = %q, want substring %q", got, want)
	}
}

func TestScannerWorkerWorkPlannerRejectsSBOMTargetWithoutRootPath(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 4, 20, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "scanner-worker-sbom",
		CollectorKind: scope.CollectorScannerWorker,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"analyzer":"sbom_generation","sbom_targets":[
			{"scope_id":"scanner-worker://repository/repository-corpus"}
		]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, _, err := WorkPlanner{}.PlanScannerWorkerWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260604T200000Z",
	})
	if err == nil {
		t.Fatal("PlanScannerWorkerWork() error = nil, want missing root_path rejection")
	}
	if got, want := err.Error(), "scanner-worker sbom_generation target root_path is required"; !strings.Contains(got, want) {
		t.Fatalf("PlanScannerWorkerWork() error = %q, want substring %q", got, want)
	}
}
