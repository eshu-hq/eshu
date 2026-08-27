// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/sbomattestation"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeSBOMAttestationPlanner struct {
	requests []sbomattestation.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
}

func (f *fakeSBOMAttestationPlanner) PlanSBOMAttestationWork(_ context.Context, request sbomattestation.PlanRequest) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesSBOMAttestationWorkThroughChildPlanner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.June, 18, 15, 30, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{InstanceID: "sbom-primary", CollectorKind: scope.CollectorSBOMAttestation, Mode: workflow.CollectorModeScheduled, Enabled: true, ClaimsEnabled: true, Configuration: `{"targets":[{"scope_id":"sbom://example","source_type":"configured_source","artifact_kind":"sbom","document_format":"cyclonedx","document_url":"https://example.invalid/sbom.json"}]}`, LastObservedAt: now, CreatedAt: now, UpdatedAt: now}
	run := workflow.Run{RunID: "sbom_attestation:sbom-primary:schedule:scheduled-20260618T150000Z", TriggerKind: workflow.TriggerKindSchedule, Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "{}", RequestedCollector: string(scope.CollectorSBOMAttestation), CreatedAt: now, UpdatedAt: now}
	item := workflow.WorkItem{WorkItemID: "sbom-item", RunID: run.RunID, CollectorKind: scope.CollectorSBOMAttestation, CollectorInstanceID: instance.InstanceID, SourceSystem: string(scope.CollectorSBOMAttestation), ScopeID: "sbom://example", AcceptanceUnitID: "sbom://example", SourceRunID: "sbom_attestation:generation", GenerationID: "sbom_attestation:generation", FairnessKey: "sbom_attestation:sbom-primary:sbom", Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now}
	planner := &fakeSBOMAttestationPlanner{run: run, items: []workflow.WorkItem{item}}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := Service{Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true, ReconcileInterval: time.Hour, ReapInterval: time.Hour, ClaimLeaseTTL: time.Minute, HeartbeatInterval: 20 * time.Second, ExpiredClaimLimit: 10, ExpiredClaimRequeueDelay: 5 * time.Second, CollectorInstances: []workflow.DesiredCollectorInstance{{InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode, Enabled: true, ClaimsEnabled: true, Configuration: instance.Configuration}}}, Store: store, SBOMAttestationPlanner: planner, Clock: func() time.Time { return now }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := len(planner.requests); got != 1 {
		t.Fatalf("planner requests = %d, want 1", got)
	}
	if got, want := planner.requests[0].PlanKey, "scheduled-20260618T150000Z"; got != want {
		t.Fatalf("PlanKey = %q, want %q", got, want)
	}
	if got := len(store.createdRuns); got != 1 {
		t.Fatalf("created runs = %d, want 1", got)
	}
	if got := len(store.enqueuedItems); got != 1 {
		t.Fatalf("enqueued items = %d, want 1", got)
	}
}
