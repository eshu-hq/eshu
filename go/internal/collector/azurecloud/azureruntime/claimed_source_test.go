package azureruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// newClaimedFixtureSource builds a fixture-backed source with a non-zero
// redaction key, matching the claimed-live contract that the source must be
// keyed before it serves a claim.
func newClaimedFixtureSource(t *testing.T, provider azurecloud.PageProvider, targets ...TargetConfig) *Source {
	t.Helper()
	src := newFixtureSource(t, provider, targets...)
	key, err := redact.NewKey([]byte("azure-claimed-source-test-key"))
	if err != nil {
		t.Fatalf("redact.NewKey: %v", err)
	}
	src.RedactionKey = key
	return src
}

// claimedScopeID returns the durable Eshu scope id the configured testTarget
// resolves to once defaults are applied, so claimed-work tests address the same
// shard the source observes.
func claimedScopeID(t *testing.T) string {
	t.Helper()
	validated, err := testTarget().validated()
	if err != nil {
		t.Fatalf("validate test target: %v", err)
	}
	return scopeIDForTarget(validated)
}

func azureClaimedWorkItem(scopeID string) workflow.WorkItem {
	now := time.Date(2026, 6, 18, 16, 0, 0, 0, time.UTC)
	return workflow.WorkItem{
		WorkItemID:          "azure:azure-collector-1:azure-generation-1",
		RunID:               "azure:azure-collector-1:schedule:test",
		CollectorKind:       scope.CollectorAzure,
		CollectorInstanceID: "azure-collector-1",
		SourceSystem:        string(scope.CollectorAzure),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "azure-generation-1",
		GenerationID:        "azure-generation-1",
		FairnessKey:         "azure:azure-collector-1:" + scopeID,
		Status:              workflow.WorkItemStatusClaimed,
		CurrentClaimID:      "claim-1",
		CurrentFencingToken: 53,
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

func TestSourceNextClaimedCollectsMatchingWorkItem(t *testing.T) {
	t.Parallel()

	provider := NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{})
	src := newClaimedFixtureSource(t, provider, testTarget())

	scopeID := claimedScopeID(t)
	item := azureClaimedWorkItem(scopeID)
	collected, ok, err := src.NextClaimed(context.Background(), item)
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
	envs := drain(t, collected)
	resources := factsOfKind(envs, facts.AzureCloudResourceFactKind)
	if len(resources) == 0 {
		t.Fatal("azure_cloud_resource count = 0, want claimed source facts")
	}
	for _, env := range envs {
		if env.GenerationID != item.GenerationID {
			t.Fatalf("fact generation %q != claim generation %q", env.GenerationID, item.GenerationID)
		}
		if env.FencingToken != item.CurrentFencingToken {
			t.Fatalf("fact fencing token = %d, want claim fencing token %d", env.FencingToken, item.CurrentFencingToken)
		}
	}
}

func TestSourceNextClaimedRejectsUnauthorizedScope(t *testing.T) {
	t.Parallel()

	src := newClaimedFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	item := azureClaimedWorkItem("azure:tenant-zzz:subscription:not-configured:all:all:resource_graph")

	_, _, err := src.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want unauthorized scope rejection")
	}
	if strings.Contains(err.Error(), testTarget().ProviderScopeID) {
		t.Fatalf("NextClaimed() leaked configured provider scope id in error: %q", err.Error())
	}
}

func TestSourceNextClaimedRejectsMismatchedInstance(t *testing.T) {
	t.Parallel()

	src := newClaimedFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	item := azureClaimedWorkItem(claimedScopeID(t))
	item.CollectorInstanceID = "azure-collector-OTHER"

	if _, _, err := src.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want collector instance mismatch rejection")
	}
}

func TestSourceNextClaimedRejectsNonPositiveFencingToken(t *testing.T) {
	t.Parallel()

	src := newClaimedFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	item := azureClaimedWorkItem(claimedScopeID(t))
	item.CurrentFencingToken = 0

	if _, _, err := src.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want non-positive fencing token rejection")
	}
}

func TestSourceNextClaimedRejectsWrongCollectorKind(t *testing.T) {
	t.Parallel()

	src := newClaimedFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	item := azureClaimedWorkItem(claimedScopeID(t))
	item.CollectorKind = scope.CollectorGCP

	if _, _, err := src.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want collector kind rejection")
	}
}

func TestSourceNextClaimedRejectsUnclaimedStatus(t *testing.T) {
	t.Parallel()

	src := newClaimedFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	item := azureClaimedWorkItem(claimedScopeID(t))
	item.Status = workflow.WorkItemStatusPending

	if _, _, err := src.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want non-claimed status rejection")
	}
}

func TestSourceNextClaimedRejectsGenerationRunMismatch(t *testing.T) {
	t.Parallel()

	src := newClaimedFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	item := azureClaimedWorkItem(claimedScopeID(t))
	item.SourceRunID = "azure-generation-2"

	if _, _, err := src.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want source_run_id/generation_id mismatch rejection")
	}
}

func TestSourceNextClaimedRejectsZeroRedactionKey(t *testing.T) {
	t.Parallel()

	// A source with a zero redaction key must refuse claimed-live work so tag
	// observation facts never carry an unkeyed marker (parity with GCP).
	src := newFixtureSource(t, NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{}), testTarget())
	if !src.RedactionKey.IsZero() {
		t.Fatal("test precondition: fixture source must start with a zero redaction key")
	}
	item := azureClaimedWorkItem(claimedScopeID(t))

	if _, _, err := src.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want zero redaction key rejection")
	}
}
