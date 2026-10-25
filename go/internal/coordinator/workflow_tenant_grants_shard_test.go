// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// A sharded security-alert org target (#9019) collects through generated
// shard scopes the operator never grants one by one. Each shard, and each
// retirement of one, is part of the base target's work, so it is authorized
// by the base scope's grant and by nothing else.
func TestCreateWorkflowWorkAuthorizesSecurityAlertShardsByTheBaseGrant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC)
	const base = "security-alert:github-org:acme"
	store := &fakeStore{}
	grants := &fakeTenantGrantReader{grants: []WorkflowTenantScopeGrant{{ScopeID: base, PolicyRevisionHash: "policy-a"}}}
	service := Service{
		Config: Config{TenantBoundary: WorkflowTenantBoundary{
			TenantID: "tenant-a", WorkspaceID: "workspace-a", SubjectClass: "collector", PolicyRevisionHash: "policy-a",
		}},
		Store:             store,
		TenantGrantReader: grants,
	}
	item := func(scopeID, acceptanceUnitID string) workflow.WorkItem {
		w := workItemForTenantGrantTest(now, scopeID)
		w.CollectorKind, w.SourceSystem, w.AcceptanceUnitID = scope.CollectorSecurityAlert, string(scope.CollectorSecurityAlert), acceptanceUnitID
		return w
	}
	shard0, shard1 := shard.ScopeID(base, "0"), shard.ScopeID(base, "1")
	other := shard.ScopeID("security-alert:github-org:other", "0")
	instance := collectorInstanceForTenantGrantTest()
	instance.CollectorKind = scope.CollectorSecurityAlert
	enqueued, err := service.createWorkflowWorkIfNoOpenTargets(context.Background(), instance, runForTenantGrantTest(now), []workflow.WorkItem{
		item(shard0, shard0),
		item(shard1, shard1),
		item(base, shard.RetirementAcceptanceUnitID(base)),
		item(other, other),
	})
	if err != nil {
		t.Fatalf("createWorkflowWorkIfNoOpenTargets() error = %v", err)
	}
	if enqueued != 3 {
		t.Fatalf("enqueued = %d, want the two shards and the base retirement (the other target has no grant)", enqueued)
	}
	if got := grants.queries[0].ScopeIDs; !slices.Equal(got, []string{base, "security-alert:github-org:other"}) {
		t.Fatalf("grant query scope ids = %v, want the base scope ids", got)
	}
	for _, enq := range store.enqueuedItems {
		if enq.PolicyRevisionHash != "policy-a" || enq.TenantID != "tenant-a" {
			t.Fatalf("item %q tenant stamp = %q/%q", enq.ScopeID, enq.TenantID, enq.PolicyRevisionHash)
		}
	}
}
