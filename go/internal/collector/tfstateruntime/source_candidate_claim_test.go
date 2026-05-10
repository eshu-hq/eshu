package tfstateruntime_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceCollectsTerraformStateCandidateScopedWorkItem(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 9, 0, 0, 0, time.UTC)
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope(
		"repo-scope-123",
		string(stateKey.BackendKind),
		stateKey.Locator,
		map[string]string{"repo_id": "platform-infra"},
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	realGeneration, err := scope.NewTerraformStateSnapshotGeneration(
		scopeValue.ScopeID,
		17,
		"lineage-123",
		observedAt,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	candidate := terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     stateKey.Locator,
		},
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		RepoID: "platform-infra",
		Region: "us-east-1",
	}
	candidateID, err := terraformstate.CandidatePlanningID(candidate)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	factory := &fakeFactory{
		source: &fakeStateSource{
			key: stateKey,
			state: `{
				"format_version": "1.0",
				"terraform_version": "1.9.8",
				"serial": 17,
				"lineage": "lineage-123",
				"resources": []
			}`,
			observedAt: observedAt,
		},
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{seedFromCandidate(candidate)},
			},
		},
		SourceFactory: factory,
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-candidate-1",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         candidateID,
		GenerationID:        candidateID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true for matching candidate scope")
	}
	if got, want := collected.Scope.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := collected.Generation.GenerationID, realGeneration.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want real state generation %q", got, want)
	}
}

func TestClaimedSourceMatchesCandidateScopedWorkItemByVersionID(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 9, 10, 0, 0, time.UTC)
	locator := "s3://tfstate-prod/services/api/terraform.tfstate"
	first := terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     locator,
			VersionID:   "version-a",
		},
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		Region: "us-east-1",
	}
	second := first
	second.State.VersionID = "version-b"
	secondID, err := terraformstate.CandidatePlanningID(second)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope("", string(terraformstate.BackendS3), locator, nil)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	secondGeneration, err := scope.NewTerraformStateSnapshotGeneration(scopeValue.ScopeID, 22, "lineage-b", observedAt)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	factory := &candidateSourceFactory{
		sources: map[string]*fakeStateSource{
			"version-a": {
				key:        first.State,
				state:      `{"serial":11,"lineage":"lineage-a","resources":[]}`,
				observedAt: observedAt,
			},
			"version-b": {
				key:        second.State,
				state:      `{"serial":22,"lineage":"lineage-b","resources":[]}`,
				observedAt: observedAt,
			},
		},
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{
					seedFromCandidate(first),
					seedFromCandidate(second),
				},
			},
		},
		SourceFactory: factory,
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-candidate-version-1",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         secondID,
		GenerationID:        secondID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Generation.GenerationID, secondGeneration.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want version-b generation %q", got, want)
	}
	if got, want := factory.opened, []string{"version-b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("opened versions = %#v, want %#v", got, want)
	}
}

func TestClaimedSourceSkipsOpeningNonmatchingCandidateScope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 9, 15, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	candidate := terraformstate.DiscoveryCandidate{
		State:  stateKey,
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		Region: "us-east-1",
	}
	candidateID, err := terraformstate.CandidatePlanningID(candidate)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	factory := &countingFactory{
		source: &fakeStateSource{
			key:        stateKey,
			state:      `{"serial":17,"lineage":"lineage-123","resources":[]}`,
			observedAt: observedAt,
		},
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{seedFromCandidate(candidate)},
			},
		},
		SourceFactory: factory,
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-candidate-2",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             "state_snapshot:s3:other-locator-hash",
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         candidateID,
		GenerationID:        candidateID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false for nonmatching scope")
	}
	if got := factory.calls; got != 0 {
		t.Fatalf("OpenSource() calls = %d, want 0 for nonmatching scope", got)
	}
}

type countingFactory struct {
	calls  int
	source terraformstate.StateSource
}

func (f *countingFactory) OpenSource(context.Context, terraformstate.DiscoveryCandidate) (terraformstate.StateSource, error) {
	f.calls++
	return f.source, nil
}

type candidateSourceFactory struct {
	opened  []string
	sources map[string]*fakeStateSource
}

func (f *candidateSourceFactory) OpenSource(
	_ context.Context,
	candidate terraformstate.DiscoveryCandidate,
) (terraformstate.StateSource, error) {
	f.opened = append(f.opened, candidate.State.VersionID)
	return f.sources[candidate.State.VersionID], nil
}

func seedFromCandidate(candidate terraformstate.DiscoveryCandidate) terraformstate.DiscoverySeed {
	return terraformstate.DiscoverySeed{
		Kind:      candidate.State.BackendKind,
		Bucket:    "tfstate-prod",
		Key:       "services/api/terraform.tfstate",
		Region:    candidate.Region,
		RepoID:    candidate.RepoID,
		VersionID: candidate.State.VersionID,
	}
}
