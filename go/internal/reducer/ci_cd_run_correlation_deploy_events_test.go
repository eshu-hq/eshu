// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// deploymentEventEvidence builds a minimal decodedCICDDeploymentEvent for
// attach-by-sha tests, where state/deployment_id/status_id do not matter.
func deploymentEventEvidence(factID, sha, environment string) *decodedCICDDeploymentEvent {
	return deploymentEventEvidenceWithState(factID, sha, environment, "", "dep-1", "")
}

// deploymentEventEvidenceWithState builds a decodedCICDDeploymentEvent with an
// explicit state, deployment_id, and status_id, for the deterministic
// selection tests. An empty state or statusID leaves the corresponding
// optional pointer field nil, matching a deployment-creation event with no
// status transition yet.
func deploymentEventEvidenceWithState(factID, sha, environment, state, deploymentID, statusID string) *decodedCICDDeploymentEvent {
	evidence := cicdrunv1.DeploymentEvent{
		Provider:     "github_actions",
		DeploymentID: deploymentID,
		Environment:  environment,
		SHA:          sha,
	}
	if state != "" {
		evidence.State = strPtr(state)
	}
	if statusID != "" {
		evidence.StatusID = strPtr(statusID)
	}
	return &decodedCICDDeploymentEvent{
		envelope: facts.Envelope{FactID: factID},
		evidence: evidence,
	}
}

// cicdRunEvidenceWithCommit builds a *cicdRunEvidence whose only decoded field
// that matters for attach-by-sha is CommitSHA. An empty commitSHA leaves the
// run with no commit anchor, matching a run whose ci.run fact never resolved
// one.
func cicdRunEvidenceWithCommit(commitSHA string) *cicdRunEvidence {
	if commitSHA == "" {
		return &cicdRunEvidence{runDecoded: cicdrunv1.Run{}}
	}
	return &cicdRunEvidence{runDecoded: cicdrunv1.Run{CommitSHA: strPtr(commitSHA)}}
}

// 1. One run, one event with a matching sha: the event attaches.
func TestAttachDeploymentEventsToRunsMatchesBySHA(t *testing.T) {
	t.Parallel()

	run := cicdRunEvidenceWithCommit("abc123")
	runs := map[string]*cicdRunEvidence{"run-1": run}
	event := deploymentEventEvidence("dep-event-1", "abc123", "prod")

	attachDeploymentEventsToRuns(runs, []*decodedCICDDeploymentEvent{event})

	if len(run.deploymentEvents) != 1 || run.deploymentEvents[0] != event {
		t.Fatalf("deploymentEvents = %#v, want exactly [event]", run.deploymentEvents)
	}
}

// 2. Two runs sharing a head sha, one event: BOTH runs receive it.
func TestAttachDeploymentEventsToRunsFansOutToSharedHeadSHA(t *testing.T) {
	t.Parallel()

	runA := cicdRunEvidenceWithCommit("abc123")
	runB := cicdRunEvidenceWithCommit("abc123")
	runs := map[string]*cicdRunEvidence{"run-a": runA, "run-b": runB}
	event := deploymentEventEvidence("dep-event-1", "abc123", "prod")

	attachDeploymentEventsToRuns(runs, []*decodedCICDDeploymentEvent{event})

	if len(runA.deploymentEvents) != 1 || runA.deploymentEvents[0] != event {
		t.Fatalf("runA.deploymentEvents = %#v, want exactly [event]", runA.deploymentEvents)
	}
	if len(runB.deploymentEvents) != 1 || runB.deploymentEvents[0] != event {
		t.Fatalf("runB.deploymentEvents = %#v, want exactly [event]", runB.deploymentEvents)
	}
}

// 3. An event sha that matches no run attaches to nothing, panics nothing, and
// leaves the run's decision unaffected (asserted via the empty attach result
// itself; classification behavior is covered separately).
func TestAttachDeploymentEventsToRunsNoMatchAttachesNothing(t *testing.T) {
	t.Parallel()

	run := cicdRunEvidenceWithCommit("abc123")
	runs := map[string]*cicdRunEvidence{"run-1": run}
	event := deploymentEventEvidence("dep-event-1", "def456", "prod")

	attachDeploymentEventsToRuns(runs, []*decodedCICDDeploymentEvent{event})

	if len(run.deploymentEvents) != 0 {
		t.Fatalf("deploymentEvents = %#v, want none for a non-matching sha", run.deploymentEvents)
	}
}

// 4. A run with an empty CommitSHA matches nothing, even against an event
// that also carries an empty sha — two empty strings must never join.
func TestAttachDeploymentEventsToRunsEmptyCommitMatchesNothing(t *testing.T) {
	t.Parallel()

	run := cicdRunEvidenceWithCommit("")
	runs := map[string]*cicdRunEvidence{"run-1": run}
	event := deploymentEventEvidence("dep-event-1", "", "prod")

	attachDeploymentEventsToRuns(runs, []*decodedCICDDeploymentEvent{event})

	if len(run.deploymentEvents) != 0 {
		t.Fatalf("deploymentEvents = %#v, want none for an empty-commit run", run.deploymentEvents)
	}
}

// 5. A run's attached deployment event environment beats its declared
// (ci.environment_observation) environment, and EnvironmentEvidence records
// which evidence won.
func TestClassifyCICDRunEvidencePrefersDeploymentEventEnvironment(t *testing.T) {
	t.Parallel()

	sha := "abc123"
	repo := "repo-api"
	ev := &cicdRunEvidence{
		run:                 facts.Envelope{FactID: "run-fact"},
		runDecoded:          cicdrunv1.Run{Provider: "github_actions", RunID: "run-1", RepositoryID: &repo, CommitSHA: &sha},
		environments:        []facts.Envelope{{FactID: "env-fact"}},
		environmentsDecoded: []cicdrunv1.EnvironmentObservation{{Environment: strPtr("staging")}},
		deploymentEvents: []*decodedCICDDeploymentEvent{
			deploymentEventEvidenceWithState("dep-1", sha, "production", "success", "10", "100"),
		},
	}

	decision := classifyCICDRunEvidence(ev, map[string][]cicdImageIdentity{})

	if decision.Environment != "prod" {
		t.Fatalf("Environment = %q, want %q (canonicalized from the deploy event, not the declared observation)", decision.Environment, "prod")
	}
	if decision.EnvironmentEvidence != "deploy_event" {
		t.Fatalf("EnvironmentEvidence = %q, want %q", decision.EnvironmentEvidence, "deploy_event")
	}
	if !stringSliceContains(decision.EvidenceFactIDs, "dep-1") {
		t.Fatalf("EvidenceFactIDs = %#v, want the winning deployment event's fact id", decision.EvidenceFactIDs)
	}
}

// 6. No attached deployment events: the existing declared-environment path
// still wins, and EnvironmentEvidence == "declared" proves no regression.
func TestClassifyCICDRunEvidenceFallsBackToDeclaredEnvironment(t *testing.T) {
	t.Parallel()

	sha := "abc123"
	repo := "repo-api"
	ev := &cicdRunEvidence{
		run:                 facts.Envelope{FactID: "run-fact"},
		runDecoded:          cicdrunv1.Run{Provider: "github_actions", RunID: "run-1", RepositoryID: &repo, CommitSHA: &sha},
		environments:        []facts.Envelope{{FactID: "env-fact"}},
		environmentsDecoded: []cicdrunv1.EnvironmentObservation{{Environment: strPtr("staging")}},
	}

	decision := classifyCICDRunEvidence(ev, map[string][]cicdImageIdentity{})

	if decision.Environment != "stage" {
		t.Fatalf("Environment = %q, want %q (canonicalized declared observation)", decision.Environment, "stage")
	}
	if decision.EnvironmentEvidence != "declared" {
		t.Fatalf("EnvironmentEvidence = %q, want %q", decision.EnvironmentEvidence, "declared")
	}
}

// 7. Determinism: feeding the same events in two different slice orders must
// select the same winner both times, ranked by state (success > in_progress >
// other) and tie-broken by the highest deployment_id then status_id as
// strings.
func TestSelectDeploymentEventIsDeterministicAcrossOrder(t *testing.T) {
	t.Parallel()

	success := deploymentEventEvidenceWithState("dep-success", "abc123", "production", "success", "5", "50")
	pending := deploymentEventEvidenceWithState("dep-pending", "abc123", "staging", "pending", "9", "90")
	inProgress := deploymentEventEvidenceWithState("dep-in-progress", "abc123", "canary", "in_progress", "20", "200")
	higherID := deploymentEventEvidenceWithState("dep-higher-id", "abc123", "production", "success", "6", "60")

	orderA := []*decodedCICDDeploymentEvent{success, pending, inProgress, higherID}
	orderB := []*decodedCICDDeploymentEvent{higherID, inProgress, pending, success}

	winnerA := selectDeploymentEvent(orderA)
	winnerB := selectDeploymentEvent(orderB)

	if winnerA == nil || winnerB == nil {
		t.Fatalf("selectDeploymentEvent returned nil: A=%v B=%v", winnerA, winnerB)
	}
	if winnerA.envelope.FactID != winnerB.envelope.FactID {
		t.Fatalf("winner differs by slice order: A=%q B=%q, want the same winner regardless of order", winnerA.envelope.FactID, winnerB.envelope.FactID)
	}
	if winnerA.envelope.FactID != "dep-higher-id" {
		t.Fatalf("winner FactID = %q, want %q (highest deployment_id among the success-state events)", winnerA.envelope.FactID, "dep-higher-id")
	}
}

// 8. The published reducer_ci_cd_run_correlation payload carries
// environment_evidence, the seam #5426 will branch on.
func TestCICDRunCorrelationPayloadIncludesEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	payload := cicdRunCorrelationPayload(CICDRunCorrelationWrite{}, CICDRunCorrelationDecision{
		Provider:            "github_actions",
		RunID:               "run-1",
		EnvironmentEvidence: "deploy_event",
	})

	if got, want := payload["environment_evidence"], "deploy_event"; got != want {
		t.Fatalf("environment_evidence = %#v, want %#v", got, want)
	}
}

// 9. A ci.deployment_event fact missing its required sha field is quarantined
// as an input_invalid dead-letter, not silently dropped and not fatal to the
// rest of the intent's batch.
func TestCICDRunCorrelationHandlerQuarantinesDeploymentEventMissingSHA(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-deploy-event",
		FactKind: facts.CICDDeploymentEventFactKind,
		Payload: map[string]any{
			"provider":      "github_actions",
			"deployment_id": "1",
			"environment":   "production",
			// "sha" intentionally absent.
		},
	}
	validRun := ciRunFact("run-deploy-valid", "github_actions", "repo-api", "abc123")

	loader := &stubCICDRunCorrelationFactLoader{scopeFacts: []facts.Envelope{malformed, validRun}}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-cicd-deploy-event-quarantine",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-deploy-valid:1",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed",
	})
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed ci.deployment_event fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-sha deployment event must be recorded as one input_invalid quarantine", got)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	found := false
	for _, decision := range writer.write.Decisions {
		if decision.RunID == "run-deploy-valid" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no decision produced for the valid sibling run run-deploy-valid; got %+v", writer.write.Decisions)
	}
}

// An event whose environment canonicalizes to nothing carries no deployment
// truth, so it must not win the environment and must not stamp
// environment_evidence=deploy_event.
//
// Reporting it would suppress the declared fallback and publish an empty
// environment labelled as deployment evidence, which #5426 reads as truth. The
// typed decode enforces presence and non-null but not non-empty, and the
// contract module serves external collectors and cassettes as well as this
// repo's collector, so the guard cannot live only at the emitter.
func TestClassifyDeploymentEventEnvironmentRejectsBlankEnvironment(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"empty":      "",
		"whitespace": "   ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env, factID, ok := classifyCICDDeploymentEventEnvironment(
				[]*decodedCICDDeploymentEvent{deploymentEventEvidenceWithState("f-1", "sha-1", raw, "success", "9001", "77001")},
			)
			if ok {
				t.Fatalf("ok = true (env=%q factID=%q), want false: a blank environment is not deployment truth", env, factID)
			}
			if env != "" {
				t.Fatalf("env = %q, want empty", env)
			}
		})
	}
}

// The declared fallback must still apply when the only deployment event has a
// blank environment, rather than the run losing its environment entirely.
func TestClassifyRunEvidenceFallsBackWhenDeploymentEnvironmentBlank(t *testing.T) {
	t.Parallel()

	const sha = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

	decisions, _, _ := buildCICDRunCorrelationDecisionsWithQuarantine([]facts.Envelope{
		{
			FactID:   "run",
			FactKind: facts.CICDRunFactKind,
			Payload: map[string]any{
				"provider": "github_actions", "run_id": "5150", "run_attempt": "1",
				"commit_sha": sha, "repository_id": "repository:r_x",
			},
		},
		{
			FactID:   "declared-env",
			FactKind: facts.CICDEnvironmentObservationFactKind,
			Payload: map[string]any{
				"provider": "github_actions", "run_id": "5150", "run_attempt": "1",
				"environment": "staging",
			},
		},
		{
			FactID:   "blank-deployment",
			FactKind: facts.CICDDeploymentEventFactKind,
			Payload: map[string]any{
				"provider": "github_actions", "deployment_id": "9001",
				"environment": "", "sha": sha,
			},
		},
	})

	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}
	if got := decisions[0].Environment; got != "stage" {
		t.Fatalf("Environment = %q, want the declared staging canonicalized to stage", got)
	}
	if got := decisions[0].EnvironmentEvidence; got != "declared" {
		t.Fatalf("EnvironmentEvidence = %q, want declared: a blank deploy event must not claim deployment truth", got)
	}
}
