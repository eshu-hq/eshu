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
			{"provider":"harbor","base_url":"https://harbor.example.com","repository":"Project/API","references":["latest"]}
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
