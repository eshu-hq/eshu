// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"encoding/json"
	"strings"
	"testing"
)

// environment_url and log_url are set by whatever created the deployment, so
// they can carry a signed query string, an embedded credential, or a fragment.
// They land in a durable fact, so they must be stripped the same way artifact
// URLs already are.
func TestDeploymentEventEnvelopesStripSecretsFromProviderURLs(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(map[string]any{"deployments": []map[string]any{{
		"deployment": map[string]any{
			"id": 9001, "sha": "0f1e2d3c", "environment": "production",
		},
		"statuses": []map[string]any{{
			"id":              77001,
			"state":           "success",
			"environment_url": "https://app.example.invalid/live?token=SECRET-VALUE&sig=abc#frag",
			"log_url":         "https://logs.example.invalid/run?access_key=ANOTHER-SECRET",
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	envelopes, err := GitHubActionsDeploymentEnvelopes(raw, deploymentTestContext())
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes: %v", err)
	}
	if len(envelopes) == 0 {
		t.Fatal("no envelopes emitted")
	}

	for _, envelope := range envelopes {
		blob, marshalErr := json.Marshal(envelope.Payload)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		for _, secret := range []string{"SECRET-VALUE", "ANOTHER-SECRET", "token=", "access_key=", "sig=", "#frag"} {
			if strings.Contains(string(blob), secret) {
				t.Fatalf("durable payload retained %q from a provider URL: %s", secret, blob)
			}
		}
	}
}

// GitHub allows a deployment status to report a different environment than its
// parent -- a redeploy can retarget, which is why the parent keeps its first
// value in original_environment. The status is the more specific truth for that
// transition, so denormalizing the stale parent value would publish the wrong
// environment as deploy_event truth.
func TestDeploymentEventEnvelopesPreferStatusEnvironmentOverride(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(map[string]any{"deployments": []map[string]any{{
		"deployment": map[string]any{
			"id": 9001, "sha": "0f1e2d3c", "environment": "staging",
		},
		"statuses": []map[string]any{{
			"id": 77001, "state": "success", "environment": "production",
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	envelopes, err := GitHubActionsDeploymentEnvelopes(raw, deploymentTestContext())
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes: %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envelopes))
	}
	if got := envelopes[0].Payload["environment"]; got != "production" {
		t.Fatalf("environment = %v, want the status override \"production\" rather than the parent's \"staging\"", got)
	}
}

// A status that does not override keeps the parent's environment, so the
// override is a preference rather than a requirement.
func TestDeploymentEventEnvelopesFallBackToParentEnvironment(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(map[string]any{"deployments": []map[string]any{{
		"deployment": map[string]any{
			"id": 9001, "sha": "0f1e2d3c", "environment": "staging",
		},
		"statuses": []map[string]any{{"id": 77001, "state": "success"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	envelopes, err := GitHubActionsDeploymentEnvelopes(raw, deploymentTestContext())
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes: %v", err)
	}
	if got := envelopes[0].Payload["environment"]; got != "staging" {
		t.Fatalf("environment = %v, want the parent's \"staging\"", got)
	}
}

func deploymentTestContext() FixtureContext {
	return FixtureContext{
		CollectorInstanceID: "cicd-collector-test",
		ScopeID:             "ci_cd_run:github_actions:acme:api",
		GenerationID:        "gen-1",
		Repository:          "acme/api",
		SourceURI:           "https://github.com/acme/api",
	}
}
