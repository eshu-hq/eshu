// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// deploymentFixtureCtx builds a FixtureContext for the GitHub Deployments API
// tests below, mirroring the run-fixture tests' fixed observedAt/scope
// pattern (github_actions_fixture_test.go) but carrying the Repository field
// deployment identity needs (#5425 STEP 3).
func deploymentFixtureCtx(scopeID, repository string) FixtureContext {
	return FixtureContext{
		ScopeID:             scopeID,
		GenerationID:        "generation-1",
		CollectorInstanceID: "fixture-gh-deployments",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		SourceURI:           "https://github.com/example/repo",
		Repository:          repository,
	}
}

// singleStatusDeploymentRaw builds one deployment with one status event.
func singleStatusDeploymentRaw(deploymentID, statusID int, state string) []byte {
	return []byte(`{"deployments":[{
		"deployment":{"id":` + strconv.Itoa(deploymentID) + `,"sha":"0123456789abcdef0123456789abcdef01234567","ref":"main","task":"deploy","environment":"production","original_environment":"production","transient_environment":false,"production_environment":true,"created_at":"2026-07-20T00:00:00Z","updated_at":"2026-07-20T00:00:00Z"},
		"statuses":[{"id":` + strconv.Itoa(statusID) + `,"state":"` + state + `","created_at":"2026-07-20T00:01:00Z","updated_at":"2026-07-20T00:01:00Z"}]
	}]}`)
}

func TestGitHubActionsDeploymentEnvelopesStableKeyIdenticalAcrossPolls(t *testing.T) {
	t.Parallel()

	ctx := deploymentFixtureCtx("ci-cd:github-actions:example/repo", "example/repo")
	raw := singleStatusDeploymentRaw(9001, 8001, "success")

	first, err := GitHubActionsDeploymentEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
	}
	second, err := GitHubActionsDeploymentEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
	}

	firstEvents := envelopesByKind(first)[facts.CICDDeploymentEventFactKind]
	secondEvents := envelopesByKind(second)[facts.CICDDeploymentEventFactKind]
	if len(firstEvents) != 1 || len(secondEvents) != 1 {
		t.Fatalf("want exactly 1 deployment event per poll, got %d and %d", len(firstEvents), len(secondEvents))
	}
	if firstEvents[0].StableFactKey != secondEvents[0].StableFactKey {
		t.Fatalf("StableFactKey = %q vs %q, want identical across polls of the same deployment+status",
			firstEvents[0].StableFactKey, secondEvents[0].StableFactKey)
	}
}

func TestGitHubActionsDeploymentEnvelopesStableKeyDiffersPerStatusTransition(t *testing.T) {
	t.Parallel()

	ctx := deploymentFixtureCtx("ci-cd:github-actions:example/repo", "example/repo")
	raw := []byte(`{"deployments":[{
		"deployment":{"id":9001,"sha":"0123456789abcdef0123456789abcdef01234567","environment":"production"},
		"statuses":[
			{"id":8001,"state":"pending"},
			{"id":8002,"state":"success"}
		]
	}]}`)

	envelopes, err := GitHubActionsDeploymentEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
	}
	events := envelopesByKind(envelopes)[facts.CICDDeploymentEventFactKind]
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 (pending + success on the same deployment)", len(events))
	}
	if events[0].StableFactKey == events[1].StableFactKey {
		t.Fatalf("StableFactKey identical for pending and success events, want distinct durable facts per status transition")
	}
}

func TestGitHubActionsDeploymentEnvelopesEmitsOneEnvelopeWithEmptyStatusIDWhenNoStatuses(t *testing.T) {
	t.Parallel()

	ctx := deploymentFixtureCtx("ci-cd:github-actions:example/repo", "example/repo")
	raw := []byte(`{"deployments":[{
		"deployment":{"id":9001,"sha":"0123456789abcdef0123456789abcdef01234567","environment":"production"},
		"statuses":[]
	}]}`)

	envelopes, err := GitHubActionsDeploymentEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
	}
	events := envelopesByKind(envelopes)[facts.CICDDeploymentEventFactKind]
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want exactly 1 for a zero-status deployment", len(events))
	}
	assertPayload(t, events[0].Payload, "status_id", "")
	assertPayload(t, events[0].Payload, "deployment_id", "9001")
}

func TestGitHubActionsDeploymentEnvelopesDistinctScopesSameDeploymentIDProduceDistinctKeys(t *testing.T) {
	t.Parallel()

	raw := singleStatusDeploymentRaw(9001, 8001, "success")
	ctxA := deploymentFixtureCtx("ci-cd:github-actions:example/repo-a", "example/repo-a")
	ctxB := deploymentFixtureCtx("ci-cd:github-actions:example/repo-b", "example/repo-b")

	envelopesA, err := GitHubActionsDeploymentEnvelopes(raw, ctxA)
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
	}
	envelopesB, err := GitHubActionsDeploymentEnvelopes(raw, ctxB)
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
	}
	eventA := envelopesByKind(envelopesA)[facts.CICDDeploymentEventFactKind][0]
	eventB := envelopesByKind(envelopesB)[facts.CICDDeploymentEventFactKind][0]
	if eventA.StableFactKey == eventB.StableFactKey {
		t.Fatalf("StableFactKey identical across two different scopes/repositories sharing numeric deployment id 9001, want distinct (provider-neutrality: GitLab deployment iids are per-project)")
	}
}

func TestGitHubActionsDeploymentEnvelopesDenormalizesParentFieldsOntoEveryStatusRow(t *testing.T) {
	t.Parallel()

	ctx := deploymentFixtureCtx("ci-cd:github-actions:example/repo", "example/repo")
	raw := []byte(`{"deployments":[{
		"deployment":{"id":9001,"sha":"0123456789abcdef0123456789abcdef01234567","ref":"main","task":"deploy","environment":"production","original_environment":"production","transient_environment":false,"production_environment":true},
		"statuses":[
			{"id":8001,"state":"pending"},
			{"id":8002,"state":"success"}
		]
	}]}`)

	envelopes, err := GitHubActionsDeploymentEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("GitHubActionsDeploymentEnvelopes() error = %v, want nil", err)
	}
	events := envelopesByKind(envelopes)[facts.CICDDeploymentEventFactKind]
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	for _, event := range events {
		assertPayload(t, event.Payload, "sha", "0123456789abcdef0123456789abcdef01234567")
		assertPayload(t, event.Payload, "ref", "main")
		assertPayload(t, event.Payload, "task", "deploy")
		assertPayload(t, event.Payload, "environment", "production")
		assertPayload(t, event.Payload, "original_environment", "production")
		assertPayload(t, event.Payload, "production_environment", true)
		assertPayload(t, event.Payload, "transient_environment", false)
	}
}
