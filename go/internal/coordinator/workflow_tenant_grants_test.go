// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestCreateWorkflowWorkIfNoOpenTargetsDeniesMissingTenantGrant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	service := Service{
		Config: Config{
			TenantBoundary: WorkflowTenantBoundary{
				TenantID:           "tenant-a",
				WorkspaceID:        "workspace-a",
				SubjectClass:       "collector",
				PolicyRevisionHash: "policy-a",
			},
		},
		Store:             store,
		TenantGrantReader: &fakeTenantGrantReader{},
	}

	enqueued, err := service.createWorkflowWorkIfNoOpenTargets(
		context.Background(),
		collectorInstanceForTenantGrantTest(),
		runForTenantGrantTest(now),
		[]workflow.WorkItem{workItemForTenantGrantTest(now, "aws:scope:lambda")},
	)
	if err != nil {
		t.Fatalf("createWorkflowWorkIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := enqueued, 0; got != want {
		t.Fatalf("enqueued = %d, want %d", got, want)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0", got)
	}
}

func TestCreateWorkflowWorkIfNoOpenTargetsAppliesActiveTenantGrant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 12, 5, 0, 0, time.UTC)
	store := &fakeStore{}
	grantReader := &fakeTenantGrantReader{
		grants: []WorkflowTenantScopeGrant{{
			ScopeID:            "aws:scope:lambda",
			PolicyRevisionHash: "policy-a",
		}},
	}
	service := Service{
		Config: Config{
			TenantBoundary: WorkflowTenantBoundary{
				TenantID:           "tenant-a",
				WorkspaceID:        "workspace-a",
				SubjectClass:       "collector",
				PolicyRevisionHash: "policy-a",
			},
		},
		Store:             store,
		TenantGrantReader: grantReader,
	}

	enqueued, err := service.createWorkflowWorkIfNoOpenTargets(
		context.Background(),
		collectorInstanceForTenantGrantTest(),
		runForTenantGrantTest(now),
		[]workflow.WorkItem{workItemForTenantGrantTest(now, "aws:scope:lambda")},
	)
	if err != nil {
		t.Fatalf("createWorkflowWorkIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := enqueued, 1; got != want {
		t.Fatalf("enqueued = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	item := store.enqueuedItems[0]
	if got, want := item.TenantID, "tenant-a"; got != want {
		t.Fatalf("TenantID = %q, want %q", got, want)
	}
	if got, want := item.WorkspaceID, "workspace-a"; got != want {
		t.Fatalf("WorkspaceID = %q, want %q", got, want)
	}
	if got, want := item.SubjectClass, "collector"; got != want {
		t.Fatalf("SubjectClass = %q, want %q", got, want)
	}
	if got, want := item.PolicyRevisionHash, "policy-a"; got != want {
		t.Fatalf("PolicyRevisionHash = %q, want %q", got, want)
	}
	if got, want := grantReader.queries[0].ScopeIDs, []string{"aws:scope:lambda"}; !sameStringSet(got, want) {
		t.Fatalf("grant query ScopeIDs = %#v, want %#v", got, want)
	}
	if got, want := grantReader.queries[0].Limit, 1; got != want {
		t.Fatalf("grant query Limit = %d, want %d", got, want)
	}
}

func TestCreateWorkflowWorkIfNoOpenTargetsFiltersRequestedScopes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 12, 10, 0, 0, time.UTC)
	store := &fakeStore{}
	service := Service{
		Config: Config{
			TenantBoundary: WorkflowTenantBoundary{
				TenantID:           "tenant-a",
				WorkspaceID:        "workspace-a",
				SubjectClass:       "collector",
				PolicyRevisionHash: "policy-a",
			},
		},
		Store: store,
		TenantGrantReader: &fakeTenantGrantReader{
			grants: []WorkflowTenantScopeGrant{{
				ScopeID:            "aws:scope:lambda",
				PolicyRevisionHash: "policy-a",
			}},
		},
	}
	run := runForTenantGrantTest(now)
	run.RequestedScopeSet = `{"targets":[{"scope_id":"aws:scope:lambda"},{"scope_id":"aws:scope:s3"}]}`

	enqueued, err := service.createWorkflowWorkIfNoOpenTargets(
		context.Background(),
		collectorInstanceForTenantGrantTest(),
		run,
		[]workflow.WorkItem{
			workItemForTenantGrantTest(now, "aws:scope:lambda"),
			workItemForTenantGrantTest(now, "aws:scope:s3"),
		},
	)
	if err != nil {
		t.Fatalf("createWorkflowWorkIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := enqueued, 1; got != want {
		t.Fatalf("enqueued = %d, want %d", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got := store.createdRuns[0].RequestedScopeSet; strings.Contains(got, "aws:scope:s3") {
		t.Fatalf("RequestedScopeSet leaked denied scope: %s", got)
	}
	if got := store.createdRuns[0].RequestedScopeSet; !strings.Contains(got, "aws:scope:lambda") {
		t.Fatalf("RequestedScopeSet missing authorized scope: %s", got)
	}
}

func TestCreateWorkflowWorkIfNoOpenTargetsDropsUnknownScopeSetShape(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 12, 15, 0, 0, time.UTC)
	store := &fakeStore{}
	service := Service{
		Config: Config{
			TenantBoundary: WorkflowTenantBoundary{
				TenantID:           "tenant-a",
				WorkspaceID:        "workspace-a",
				SubjectClass:       "collector",
				PolicyRevisionHash: "policy-a",
			},
		},
		Store: store,
		TenantGrantReader: &fakeTenantGrantReader{
			grants: []WorkflowTenantScopeGrant{{
				ScopeID:            "aws:scope:lambda",
				PolicyRevisionHash: "policy-a",
			}},
		},
	}
	run := runForTenantGrantTest(now)
	run.RequestedScopeSet = `{"denied_scope_id":"aws:scope:s3"}`

	enqueued, err := service.createWorkflowWorkIfNoOpenTargets(
		context.Background(),
		collectorInstanceForTenantGrantTest(),
		run,
		[]workflow.WorkItem{
			workItemForTenantGrantTest(now, "aws:scope:lambda"),
			workItemForTenantGrantTest(now, "aws:scope:s3"),
		},
	)
	if err != nil {
		t.Fatalf("createWorkflowWorkIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := enqueued, 1; got != want {
		t.Fatalf("enqueued = %d, want %d", got, want)
	}
	if got, want := store.createdRuns[0].RequestedScopeSet, "[]"; got != want {
		t.Fatalf("RequestedScopeSet = %s, want %s", got, want)
	}
}

type fakeTenantGrantReader struct {
	grants  []WorkflowTenantScopeGrant
	queries []WorkflowTenantGrantQuery
}

func (f *fakeTenantGrantReader) ListWorkflowScopeGrants(
	_ context.Context,
	query WorkflowTenantGrantQuery,
) ([]WorkflowTenantScopeGrant, error) {
	query.ScopeIDs = append([]string(nil), query.ScopeIDs...)
	f.queries = append(f.queries, query)
	return append([]WorkflowTenantScopeGrant(nil), f.grants...), nil
}

func sameStringSet(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func collectorInstanceForTenantGrantTest() workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    "collector-aws",
		CollectorKind: scope.CollectorAWS,
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
	}
}

func runForTenantGrantTest(now time.Time) workflow.Run {
	return workflow.Run{
		RunID:              "run-tenant-grant",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func workItemForTenantGrantTest(now time.Time, scopeID string) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "item-" + scopeID,
		RunID:               "run-tenant-grant",
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: "collector-aws",
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             scopeID,
		AcceptanceUnitID:    `{"service_kind":"lambda"}`,
		SourceRunID:         "source-run-tenant-grant",
		GenerationID:        "generation-tenant-grant",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}
