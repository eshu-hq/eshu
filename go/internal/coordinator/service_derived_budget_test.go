package coordinator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceRunActiveModeSurfacesPackageRegistryDerivedBudgetExhaustion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 28, 15, 30, 0, 0, time.UTC)
	instance := testServicePackageRegistryInstance(now)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":2,"version_limit":1}}`
	targetReader := &fakeOwnedPackageTargetReader{targets: makeOwnedNPMTargets(5)}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := Service{
		Config:                   activeDerivedTargetConfig(scope.CollectorPackageRegistry, "collector-package-registry", instance.Configuration),
		Store:                    store,
		PackageRegistryPlanner:   PackageRegistryWorkPlanner{},
		OwnedPackageTargetReader: targetReader,
		Clock:                    func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(targetReader.requests), 1; got != want {
		t.Fatalf("target reader requests = %d, want %d", got, want)
	}
	if got, want := targetReader.requests[0].Limit, 3; got != want {
		t.Fatalf("target reader limit = %d, want budget plus exhaustion lookahead %d", got, want)
	}
	if got, want := targetReader.requests[0].RotationOffset, derivedTargetRotationOffset(now, time.Hour, 2); got != want {
		t.Fatalf("target reader rotation offset = %d, want %d", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}

	var requested struct {
		SkippedTargets []derivedTargetSkipEvidence `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(store.createdRuns[0].RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", store.createdRuns[0].RequestedScopeSet, err)
	}
	assertDerivedBudgetSkipEvidence(t, requested.SkippedTargets, "package_registry", 2, 2, 1)
}

func TestServiceRunActiveModeSurfacesVulnerabilityDerivedBudgetExhaustion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 28, 15, 45, 0, 0, time.UTC)
	instance := testServiceVulnerabilityIntelligenceInstance(now)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":2}}`
	targetReader := &fakeOwnedPackageTargetReader{targets: makeOwnedNPMTargets(5)}
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, "collector-vulnerability-intelligence", instance.Configuration),
		Store:                            store,
		VulnerabilityIntelligencePlanner: VulnerabilityIntelligenceWorkPlanner{},
		OwnedPackageTargetReader:         targetReader,
		Clock:                            func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(targetReader.requests), 1; got != want {
		t.Fatalf("target reader requests = %d, want %d", got, want)
	}
	if got, want := targetReader.requests[0].Limit, 3; got != want {
		t.Fatalf("target reader limit = %d, want budget plus exhaustion lookahead %d", got, want)
	}
	if got, want := targetReader.requests[0].RotationOffset, derivedTargetRotationOffset(now, time.Hour, 2); got != want {
		t.Fatalf("target reader rotation offset = %d, want %d", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}

	var requested struct {
		SkippedTargets []derivedTargetSkipEvidence `json:"skipped_targets"`
	}
	if err := json.Unmarshal([]byte(store.createdRuns[0].RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", store.createdRuns[0].RequestedScopeSet, err)
	}
	assertDerivedBudgetSkipEvidence(t, requested.SkippedTargets, "vulnerability_intelligence", 2, 2, 1)
}

func activeDerivedTargetConfig(
	collectorKind scope.CollectorKind,
	instanceID string,
	configuration string,
) Config {
	return Config{
		DeploymentMode:           deploymentModeActive,
		ClaimsEnabled:            true,
		ReconcileInterval:        time.Hour,
		ReapInterval:             time.Hour,
		ClaimLeaseTTL:            time.Minute,
		HeartbeatInterval:        20 * time.Second,
		ExpiredClaimLimit:        10,
		ExpiredClaimRequeueDelay: 5 * time.Second,
		CollectorInstances: []workflow.DesiredCollectorInstance{{
			InstanceID:    instanceID,
			CollectorKind: collectorKind,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
			ClaimsEnabled: true,
			Configuration: configuration,
		}},
	}
}
