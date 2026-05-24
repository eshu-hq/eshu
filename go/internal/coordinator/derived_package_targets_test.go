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
			ScopeID string  `json:"scope_id"`
			Derived bool    `json:"derived"`
			Version *string `json:"version"`
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
		if target.Version != nil {
			t.Fatalf("requested package-registry target leaked version metadata: %#v", target)
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

func TestExactOwnedDependencyVersionAllowsSemverPrereleaseVersions(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"1.0.0-next.1",
		"1.0.0-git.1",
		"1.0.0+git.sha",
	} {
		got, ok := exactOwnedDependencyVersion(raw)
		if !ok {
			t.Fatalf("exactOwnedDependencyVersion(%q) ok = false, want true", raw)
		}
		if got != raw {
			t.Fatalf("exactOwnedDependencyVersion(%q) = %q, want %q", raw, got, raw)
		}
	}

	if got, ok := exactOwnedDependencyVersion("^1.0.0-next.1"); ok {
		t.Fatalf("exactOwnedDependencyVersion() = %q, want range rejection", got)
	}
	if got, ok := exactOwnedDependencyVersion("git+https://github.com/acme/pkg.git"); ok {
		t.Fatalf("exactOwnedDependencyVersion() = %q, want git URL rejection", got)
	}
	if got, ok := exactOwnedDependencyVersion("git://github.com/acme/pkg.git"); ok {
		t.Fatalf("exactOwnedDependencyVersion() = %q, want git URL rejection", got)
	}
	if got, ok := exactOwnedDependencyVersion("gitlab:acme/pkg"); ok {
		t.Fatalf("exactOwnedDependencyVersion() = %q, want git URL rejection", got)
	}
	if got, ok := exactOwnedDependencyVersion("release-2026-05-24"); ok {
		t.Fatalf("exactOwnedDependencyVersion() = %q, want non-semver rejection", got)
	}
}

func TestPackageRegistryDerivedTargetUsesNormalizedMetadataURL(t *testing.T) {
	t.Parallel()

	target, ok := npmPackageRegistryTarget(
		workflow.OwnedPackageDependencyTarget{
			Ecosystem:   "npm",
			PackageName: "Vite",
		},
		1,
		200,
	)
	if !ok {
		t.Fatal("npmPackageRegistryTarget() ok = false, want true")
	}
	if got, want := target.ScopeID, "npm://registry.npmjs.org/vite"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := target.SourceURI, "https://registry.npmjs.org/vite"; got != want {
		t.Fatalf("SourceURI = %q, want normalized metadata URL %q", got, want)
	}
	if got, want := target.MetadataURL, "https://registry.npmjs.org/vite"; got != want {
		t.Fatalf("MetadataURL = %q, want normalized metadata URL %q", got, want)
	}
}
