// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestBuildProjectionQueuesContainerImageIdentityForCICDContainerArtifact is the
// #5423 reachability guard: a container-image ci.artifact must trigger a
// container_image_identity intent for the CI scope, so the reducer co-loads the
// scope-local ci.run/ci.artifact with the cross-scope active OCI manifest and
// the run's commit can reach the identity decision. Without this trigger the
// commit-revision threading is dead code in production (CI facts and OCI
// manifests live in different scopes).
func TestBuildProjectionQueuesContainerImageIdentityForCICDContainerArtifact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "ci_cd_run:github_actions:eshu-hq:supply-chain-demo",
		ScopeKind:    "ci_cd_run_repository",
		SourceSystem: "ci_cd_run",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "cicd-generation-1",
		ObservedAt:   time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.June, 25, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		cicdContainerArtifactEnvelope("fact-ci-artifact-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireContainerImageIdentityIntent(t, projection.reducerIntents)
	if got, want := intent.FactID, "fact-ci-artifact-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the ci.artifact fact", got)
	}
	if got, want := intent.SourceSystem, "ci_cd_run"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionDoesNotQueueContainerImageIdentityForNonContainerCICDArtifact
// pins the trigger predicate: a non-image artifact (coverage report, SBOM
// bundle) carries no image reference and must not spawn an identity intent.
func TestBuildProjectionDoesNotQueueContainerImageIdentityForNonContainerCICDArtifact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "ci_cd_run:github_actions:eshu-hq:supply-chain-demo",
		ScopeKind:    "ci_cd_run_repository",
		SourceSystem: "ci_cd_run",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "cicd-generation-2",
		ObservedAt:   time.Date(2026, time.June, 25, 11, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.June, 25, 11, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	artifact := cicdContainerArtifactEnvelope("fact-ci-artifact-coverage", scopeValue.ScopeID, generation.GenerationID)
	artifact.Payload["artifact_type"] = "coverage_report"

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{artifact})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainContainerImageIdentity {
			t.Fatalf("unexpected container_image_identity intent from a non-container ci.artifact")
		}
	}
}

func cicdContainerArtifactEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.CICDArtifactFactKind,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "ci_cd_run",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "ci_cd_run",
		},
		Payload: map[string]any{
			"provider":        "github_actions",
			"run_id":          "42",
			"run_attempt":     "1",
			"artifact_type":   "container_image",
			"artifact_digest": "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
		},
	}
}
