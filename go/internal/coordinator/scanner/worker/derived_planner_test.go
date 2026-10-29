// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPlanDerivedImageWorkGroupsSourcesWithinBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	instance := workflow.CollectorInstance{
		InstanceID: "scanner-a", CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
		Configuration:  `{"analyzer":"image_unpacking","derive_from_running_images":true}`,
		LastObservedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	first := workflow.RunningImageObservationCandidate{
		ObservationID: 1, ProducerInstanceID: "source-a", SourceScopeID: "kubernetes://cluster-a",
		SourceGenerationID: "generation-a", Registry: "registry.example.invalid", Repository: "private/a",
		RuntimeImageReference: "registry.example.invalid/private/a:one", RuntimeDigest: digest,
		RuntimeDigestKind: workflow.RunningImageDigestManifest, ManifestDigest: digest,
		AuthorizationDomain: "domain-a",
	}
	second := first
	second.ObservationID = 2
	second.ProducerInstanceID = "source-b"
	second.SourceScopeID = "ecs://cluster-b"
	second.SourceGenerationID = "generation-b"
	second.Repository = "private/b"
	second.RuntimeImageReference = "registry.example.invalid/private/b:two"
	run, items, targets, _, err := WorkPlanner{}.PlanDerivedImageWork(context.Background(), DerivedImagePlanRequest{
		Instance: instance, ObservedAt: now, PlanKey: "derived-1", AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
		Candidates: []workflow.RunningImageObservationCandidate{first, second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(targets) != 1 || len(targets[0].Sources) != 2 {
		t.Fatalf("planned items=%d targets=%d sources=%d, want 1/1/2", len(items), len(targets), len(targets[0].Sources))
	}
	if items[0].ScopeID != targets[0].ScanTargetID || targets[0].WorkItemID != items[0].WorkItemID {
		t.Fatal("work item and target identities differ")
	}
	if strings.Contains(run.RequestedScopeSet, "private/") || strings.Contains(run.RequestedScopeSet, "registry.example.invalid") {
		t.Fatal("run metadata contains a source locator")
	}
	if targets[0].Sources[0].SourceObservationID != 1 || targets[0].Sources[1].SourceObservationID != 2 {
		t.Fatal("canonical source observations were not preserved")
	}
}

func TestPlanDerivedImageWorkSkipsDigestlessCandidateWithReason(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	instance := workflow.CollectorInstance{
		InstanceID: "scanner-a", CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
		Configuration:  `{"analyzer":"image_unpacking","derive_from_running_images":true}`,
		LastObservedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	valid := workflow.RunningImageObservationCandidate{
		ObservationID: 1, ProducerInstanceID: "source-a", SourceScopeID: "kubernetes://cluster-a",
		SourceGenerationID: "generation-a", Registry: "registry.example.invalid", Repository: "private/a",
		RuntimeImageReference: "registry.example.invalid/private/a:one", RuntimeDigest: digest,
		RuntimeDigestKind: workflow.RunningImageDigestManifest, ManifestDigest: digest,
		AuthorizationDomain: "domain-a",
	}
	tagOnly := workflow.RunningImageObservationCandidate{
		ObservationID: 2, ProducerInstanceID: "source-a", SourceScopeID: "kubernetes://cluster-a",
		SourceGenerationID: "generation-a", Registry: "registry.example.invalid", Repository: "private/b",
		RuntimeImageReference: "registry.example.invalid/private/b:two",
		RuntimeDigestKind:     workflow.RunningImageDigestUnknown,
		AuthorizationDomain:   "domain-a",
	}
	_, items, targets, skipped, err := WorkPlanner{}.PlanDerivedImageWork(context.Background(), DerivedImagePlanRequest{
		Instance: instance, ObservedAt: now, PlanKey: "derived-skip-1", AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
		Candidates: []workflow.RunningImageObservationCandidate{valid, tagOnly},
	})
	if err != nil {
		t.Fatalf("digestless candidate must skip, not fail the page: %v", err)
	}
	if len(items) != 1 || len(targets) != 1 {
		t.Fatalf("planned items=%d targets=%d, want 1/1", len(items), len(targets))
	}
	found := false
	for _, evidence := range skipped {
		if evidence.Reason == schedule.DerivedTargetSkipReasonMissingDigest && evidence.SkippedCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing digest skip not recorded by reason: %+v", skipped)
	}
}
