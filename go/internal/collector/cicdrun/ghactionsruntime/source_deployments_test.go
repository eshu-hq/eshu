// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fakeDeploymentClient embeds fakeClient (FetchRuns) and adds FetchDeployments,
// so it satisfies both Client and DeploymentFetcher without touching every
// other run-collection test double in this package -- see DeploymentFetcher's
// doc comment in source_deployments.go for why that split exists.
type fakeDeploymentClient struct {
	fakeClient
	deploymentPage DeploymentPage
	deploymentErr  error
}

func (f fakeDeploymentClient) FetchDeployments(context.Context, TargetConfig) (DeploymentPage, error) {
	return f.deploymentPage, f.deploymentErr
}

func newDeploymentClaimedSource(t *testing.T, client fakeDeploymentClient) ClaimedSource {
	t.Helper()
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Now:                 func() time.Time { return time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC) },
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			SourceURI:           "https://github.com/example/repo",
			MaxRuns:             1,
			MaxJobs:             10,
			MaxArtifacts:        10,
			MaxDeployments:      10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return source
}

func claimDeploymentWorkItem() workflow.WorkItem {
	return workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	}
}

func filterFactKind(envelopes []facts.Envelope, factKind string) []facts.Envelope {
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			out = append(out, envelope)
		}
	}
	return out
}

// TestClaimedSourceEmitsDeploymentEventsInSameGenerationAsRunFacts covers
// test 11: ci.deployment_event facts MUST land in the SAME
// CollectedGeneration (same ScopeID+GenerationID) as the ci.run facts from
// the same claim, because the reducer's correlation intent
// (ci_cd_run_correlation_deploy_events.go) only forms for a generation
// containing a ci.run -- a deployment fact emitted into a different
// generation would never be attached.
func TestClaimedSourceEmitsDeploymentEventsInSameGenerationAsRunFacts(t *testing.T) {
	t.Parallel()

	client := fakeDeploymentClient{
		fakeClient: fakeClient{page: RunPage{Snapshots: []RunSnapshot{{
			Run: map[string]any{
				"id":       1001,
				"head_sha": "0123456789abcdef0123456789abcdef01234567",
				"repository": map[string]any{
					"full_name": "example/repo",
					"html_url":  "https://github.com/example/repo",
				},
			},
		}}}},
		deploymentPage: DeploymentPage{Snapshots: []DeploymentSnapshot{{
			Deployment: map[string]any{
				"id":          9001,
				"sha":         "0123456789abcdef0123456789abcdef01234567",
				"environment": "production",
			},
			Statuses: []map[string]any{{"id": 8001, "state": "success"}},
		}}},
	}
	source := newDeploymentClaimedSource(t, client)

	collected, ok, err := source.NextClaimed(context.Background(), claimDeploymentWorkItem())
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	envelopes := drainFacts(t, collected.Facts)
	runFact := requireFactKind(t, envelopes, facts.CICDRunFactKind)
	deploymentFact := requireFactKind(t, envelopes, facts.CICDDeploymentEventFactKind)
	if deploymentFact.ScopeID != runFact.ScopeID {
		t.Fatalf("deployment ScopeID = %q, run ScopeID = %q, want identical (same CollectedGeneration)", deploymentFact.ScopeID, runFact.ScopeID)
	}
	if deploymentFact.GenerationID != runFact.GenerationID {
		t.Fatalf("deployment GenerationID = %q, run GenerationID = %q, want identical (same CollectedGeneration)", deploymentFact.GenerationID, runFact.GenerationID)
	}
	if collected.Generation.ScopeID != deploymentFact.ScopeID || collected.Generation.GenerationID != deploymentFact.GenerationID {
		t.Fatalf("deployment fact scope/generation = %q/%q, want it to match the CollectedGeneration %q/%q",
			deploymentFact.ScopeID, deploymentFact.GenerationID, collected.Generation.ScopeID, collected.Generation.GenerationID)
	}
}

// TestClaimedSourceEmitsUnanchoredDeploymentWarning covers test 12: a
// deployment event whose sha matches none of the claim's fetched run head
// shas still emits its ci.deployment_event fact (evidence is never
// dropped at collection time) AND a deployment_unanchored ci.warning, so the
// gap is visible to an operator instead of the fact silently going nowhere
// once the reducer's sha-based attach (attachDeploymentEventsToRuns) finds
// no match.
func TestClaimedSourceEmitsUnanchoredDeploymentWarning(t *testing.T) {
	t.Parallel()

	client := fakeDeploymentClient{
		fakeClient: fakeClient{page: RunPage{Snapshots: []RunSnapshot{{
			Run: map[string]any{
				"id":       1001,
				"head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"repository": map[string]any{
					"full_name": "example/repo",
					"html_url":  "https://github.com/example/repo",
				},
			},
		}}}},
		deploymentPage: DeploymentPage{Snapshots: []DeploymentSnapshot{{
			Deployment: map[string]any{
				"id":          9001,
				"sha":         "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"environment": "production",
			},
		}}},
	}
	source := newDeploymentClaimedSource(t, client)

	collected, ok, err := source.NextClaimed(context.Background(), claimDeploymentWorkItem())
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	envelopes := drainFacts(t, collected.Facts)
	requireFactKind(t, envelopes, facts.CICDDeploymentEventFactKind)

	warnings := filterFactKind(envelopes, facts.CICDWarningFactKind)
	found := false
	for _, warning := range warnings {
		if warning.Payload["reason"] == "deployment_unanchored" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no ci.warning with reason=deployment_unanchored among %d warnings: %#v", len(warnings), warnings)
	}
}
