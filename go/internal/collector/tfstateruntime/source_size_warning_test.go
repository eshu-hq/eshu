package tfstateruntime_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsWarningGenerationForOversizedState(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
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
	candidate := terraformstate.DiscoveryCandidate{
		State:  stateKey,
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		RepoID: "platform-infra",
		Region: "us-east-1",
	}
	candidateID, err := terraformstate.CandidatePlanningID(candidate)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{seedFromCandidate(candidate)},
			},
		},
		SourceFactory: &fakeFactory{
			source: &fakeStateSource{
				key:        stateKey,
				openErr:    fmt.Errorf("%w: size=1024 max=512", terraformstate.ErrStateTooLarge),
				observedAt: observedAt,
			},
		},
		RedactionKey: key,
		Clock:        func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-too-large",
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
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if err := collected.Generation.ValidateForScope(scopeValue); err != nil {
		t.Fatalf("warning generation ValidateForScope() error = %v, want nil", err)
	}
	if !strings.Contains(collected.Generation.FreshnessHint, "warning=state_too_large") {
		t.Fatalf("FreshnessHint = %q, want state_too_large warning", collected.Generation.FreshnessHint)
	}

	warning := factByKind(t, drainRuntimeFacts(t, collected.Facts), facts.TerraformStateWarningFactKind)
	if got, want := warning.Payload["warning_kind"], "state_too_large"; got != want {
		t.Fatalf("warning_kind = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["source"], string(terraformstate.DiscoveryCandidateSourceSeed); got != want {
		t.Fatalf("source = %#v, want %#v", got, want)
	}
	if strings.Contains(warning.SourceRef.SourceURI, stateKey.Locator) ||
		strings.Contains(warning.SourceRef.SourceRecordID, stateKey.Locator) {
		t.Fatalf("warning source ref leaked state locator: %#v", warning.SourceRef)
	}
	if got, want := warning.FencingToken, int64(42); got != want {
		t.Fatalf("FencingToken = %d, want %d", got, want)
	}
}
