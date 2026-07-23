// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildCICDRunCorrelationDecisionsTrimsIdentityFieldsLikePayloadString is
// the regression test for the codex #4724 P2 accuracy finding: the
// pre-migration reducer read every ci.* payload field through payloadString,
// which did strings.TrimSpace(fmt.Sprint(value)) on every read
// (go/internal/reducer/package_correlation_writer.go:payloadString). The typed
// decode seam preserves the raw, untrimmed collector string, so without an
// explicit trim at the point of use the correlation key and the anchor
// emptiness checks drift for padded/whitespace inputs:
//
//   - a padded run_id (" run-1 ") joins under a padded key instead of the
//     clean "run-1", splitting a run's evidence across two buckets or missing
//     the join to its artifact/environment/trigger facts entirely, and
//   - a whitespace-only commit_sha ("   ") passes the `== ""` emptiness check
//     (a non-empty raw string) so the run is NOT marked unresolved, inventing a
//     correlation the pre-migration trimmed path skipped.
//
// This test builds ONE ci.run with a padded run_id and a whitespace-only
// commit_sha, plus a ci.artifact whose run_id is padded the SAME way (so the
// old trimmed key joins them), and asserts the decision is keyed by the
// trimmed identity and classified unresolved — byte-identical to the
// pre-migration payloadString-trimmed behavior. It FAILS on the raw-typed HEAD
// (the decision keys under the padded run_id and is classified derived/exact
// rather than unresolved) and passes once the identity fields are trimmed at
// the point of use.
func TestBuildCICDRunCorrelationDecisionsTrimsIdentityFieldsLikePayloadString(t *testing.T) {
	t.Parallel()

	paddedRun := facts.Envelope{
		FactID:           "ci.run:padded",
		FactKind:         facts.CICDRunFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":      " github_actions ",
			"run_id":        " run-1 ",
			"run_attempt":   " 1 ",
			"repository_id": " repo-api ",
			"commit_sha":    "   ", // whitespace-only: old path trimmed to "" -> unresolved
			"status":        "completed",
			"result":        "success",
		},
	}
	// An artifact whose run identity is padded the SAME way, so under the
	// old trimmed key it joins to the run above. If the run key is trimmed
	// but the artifact key is not (or vice-versa) they no longer join, which
	// this fixture also guards against.
	paddedArtifact := facts.Envelope{
		FactID:           "ci.artifact:padded",
		FactKind:         facts.CICDArtifactFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":        " github_actions ",
			"run_id":          " run-1 ",
			"run_attempt":     " 1 ",
			"artifact_type":   "container_image",
			"artifact_digest": "  " + testCICDDigest + "  ",
		},
	}

	decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{paddedRun, paddedArtifact})

	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1 (the padded run and artifact must join under one trimmed key, not split across padded/clean buckets): %#v", len(decisions), decisions)
	}
	got := decisions[0]

	// Identity fields on the decision must be the trimmed values, matching the
	// pre-migration payloadString behavior and keeping the persisted reducer
	// fact identity stable regardless of collector-side whitespace.
	if got.Provider != "github_actions" {
		t.Fatalf("Provider = %q, want %q (trimmed like the old payloadString path)", got.Provider, "github_actions")
	}
	if got.RunID != "run-1" {
		t.Fatalf("RunID = %q, want %q (trimmed like the old payloadString path)", got.RunID, "run-1")
	}
	if got.RunAttempt != "1" {
		t.Fatalf("RunAttempt = %q, want %q (trimmed like the old payloadString path)", got.RunAttempt, "1")
	}
	if got.RepositoryID != "repo-api" {
		t.Fatalf("RepositoryID = %q, want %q (trimmed like the old payloadString path)", got.RepositoryID, "repo-api")
	}

	// A whitespace-only commit_sha trimmed to "" in the old path, so the run
	// was UNRESOLVED (missing repository_id or commit_sha anchor). The raw
	// path leaves "   " non-empty and skips that branch — the exact drift.
	if got.CommitSHA != "" {
		t.Fatalf("CommitSHA = %q, want empty string (a whitespace-only commit_sha trimmed to empty in the pre-migration path)", got.CommitSHA)
	}
	if got.Outcome != CICDRunCorrelationUnresolved {
		t.Fatalf("Outcome = %q, want %q; a whitespace-only commit_sha must make the run unresolved exactly as the pre-migration trimmed path did", got.Outcome, CICDRunCorrelationUnresolved)
	}
}

// TestBuildCICDRunCorrelationDecisionsTrimsArtifactAndWorkflowImageEvidence is
// the companion regression for the digest/environment/evidence_class/image_ref
// fields the pre-migration path also read through the trimming payloadString.
// It proves a padded artifact_digest still joins to its container-image
// identity row (exact correlation), a padded environment surfaces trimmed on
// the decision, and a workflow image whose evidence_class carries surrounding
// whitespace still classifies as a workflow_image_ref match (the old trimmed
// `== "workflow_image_ref"` compare tolerated padding; the raw compare would
// silently drop it).
func TestBuildCICDRunCorrelationDecisionsTrimsArtifactAndWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	t.Run("padded artifact digest joins container image identity", func(t *testing.T) {
		t.Parallel()

		decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
			ciRunFact("run-artifact", "github_actions", "repo-api", "abc123"),
			{
				FactID:           "ci.artifact:padded-digest",
				FactKind:         facts.CICDArtifactFactKind,
				SourceConfidence: facts.SourceConfidenceReported,
				Payload: map[string]any{
					"provider":        "github_actions",
					"run_id":          "run-artifact",
					"run_attempt":     "1",
					"artifact_type":   "container_image",
					"artifact_digest": " " + testCICDDigest + " ",
				},
			},
			containerImageIdentityFact("image-identity", "repo-api", "registry.example.com/team/api@"+testCICDDigest, testCICDDigest),
		})

		got := cicdDecisionsByRun(decisions)["github_actions:run-artifact:1"]
		assertCICDDecision(t, got, CICDRunCorrelationExact, 1)
		if got.ArtifactDigest != testCICDDigest {
			t.Fatalf("ArtifactDigest = %q, want the trimmed digest %q (a padded artifact_digest trimmed in the old path before the identity join)", got.ArtifactDigest, testCICDDigest)
		}
	})

	t.Run("padded environment surfaces trimmed", func(t *testing.T) {
		t.Parallel()

		decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
			ciRunFact("run-env", "github_actions", "repo-api", "abc123"),
			{
				FactID:           "ci.env:padded",
				FactKind:         facts.CICDEnvironmentObservationFactKind,
				SourceConfidence: facts.SourceConfidenceReported,
				Payload: map[string]any{
					"provider":    "github_actions",
					"run_id":      "run-env",
					"run_attempt": "1",
					"environment": " prod ",
				},
			},
		})

		got := cicdDecisionsByRun(decisions)["github_actions:run-env:1"]
		if got.Environment != "prod" {
			t.Fatalf("Environment = %q, want %q (trimmed like the old payloadString path)", got.Environment, "prod")
		}
	})

	t.Run("alias environment surfaces canonical", func(t *testing.T) {
		t.Parallel()

		decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
			ciRunFact("run-env-alias", "github_actions", "repo-api", "abc123"),
			{
				FactID:           "ci.env:alias",
				FactKind:         facts.CICDEnvironmentObservationFactKind,
				SourceConfidence: facts.SourceConfidenceReported,
				Payload: map[string]any{
					"provider":    "github_actions",
					"run_id":      "run-env-alias",
					"run_attempt": "1",
					"environment": "production",
				},
			},
		})

		got := cicdDecisionsByRun(decisions)["github_actions:run-env-alias:1"]
		if got.Environment != "prod" {
			t.Fatalf("Environment = %q, want %q (environment-alias contract: CI observations canonicalize through environment.Canonical)", got.Environment, "prod")
		}
	})

	t.Run("padded workflow image evidence_class still matches", func(t *testing.T) {
		t.Parallel()

		decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
			ciRunFact("run-workflow", "github_actions", "repo-api", "abc123"),
			{
				FactID:           "ci.workflow_image:padded",
				FactKind:         facts.CICDWorkflowImageEvidenceFactKind,
				SourceConfidence: facts.SourceConfidenceObserved,
				Payload: map[string]any{
					"repository_id":  " repo-api ",
					"evidence_class": " workflow_image_ref ",
					"image_ref":      " registry.example.com/team/api:prod ",
				},
			},
			containerImageIdentityFact("image-identity", "repo-api", "registry.example.com/team/api:prod", testCICDDigest),
		})

		got := cicdDecisionsByRun(decisions)["github_actions:run-workflow:1"]
		// The workflow image carries no commit_sha, so it correlates through the
		// repository-wide fallback and lands as derived, not exact (#5424); the
		// padded evidence_class still trims and matches workflow_image_ref, which
		// is what this case guards.
		assertCICDDecision(t, got, CICDRunCorrelationDerived, 1)
		if got.CorrelationKind != "workflow_image" {
			t.Fatalf("CorrelationKind = %q, want workflow_image (a padded evidence_class must still match workflow_image_ref like the old trimmed compare)", got.CorrelationKind)
		}
		if got.ImageRef != "registry.example.com/team/api:prod" {
			t.Fatalf("ImageRef = %q, want the trimmed image ref (a padded image_ref trimmed in the old path)", got.ImageRef)
		}
	})
}
