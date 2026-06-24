// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

	run, items, err := ScannerWorkerWorkPlanner{}.PlanScannerWorkerWork(context.Background(), ScannerWorkerPlanRequest{
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

	_, _, err := ScannerWorkerWorkPlanner{}.PlanScannerWorkerWork(context.Background(), ScannerWorkerPlanRequest{
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

	_, _, err := ScannerWorkerWorkPlanner{}.PlanScannerWorkerWork(context.Background(), ScannerWorkerPlanRequest{
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
