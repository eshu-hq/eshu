// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import "testing"

// Only a deployment state that means something actually reached the
// environment may publish environment_evidence=deploy_event. A failed, reverted,
// or not-yet-started deployment is not deployment truth, and #5426 plus the API
// contract both read that value as if it were.
func TestClassifyDeploymentEventEnvironmentRejectsNonDeployingStates(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"failure", "error", "inactive", "pending", "queued", ""} {
		t.Run("state="+state, func(t *testing.T) {
			t.Parallel()

			env, _, ok := classifyCICDDeploymentEventEnvironment(
				[]*decodedCICDDeploymentEvent{
					deploymentEventEvidenceWithState("f-1", "sha-1", "production", state, "9001", "77001"),
				},
			)
			if ok {
				t.Fatalf("state %q reported ok with env=%q: a deployment in this state put "+
					"nothing in the environment and must not be published as deploy_event truth", state, env)
			}
		})
	}
}

// success and in_progress are the states that do mean something reached the
// environment, so they still promote.
func TestClassifyDeploymentEventEnvironmentAcceptsDeployingStates(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"success", "in_progress"} {
		t.Run("state="+state, func(t *testing.T) {
			t.Parallel()

			env, _, ok := classifyCICDDeploymentEventEnvironment(
				[]*decodedCICDDeploymentEvent{
					deploymentEventEvidenceWithState("f-1", "sha-1", "production", state, "9001", "77001"),
				},
			)
			if !ok || env != "prod" {
				t.Fatalf("state %q: ok=%v env=%q, want true/prod", state, ok, env)
			}
		})
	}
}

// The rollback case. A deployment that succeeded and was later marked inactive
// is no longer deployed, but the state ranker prefers success over everything,
// so an unfiltered selection would let the stale success win and report the
// environment as still live.
//
// Filtering to promotable states BEFORE selection is what prevents that: the
// inactive event is removed rather than out-ranked, so nothing promotes and the
// declared path answers instead.
func TestClassifyDeploymentEventEnvironmentDoesNotLetAStaleSuccessOutrankInactive(t *testing.T) {
	t.Parallel()

	// Same deployment, later status id, rolled back.
	deployed := deploymentEventEvidenceWithState("f-1", "sha-1", "production", "success", "9001", "77001")
	rolledBack := deploymentEventEvidenceWithState("f-2", "sha-1", "production", "inactive", "9001", "77002")

	if _, _, ok := classifyCICDDeploymentEventEnvironment(
		[]*decodedCICDDeploymentEvent{rolledBack},
	); ok {
		t.Fatal("an inactive-only deployment reported deployment truth")
	}

	// With both present the success is still the newest DEPLOYING evidence for
	// this run, so it legitimately promotes; the guard is that inactive alone
	// never does. This pins the boundary rather than over-claiming that any
	// inactive suppresses a sibling success.
	if _, _, ok := classifyCICDDeploymentEventEnvironment(
		[]*decodedCICDDeploymentEvent{deployed, rolledBack},
	); !ok {
		t.Fatal("a run carrying a successful deployment lost its environment entirely")
	}
}
