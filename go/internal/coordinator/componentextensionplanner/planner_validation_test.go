// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package componentextensionplanner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestComponentExtensionPlannerPlansActivationScopedWork inherits the
// planner-level coverage this package received from the pre-extraction
// component_extension_scheduler_test.go.
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
			"host":{
				"source_system":"openssf-scorecard",
				"scope":{"id":"github.com/example/widgets","kind":"repository"}
			},
			"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}
		}`,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
		LastObservedAt: observedAt,
	}

	run, items, err := WorkPlanner{}.PlanComponentExtensionWork(context.Background(), PlanRequest{
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
		Host         struct {
			SourceSystem string `json:"source_system"`
			Scope        struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
			} `json:"scope"`
		} `json:"host"`
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
	if got, want := requested.Host.SourceSystem, "openssf-scorecard"; got != want {
		t.Fatalf("requested host source system = %q, want %q", got, want)
	}
	if got, want := requested.Host.Scope.ID, "github.com/example/widgets"; got != want {
		t.Fatalf("requested host scope id = %q, want %q", got, want)
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
	if got, want := item.SourceSystem, "openssf-scorecard"; got != want {
		t.Fatalf("source system = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "github.com/example/widgets"; got != want {
		t.Fatalf("scope id = %q, want %q", got, want)
	}
	if got, want := item.AcceptanceUnitID, "github.com/example/widgets"; got != want {
		t.Fatalf("acceptance unit id = %q, want %q", got, want)
	}
	if got, want := item.Status, workflow.WorkItemStatusPending; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	// The claimed-collection runtime invariant for non-terraform kinds requires
	// the planned generation to also be the source run id (see
	// collector.validateClaimedGeneration): a component generation IS its run.
	// Diverging prefixes fail the claim at runtime, so the planner must mint a
	// single identity for both fields.
	if item.GenerationID == "" || item.GenerationID != item.SourceRunID {
		t.Fatalf("GenerationID = %q SourceRunID = %q, want same nonblank value", item.GenerationID, item.SourceRunID)
	}
}

// TestComponentExtensionPlannerRejectsUnsupportedSDKProtocol is the planner
// half of the pre-extraction
// TestShouldScheduleComponentExtensionSurfacesInvalidActivationConfig: the
// root half (shouldScheduleComponentExtension still treats this
// configuration as a component-extension instance so the planner surfaces
// the rejection) now lives in service_component_extension_test.go. The
// rejection itself is produced by componentactivation.ParseConfig, pinned
// independently in componentactivation's own test package.
func TestComponentExtensionPlannerRejectsUnsupportedSDKProtocol(t *testing.T) {
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

	_, _, err := WorkPlanner{}.PlanComponentExtensionWork(context.Background(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "scheduled-20260609T130000Z",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime.sdk_protocol") {
		t.Fatalf("PlanComponentExtensionWork() error = %v, want runtime.sdk_protocol rejection", err)
	}
}
