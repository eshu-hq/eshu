// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCassetteShapedDeploymentEventsResolveCanonicalEnvironment is the
// framework-tier proof for the golden-corpus floor, using the same payload
// shapes the cicdrun cassette carries.
//
// It exists because the corpus caught what this should have: the first version
// of the B-7 assertion filtered on environment=production and returned zero
// rows. The cassette fact legitimately says "production" -- that is what the
// GitHub Deployments API returns -- but the reducer canonicalizes through
// environment.Canonical, so the durable value is "prod". A consumer filtering
// on the provider's raw string silently gets nothing back.
//
// Pinning the canonical token here means a change to the alias table, or a
// deploy-event path that forgets to canonicalize the way the declared path
// does, fails in milliseconds instead of in a three-minute corpus run.
func TestCassetteShapedDeploymentEventsResolveCanonicalEnvironment(t *testing.T) {
	t.Parallel()

	const sha = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

	envelopes := []facts.Envelope{
		{
			FactID:   "cassette-run",
			FactKind: facts.CICDRunFactKind,
			Payload: map[string]any{
				"provider":      "github_actions",
				"run_id":        "5150",
				"run_attempt":   "1",
				"commit_sha":    sha,
				"repository_id": "repository:r_69256c06",
			},
		},
		{
			FactID:   "cassette-deployment-in-progress",
			FactKind: facts.CICDDeploymentEventFactKind,
			Payload: map[string]any{
				"provider":      "github_actions",
				"deployment_id": "9001",
				"status_id":     "77001",
				"environment":   "production",
				"sha":           sha,
				"state":         "in_progress",
			},
		},
		{
			FactID:   "cassette-deployment-success",
			FactKind: facts.CICDDeploymentEventFactKind,
			Payload: map[string]any{
				"provider":      "github_actions",
				"deployment_id": "9001",
				"status_id":     "77002",
				"environment":   "production",
				"sha":           sha,
				"state":         "success",
			},
		},
	}

	decisions, quarantined, _, _ := buildCICDRunCorrelationDecisionsWithQuarantine(envelopes)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0: %#v", len(quarantined), quarantined)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}

	decision := decisions[0]
	if decision.Environment != "prod" {
		t.Fatalf("Environment = %q, want the canonical token %q; the fact carries the provider's raw "+
			"\"production\" and environment.Canonical normalizes it", decision.Environment, "prod")
	}
	if decision.EnvironmentEvidence != "deploy_event" {
		t.Fatalf("EnvironmentEvidence = %q, want deploy_event", decision.EnvironmentEvidence)
	}
}
