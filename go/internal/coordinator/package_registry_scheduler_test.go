package coordinator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPackageRegistryWorkPlannerPlansOneWorkItemPerTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 17, 30, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPackageRegistryConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := PackageRegistryWorkPlanner{}.PlanPackageRegistryWork(context.Background(), PackageRegistryPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260513T173000Z",
	})
	if err != nil {
		t.Fatalf("PlanPackageRegistryWork() error = %v", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorPackageRegistry); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.ScopeID, "package-registry://jfrog/generic/team-api"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := item.FairnessKey, "package_registry:collector-package-registry:generic"; got != want {
		t.Fatalf("FairnessKey = %q, want %q", got, want)
	}
	if item.GenerationID == "" || item.GenerationID != item.SourceRunID {
		t.Fatalf("GenerationID = %q SourceRunID = %q, want same nonblank value", item.GenerationID, item.SourceRunID)
	}
	var requested struct {
		Targets []struct {
			ScopeID   string `json:"scope_id"`
			Ecosystem string `json:"ecosystem"`
			Provider  string `json:"provider"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet JSON = %q: %v", run.RequestedScopeSet, err)
	}
	if got, want := requested.Targets[0].ScopeID, item.ScopeID; got != want {
		t.Fatalf("RequestedScopeSet scope_id = %q, want %q", got, want)
	}
	if got, want := requested.Targets[0].Ecosystem, "generic"; got != want {
		t.Fatalf("RequestedScopeSet ecosystem = %q, want %q", got, want)
	}
}

func TestPackageRegistryWorkPlannerRejectsDuplicateScopeTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 17, 45, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "collector-package-registry",
		CollectorKind: scope.CollectorPackageRegistry,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[
			{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api"],"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api"},
			{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api-copy"],"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api-copy"}
		]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, _, err := PackageRegistryWorkPlanner{}.PlanPackageRegistryWork(context.Background(), PackageRegistryPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260513T174500Z",
	})
	if err == nil {
		t.Fatal("PlanPackageRegistryWork() error = nil, want duplicate scope rejection")
	}
}

func testPackageRegistryConfiguration() string {
	return `{"targets":[{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api"],"package_limit":10,"version_limit":25,"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api"}]}`
}
