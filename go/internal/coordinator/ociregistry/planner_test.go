// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociregistry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func testOCIRegistryConfiguration() string {
	return `{
		"targets": [{
			"provider": "dockerhub",
			"registry": "registry-1.docker.io",
			"repository": "library/busybox",
			"references": ["latest"],
			"tag_limit": 1
		}]
	}`
}

func TestOCIRegistryWorkPlannerPlansOneWorkItemPerTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 16, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-oci-registry",
		CollectorKind:  scope.CollectorOCIRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testOCIRegistryConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := WorkPlanner{}.PlanOCIRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260513T160000Z",
	})
	if err != nil {
		t.Fatalf("PlanOCIRegistryWork() error = %v", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorOCIRegistry); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.ScopeID, "oci-registry://registry-1.docker.io/library/busybox"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if item.GenerationID == "" || item.GenerationID != item.SourceRunID {
		t.Fatalf("GenerationID = %q SourceRunID = %q, want same nonblank value", item.GenerationID, item.SourceRunID)
	}
	var requested struct {
		Targets []struct {
			ScopeID string `json:"scope_id"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := requested.Targets[0].ScopeID, item.ScopeID; got != want {
		t.Fatalf("RequestedScopeSet scope_id = %q, want %q", got, want)
	}
}

func TestOCIRegistryWorkPlannerNormalizesProviderEndpointFields(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 16, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "collector-oci-registry",
		CollectorKind: scope.CollectorOCIRegistry,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[
			{"provider":"ecr","registry_id":"123456789012","region":"us-east-1","repository":"team/api","references":["latest"]},
			{"provider":"google_artifact_registry","registry_host":"us-west1-docker.pkg.dev","repository":"example-project/team-api/service","references":["sha256:abc"]},
			{"provider":"azure_container_registry","registry_host":"example.azurecr.io","repository":"Samples/Artifact","references":["readme"]},
			{"provider":"jfrog","base_url":"https://example.jfrog.io","repository_key":"docker-local","repository":"service-api","references":["latest"]},
			{"provider":"harbor","base_url":"https://harbor.example.com","repository":"Project/API","references":["latest"]},
			{"provider":"ghcr","repository":"eshu-hq/Eshu","references":["latest"]}
		]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := WorkPlanner{}.PlanOCIRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260513T160000Z",
	})
	if err != nil {
		t.Fatalf("PlanOCIRegistryWork() error = %v, want nil", err)
	}
	got := make(map[string]bool, len(items))
	for _, item := range items {
		got[item.ScopeID] = true
	}
	for _, want := range []string{
		"oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api",
		"oci-registry://us-west1-docker.pkg.dev/example-project/team-api/service",
		"oci-registry://example.azurecr.io/samples/artifact",
		"oci-registry://example.jfrog.io/artifactory/api/docker/docker-local/service-api",
		"oci-registry://harbor.example.com/project/api",
		"oci-registry://ghcr.io/eshu-hq/eshu",
	} {
		if !got[want] {
			t.Fatalf("planned scope IDs = %#v, missing %q", got, want)
		}
	}
}

// TestOCIRegistryWorkPlannerRejectsDuplicateNormalizedTargets pins that
// two differently-spelled dockerhub targets which normalize to the same
// repository identity are rejected specifically as a duplicate, not merely
// that planning returns some error. A prior version of this test only
// checked err != nil, which would have passed just as well for an unrelated
// configuration-parse failure and proven nothing about the normalization
// dedup this planner performs.
func TestOCIRegistryWorkPlannerRejectsDuplicateNormalizedTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 16, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "collector-oci-registry",
		CollectorKind: scope.CollectorOCIRegistry,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[
			{"provider":"dockerhub","repository":"busybox","references":["latest"]},
			{"provider":"dockerhub","registry":"docker.io","repository":"library/busybox","references":["stable"]}
		]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, _, err := WorkPlanner{}.PlanOCIRegistryWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260513T160000Z",
	})
	if err == nil {
		t.Fatal("PlanOCIRegistryWork() error = nil, want duplicate target rejection")
	}
	if !strings.Contains(err.Error(), "duplicate OCI registry target scope_id") {
		t.Fatalf("PlanOCIRegistryWork() error = %q, want a duplicate scope_id rejection", err)
	}
	const wantScopeID = "oci-registry://docker.io/library/busybox"
	if !strings.Contains(err.Error(), wantScopeID) {
		t.Fatalf("PlanOCIRegistryWork() error = %q, want it to name the colliding scope_id %q", err, wantScopeID)
	}
}

// TestOCIRegistryWorkPlannerPinsExactIdentityStrings pins the run, work-item,
// generation, source-run, and fairness identity strings byte-for-byte for a
// fixed request. The package README cites this planner's identities as stable
// across the extraction, and the surrounding tests assert only that planning
// succeeds and which scope IDs appear -- an identity-format regression would
// pass every one of them. These strings feed lease ownership and fairness
// partitioning, so a silent change to any of them is a concurrency defect, not
// a cosmetic one.
func TestOCIRegistryWorkPlannerPinsExactIdentityStrings(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 16, 0, 0, 0, time.UTC)
	run, items, err := WorkPlanner{}.PlanOCIRegistryWork(context.Background(), PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:     "collector-oci-registry",
			CollectorKind:  scope.CollectorOCIRegistry,
			Mode:           workflow.CollectorModeContinuous,
			Enabled:        true,
			ClaimsEnabled:  true,
			Configuration:  testOCIRegistryConfiguration(),
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260513T160000Z",
	})
	if err != nil {
		t.Fatalf("PlanOCIRegistryWork() error = %v, want nil", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if got, want := run.RunID, "oci_registry:collector-oci-registry:schedule:continuous-20260513T160000Z"; got != want {
		t.Fatalf("run.RunID = %q, want %q", got, want)
	}
	item := items[0]
	if got, want := item.WorkItemID, "oci_registry:collector-oci-registry:oci_registry:76ff12ff320666121e0b8362d3f8b247eb0a1ea1003040078096d96d37706369"; got != want {
		t.Fatalf("WorkItemID = %q, want %q", got, want)
	}
	if got, want := item.FairnessKey, "oci_registry:collector-oci-registry:dockerhub"; got != want {
		t.Fatalf("FairnessKey = %q, want %q", got, want)
	}
	if got, want := item.GenerationID, "oci_registry:76ff12ff320666121e0b8362d3f8b247eb0a1ea1003040078096d96d37706369"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := item.SourceRunID, item.GenerationID; got != want {
		t.Fatalf("SourceRunID = %q, want it to equal GenerationID %q", got, want)
	}
}

// TestOCIRegistryWorkPlannerRejectsBlankConfiguration pins that a blank, empty
// object, or empty-targets configuration is a validation failure rather than a
// successful empty plan. The distinction matters operationally: an empty plan
// would let a misconfigured collector instance look healthy and simply schedule
// nothing, while the error surfaces the misconfiguration. Request validation
// runs ahead of the target-count check, so the zero-target early return in
// PlanOCIRegistryWork is unreachable through this path; this test fails if a
// future change reorders those two steps.
func TestOCIRegistryWorkPlannerRejectsBlankConfiguration(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 16, 0, 0, 0, time.UTC)
	for _, configuration := range []string{"", "{}", `{"targets":[]}`} {
		run, items, err := WorkPlanner{}.PlanOCIRegistryWork(context.Background(), PlanRequest{
			Instance: workflow.CollectorInstance{
				InstanceID:     "collector-oci-registry",
				CollectorKind:  scope.CollectorOCIRegistry,
				Mode:           workflow.CollectorModeContinuous,
				Enabled:        true,
				ClaimsEnabled:  true,
				Configuration:  configuration,
				LastObservedAt: observedAt,
				CreatedAt:      observedAt,
				UpdatedAt:      observedAt,
			},
			ObservedAt: observedAt,
			PlanKey:    "continuous-20260513T160000Z",
		})
		if err == nil {
			t.Fatalf("PlanOCIRegistryWork(%q) error = nil, want a validation failure", configuration)
		}
		if !strings.Contains(err.Error(), "requires targets") {
			t.Fatalf("PlanOCIRegistryWork(%q) error = %v, want it to name the missing targets", configuration, err)
		}
		if run.RunID != "" || len(items) != 0 {
			t.Fatalf("PlanOCIRegistryWork(%q) = run %q with %d item(s), want a zero run and no items", configuration, run.RunID, len(items))
		}
	}
}
