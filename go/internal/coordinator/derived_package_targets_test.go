package coordinator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPackageRegistryWorkPlannerDerivesNPMTargetsFromOwnedPackageEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 23, 21, 30, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":10,"version_limit":50}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := PackageRegistryWorkPlanner{}.PlanPackageRegistryWork(context.Background(), PackageRegistryPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260523T213000Z",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{
			{
				Ecosystem:    "npm",
				PackageName:  "vite",
				Version:      "^5.4.11",
				RepositoryID: "repo-eshu",
			},
			{
				Ecosystem:    "npm",
				PackageName:  "@scope/widget",
				Version:      "1.2.3",
				Lockfile:     true,
				RepositoryID: "repo-eshu",
			},
			{
				Ecosystem:    "composer",
				PackageName:  "symfony/console",
				Version:      "7.0.0",
				Lockfile:     true,
				RepositoryID: "repo-eshu",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "npm://registry.npmjs.org/@scope/widget"; got != want {
		t.Fatalf("items[0].ScopeID = %q, want %q", got, want)
	}
	if got, want := items[1].ScopeID, "npm://registry.npmjs.org/vite"; got != want {
		t.Fatalf("items[1].ScopeID = %q, want %q", got, want)
	}

	var requested struct {
		Targets []struct {
			ScopeID string `json:"scope_id"`
			Derived bool   `json:"derived"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := len(requested.Targets), 2; got != want {
		t.Fatalf("len(RequestedScopeSet.targets) = %d, want %d", got, want)
	}
	for _, target := range requested.Targets {
		if !target.Derived {
			t.Fatalf("requested target %#v is not marked derived", target)
		}
	}
}

func TestVulnerabilityIntelligenceWorkPlannerDerivesOSVTargetsForExactOwnedVersions(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 23, 21, 45, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-vulnerability-intelligence",
		CollectorKind:  scope.CollectorVulnerabilityIntelligence,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":10}}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := VulnerabilityIntelligenceWorkPlanner{}.PlanVulnerabilityIntelligenceWork(context.Background(), VulnerabilityIntelligencePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260523T214500Z",
		OwnedPackageTargets: []workflow.OwnedPackageDependencyTarget{
			{
				Ecosystem:    "npm",
				PackageName:  "vite",
				Version:      "5.4.21",
				Lockfile:     true,
				RepositoryID: "repo-eshu",
			},
			{
				Ecosystem:    "npm",
				PackageName:  "ws",
				Version:      "^8.20.0",
				RepositoryID: "repo-eshu",
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanVulnerabilityIntelligenceWork() error = %v", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "vuln-intel://osv/npm/vite?version=5.4.21"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}

	var requested struct {
		Targets []struct {
			Source      string `json:"source"`
			Ecosystem   string `json:"ecosystem"`
			PackageName string `json:"package_name"`
			Version     string `json:"version"`
			Derived     bool   `json:"derived"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := requested.Targets[0].PackageName, "vite"; got != want {
		t.Fatalf("PackageName = %q, want %q", got, want)
	}
	if !requested.Targets[0].Derived {
		t.Fatalf("derived OSV target not marked derived: %#v", requested.Targets[0])
	}
}
