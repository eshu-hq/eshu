package gcpruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestSourceNextClaimedCollectsMatchingWorkItem(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope()
	scopeCfg.FencingToken = 0
	scopeID := scopeCfg.withDefaults().ScopeID
	source := newSource(
		t,
		testConfig(scopeCfg),
		NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
			scopeID: {readFixturePage(t, "assets_list_page1.json")},
		}),
		nil,
	)

	item := gcpClaimedWorkItem(scopeID)
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.ScopeID, scopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := collected.Generation.GenerationID, item.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want work-item generation %q", got, want)
	}
	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got == 0 {
		t.Fatal("gcp_cloud_resource count = 0, want claimed source facts")
	}
}

func TestSourceNextClaimedRejectsUnauthorizedScope(t *testing.T) {
	t.Parallel()

	scopeCfg := testScope()
	scopeCfg.FencingToken = 0
	source := newSource(
		t,
		testConfig(scopeCfg),
		NewFixturePageProvider(nil),
		nil,
	)
	item := gcpClaimedWorkItem("gcp:project:not-configured:mixed:resource:global")

	_, _, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want unauthorized scope rejection")
	}
	if strings.Contains(err.Error(), "my-project") {
		t.Fatalf("NextClaimed() leaked configured parent id in error: %q", err.Error())
	}
}

func gcpClaimedWorkItem(scopeID string) workflow.WorkItem {
	now := time.Date(2026, 6, 18, 16, 0, 0, 0, time.UTC)
	return workflow.WorkItem{
		WorkItemID:          "gcp:gcp-instance-1:gcp-generation-1",
		RunID:               "gcp:gcp-instance-1:schedule:test",
		CollectorKind:       scope.CollectorGCP,
		CollectorInstanceID: "gcp-instance-1",
		SourceSystem:        string(scope.CollectorGCP),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "gcp-generation-1",
		GenerationID:        "gcp-generation-1",
		FairnessKey:         "gcp:gcp-instance-1:" + scopeID,
		Status:              workflow.WorkItemStatusClaimed,
		CurrentClaimID:      "claim-1",
		CurrentFencingToken: 41,
		CurrentOwnerID:      "collector-pod-1",
		LeaseExpiresAt:      now.Add(time.Minute),
		VisibleAt:           now,
		LastClaimedAt:       now,
		CreatedAt:           now,
		UpdatedAt:           now,
		PolicyRevisionHash:  "policy-revision",
		TenantID:            "tenant",
		WorkspaceID:         "workspace",
		SubjectClass:        "collector",
	}
}
