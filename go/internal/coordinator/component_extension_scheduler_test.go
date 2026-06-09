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

func TestComponentExtensionPlannerPlansActivationScopedWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 9, 13, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.scorecard",
			"component_version":"0.1.0",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd",
			"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}
		}`,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
		LastObservedAt: observedAt,
	}

	run, items, err := ComponentExtensionWorkPlanner{}.PlanComponentExtensionWork(context.Background(), ComponentExtensionPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "scheduled-20260609T130000Z",
	})
	if err != nil {
		t.Fatalf("PlanComponentExtensionWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, "scorecard"; got != want {
		t.Fatalf("requested collector = %q, want %q", got, want)
	}
	if strings.Contains(run.RequestedScopeSet, "private") {
		t.Fatalf("requested scope set = %s, did not want private config content", run.RequestedScopeSet)
	}
	var requested struct {
		ComponentID  string `json:"component_id"`
		ConfigHandle string `json:"config_handle"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("RequestedScopeSet is not JSON: %v", err)
	}
	if got, want := requested.ComponentID, "dev.eshu.examples.scorecard"; got != want {
		t.Fatalf("component id = %q, want %q", got, want)
	}
	if got, want := requested.ConfigHandle, "component-config:abcd"; got != want {
		t.Fatalf("config handle = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("work items = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.CollectorKind, scope.CollectorKind("scorecard"); got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
	if got, want := item.CollectorInstanceID, "scorecard-primary"; got != want {
		t.Fatalf("collector instance id = %q, want %q", got, want)
	}
	if got, want := item.SourceSystem, "dev.eshu.examples.scorecard"; got != want {
		t.Fatalf("source system = %q, want %q", got, want)
	}
	if item.ScopeID == "" || item.AcceptanceUnitID != item.ScopeID {
		t.Fatalf("scope_id=%q acceptance_unit_id=%q, want same non-empty id", item.ScopeID, item.AcceptanceUnitID)
	}
	if got, want := item.Status, workflow.WorkItemStatusPending; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestShouldScheduleComponentExtensionSurfacesInvalidActivationConfig(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 9, 13, 15, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.scorecard",
			"component_version":"0.1.0",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd",
			"runtime":{"sdk_protocol":"collector-sdk/v9","adapter":"oci"}
		}`,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
		LastObservedAt: observedAt,
	}

	if !shouldScheduleComponentExtension(instance) {
		t.Fatal("shouldScheduleComponentExtension() = false, want true so planner returns the validation error")
	}
	_, _, err := ComponentExtensionWorkPlanner{}.PlanComponentExtensionWork(context.Background(), ComponentExtensionPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "scheduled-20260609T130000Z",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime.sdk_protocol") {
		t.Fatalf("PlanComponentExtensionWork() error = %v, want runtime.sdk_protocol rejection", err)
	}
}

func TestShouldScheduleComponentExtensionIgnoresUnrelatedSchemaVersionConfig(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 9, 13, 20, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "collector-git-primary",
		CollectorKind:  scope.CollectorGit,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"schema_version":"git.collector.v1","provider":"github"}`,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
		LastObservedAt: observedAt,
	}

	if shouldScheduleComponentExtension(instance) {
		t.Fatal("shouldScheduleComponentExtension() = true, want false for unrelated collector config")
	}
}
