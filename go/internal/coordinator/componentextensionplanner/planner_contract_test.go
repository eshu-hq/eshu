// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package componentextensionplanner

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func validComponentExtensionInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
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
}

// TestPlanComponentExtensionWorkDeterministicIDsForFixedRequest proves
// repeated coordinator reconciles against the same instance, plan key, and
// observed time stay idempotent: RunID, WorkItemID, GenerationID,
// SourceRunID, and FairnessKey must all match across two calls.
func TestPlanComponentExtensionWorkDeterministicIDsForFixedRequest(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 20, 9, 0, 0, 0, time.UTC)
	request := PlanRequest{
		Instance:   validComponentExtensionInstance(observedAt),
		ObservedAt: observedAt,
		PlanKey:    "scheduled-20260620T090000Z",
	}

	firstRun, firstItems, err := WorkPlanner{}.PlanComponentExtensionWork(context.Background(), request)
	if err != nil {
		t.Fatalf("first PlanComponentExtensionWork() error = %v, want nil", err)
	}
	secondRun, secondItems, err := WorkPlanner{}.PlanComponentExtensionWork(context.Background(), request)
	if err != nil {
		t.Fatalf("second PlanComponentExtensionWork() error = %v, want nil", err)
	}

	if firstRun.RunID != secondRun.RunID {
		t.Fatalf("RunID = %q, want match with %q", secondRun.RunID, firstRun.RunID)
	}
	if len(firstItems) != 1 || len(secondItems) != 1 {
		t.Fatalf("items = %d/%d, want 1/1", len(firstItems), len(secondItems))
	}
	first, second := firstItems[0], secondItems[0]
	if first.WorkItemID != second.WorkItemID {
		t.Fatalf("WorkItemID = %q, want match with %q", second.WorkItemID, first.WorkItemID)
	}
	if first.GenerationID != second.GenerationID {
		t.Fatalf("GenerationID = %q, want match with %q", second.GenerationID, first.GenerationID)
	}
	if first.SourceRunID != second.SourceRunID {
		t.Fatalf("SourceRunID = %q, want match with %q", second.SourceRunID, first.SourceRunID)
	}
	if first.FairnessKey != second.FairnessKey {
		t.Fatalf("FairnessKey = %q, want match with %q", second.FairnessKey, first.FairnessKey)
	}
}

// TestPlanComponentExtensionWorkTimestampsAreUTC proves the planner
// normalizes a non-UTC ObservedAt before stamping the run and work item, so
// coordinator reconciles never persist a local-offset timestamp.
func TestPlanComponentExtensionWorkTimestampsAreUTC(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("EDT", -4*60*60)
	observedAt := time.Date(2026, time.June, 20, 9, 0, 0, 0, location)
	run, items, err := WorkPlanner{}.PlanComponentExtensionWork(context.Background(), PlanRequest{
		Instance:   validComponentExtensionInstance(observedAt),
		ObservedAt: observedAt,
		PlanKey:    "scheduled-20260620T130000Z",
	})
	if err != nil {
		t.Fatalf("PlanComponentExtensionWork() error = %v, want nil", err)
	}
	if run.CreatedAt.Location() != time.UTC || run.UpdatedAt.Location() != time.UTC {
		t.Fatalf("run timestamps location = %v/%v, want UTC/UTC", run.CreatedAt.Location(), run.UpdatedAt.Location())
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0]
	if item.CreatedAt.Location() != time.UTC || item.UpdatedAt.Location() != time.UTC || item.VisibleAt.Location() != time.UTC {
		t.Fatalf(
			"item timestamps location = %v/%v/%v, want UTC/UTC/UTC",
			item.CreatedAt.Location(), item.UpdatedAt.Location(), item.VisibleAt.Location(),
		)
	}
}
