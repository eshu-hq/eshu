package coordinator

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceSchedulesTerraformStateSeedWorkItems(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 11, 0, 0, 0, time.UTC)
	instance := testTerraformStateCollectorInstance(observedAt)
	planner := TerraformStateWorkPlanner{}

	run, items, err := planner.PlanTerraformStateWork(context.Background(), TerraformStatePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "bootstrap",
	})
	if err != nil {
		t.Fatalf("PlanTerraformStateWork() error = %v, want nil", err)
	}
	if got, want := run.RunID, "terraform_state:collector-tfstate-primary:bootstrap:bootstrap"; got != want {
		t.Fatalf("RunID = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindBootstrap; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorTerraformState); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}

	item := items[0]
	expectedScope, err := scope.NewTerraformStateSnapshotScope(
		"platform-infra",
		string(terraformstate.BackendS3),
		"s3://tfstate-prod/services/api/terraform.tfstate",
		map[string]string{"repo_id": "platform-infra"},
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	expectedCandidateID, err := terraformstate.CandidatePlanningID(terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
			VersionID:   "version-123",
		},
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		RepoID: "platform-infra",
		Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	if got, want := item.CollectorKind, scope.CollectorTerraformState; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := item.CollectorInstanceID, instance.InstanceID; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := item.SourceSystem, string(scope.CollectorTerraformState); got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, expectedScope.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := item.GenerationID, expectedCandidateID; got != want {
		t.Fatalf("GenerationID = %q, want candidate planning ID %q", got, want)
	}
	if got, want := item.SourceRunID, expectedCandidateID; got != want {
		t.Fatalf("SourceRunID = %q, want candidate planning ID %q", got, want)
	}
	if strings.Contains(item.WorkItemID, "tfstate-prod") || strings.Contains(item.WorkItemID, "services/api") {
		t.Fatalf("WorkItemID = %q, must not expose raw S3 locator", item.WorkItemID)
	}
	if err := item.Validate(); err != nil {
		t.Fatalf("planned WorkItem Validate() error = %v, want nil", err)
	}
}

func TestTerraformStatePlannedWorkItemIsRuntimeCompatible(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 11, 5, 0, 0, time.UTC)
	instance := testTerraformStateCollectorInstance(observedAt)
	planner := TerraformStateWorkPlanner{}
	_, items, err := planner.PlanTerraformStateWork(context.Background(), TerraformStatePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "bootstrap",
	})
	if err != nil {
		t.Fatalf("PlanTerraformStateWork() error = %v, want nil", err)
	}
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:      terraformstate.BackendS3,
					Bucket:    "tfstate-prod",
					Key:       "services/api/terraform.tfstate",
					Region:    "us-east-1",
					RepoID:    "platform-infra",
					VersionID: "version-123",
				}},
			},
		},
		SourceFactory: &plannerStateFactory{
			source: &plannerStateSource{
				key: terraformstate.StateKey{
					BackendKind: terraformstate.BackendS3,
					Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
					VersionID:   "version-123",
				},
				state:      `{"serial":17,"lineage":"lineage-123","resources":[]}`,
				observedAt: observedAt,
			},
		},
		RedactionKey: key,
		Clock:        func() time.Time { return observedAt },
	}
	item := items[0]
	item.Status = workflow.WorkItemStatusClaimed
	item.CurrentFencingToken = 41

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Generation.FreshnessHint, "lineage=lineage-123 serial=17"; got != want {
		t.Fatalf("FreshnessHint = %q, want %q", got, want)
	}
}

func TestTerraformStatePlannerUsesPlanKeyForRecurringWorkIdentity(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 11, 10, 0, 0, time.UTC)
	instance := testTerraformStateCollectorInstance(observedAt)
	instance.Bootstrap = false
	planner := TerraformStateWorkPlanner{}

	firstRun, firstItems, err := planner.PlanTerraformStateWork(context.Background(), TerraformStatePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260510T1100Z",
	})
	if err != nil {
		t.Fatalf("PlanTerraformStateWork() first error = %v, want nil", err)
	}
	secondRun, secondItems, err := planner.PlanTerraformStateWork(context.Background(), TerraformStatePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt.Add(time.Hour),
		PlanKey:    "schedule-20260510T1200Z",
	})
	if err != nil {
		t.Fatalf("PlanTerraformStateWork() second error = %v, want nil", err)
	}

	if firstRun.RunID == secondRun.RunID {
		t.Fatalf("RunID = %q for both plan keys, want distinct recurring runs", firstRun.RunID)
	}
	if firstItems[0].WorkItemID == secondItems[0].WorkItemID {
		t.Fatalf("WorkItemID = %q for both plan keys, want distinct recurring work", firstItems[0].WorkItemID)
	}
	if got, want := firstRun.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
}

func TestTerraformStatePlannerRejectsInvalidDurableConfig(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 11, 15, 0, 0, time.UTC)
	instance := testTerraformStateCollectorInstance(observedAt)
	instance.Configuration = `{"discovery":{"graph":true}}`
	planner := TerraformStateWorkPlanner{}

	_, _, err := planner.PlanTerraformStateWork(context.Background(), TerraformStatePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "bootstrap",
	})
	if err == nil {
		t.Fatal("PlanTerraformStateWork() error = nil, want invalid Terraform-state config error")
	}
}

func TestTerraformStatePlannerRequestedScopeSetNamesHashedCandidates(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 11, 20, 0, 0, time.UTC)
	instance := testTerraformStateCollectorInstance(observedAt)
	planner := TerraformStateWorkPlanner{}

	run, items, err := planner.PlanTerraformStateWork(context.Background(), TerraformStatePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "bootstrap",
	})
	if err != nil {
		t.Fatalf("PlanTerraformStateWork() error = %v, want nil", err)
	}

	if !strings.Contains(run.RequestedScopeSet, items[0].ScopeID) {
		t.Fatalf("RequestedScopeSet = %q, want planned scope ID", run.RequestedScopeSet)
	}
	if !strings.Contains(run.RequestedScopeSet, items[0].GenerationID) {
		t.Fatalf("RequestedScopeSet = %q, want candidate planning ID", run.RequestedScopeSet)
	}
	for _, raw := range []string{"tfstate-prod", "services/api/terraform.tfstate", "version-123"} {
		if strings.Contains(run.RequestedScopeSet, raw) {
			t.Fatalf("RequestedScopeSet = %q, must not expose raw candidate identity %q", run.RequestedScopeSet, raw)
		}
	}
}

func testTerraformStateCollectorInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		Bootstrap:     true,
		ClaimsEnabled: true,
		Configuration: `{
			"discovery": {
				"seeds": [{
					"kind": "s3",
					"bucket": "tfstate-prod",
					"key": "services/api/terraform.tfstate",
					"region": "us-east-1",
					"repo_id": "platform-infra",
					"version_id": "version-123"
				}]
			},
			"aws": {"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader"}
		}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

type plannerStateFactory struct {
	source *plannerStateSource
}

func (f *plannerStateFactory) OpenSource(context.Context, terraformstate.DiscoveryCandidate) (terraformstate.StateSource, error) {
	return f.source, nil
}

type plannerStateSource struct {
	key        terraformstate.StateKey
	state      string
	observedAt time.Time
}

func (s *plannerStateSource) Identity() terraformstate.StateKey {
	return s.key
}

func (s *plannerStateSource) Open(context.Context) (io.ReadCloser, terraformstate.SourceMetadata, error) {
	return io.NopCloser(strings.NewReader(s.state)), terraformstate.SourceMetadata{
		ObservedAt: s.observedAt,
		Size:       int64(len(s.state)),
	}, nil
}
