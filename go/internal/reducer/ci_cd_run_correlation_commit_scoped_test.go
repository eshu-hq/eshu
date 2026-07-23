// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildCICDRunCorrelationDecisionsPrefersCommitMatchedWorkflowImage is the
// #5424 regression: a static workflow-image fact carries the commit it was
// extracted at, so two runs of the same repository on different branches (each
// declaring a different image) must each correlate to their own branch's image
// rather than fanning the first workflow file out to every run sharing the
// repository_id.
func TestBuildCICDRunCorrelationDecisionsPrefersCommitMatchedWorkflowImage(t *testing.T) {
	t.Parallel()

	const (
		imageA  = "registry.example.com/team/api:branch-a"
		imageB  = "registry.example.com/team/api:branch-b"
		digestB = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	)
	decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
		ciRunFact("run-a", "github_actions", "repo-api", "commit-a"),
		ciRunFact("run-b", "github_actions", "repo-api", "commit-b"),
		commitScopedWorkflowImageFact("wf-a", "repo-api", "commit-a", imageA),
		commitScopedWorkflowImageFact("wf-b", "repo-api", "commit-b", imageB),
		containerImageIdentityFact("id-a", "repo-api", imageA, testCICDDigest),
		containerImageIdentityFact("id-b", "repo-api", imageB, digestB),
	})

	byRun := cicdDecisionsByRun(decisions)
	if got := byRun["github_actions:run-a:1"].ImageRef; got != imageA {
		t.Fatalf("run-a ImageRef = %q, want the commit-a branch image %q", got, imageA)
	}
	if got := byRun["github_actions:run-b:1"].ImageRef; got != imageB {
		t.Fatalf("run-b ImageRef = %q, want the commit-b branch image %q", got, imageB)
	}
}

// TestBuildCICDRunCorrelationDecisionsLabelsRepositoryWideFallbackDerived proves
// the lower-confidence fallback: when no workflow file was extracted at the run's
// commit, the run still correlates to a repository-declared image but as derived
// (not exact), so a commit-blind match is never reported with commit-scoped
// confidence (#5424).
func TestBuildCICDRunCorrelationDecisionsLabelsRepositoryWideFallbackDerived(t *testing.T) {
	t.Parallel()

	const image = "registry.example.com/team/api:prod"
	decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
		ciRunFact("run-x", "github_actions", "repo-api", "commit-x"),
		// The only workflow file was extracted at a different commit, so the run
		// falls back to the repository-wide join.
		commitScopedWorkflowImageFact("wf-other", "repo-api", "commit-other", image),
		containerImageIdentityFact("id", "repo-api", image, testCICDDigest),
	})

	got := cicdDecisionsByRun(decisions)["github_actions:run-x:1"]
	if got.Outcome != CICDRunCorrelationDerived {
		t.Fatalf("Outcome = %q, want derived for a repository-wide fallback correlation", got.Outcome)
	}
	if got.ImageRef != image {
		t.Fatalf("ImageRef = %q, want %q", got.ImageRef, image)
	}
	if got.CorrelationKind != "workflow_image" {
		t.Fatalf("CorrelationKind = %q, want workflow_image", got.CorrelationKind)
	}
}

func commitScopedWorkflowImageFact(factID, repositoryID, commitSHA, imageRef string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "repo:team-api",
		GenerationID:     "generation-git",
		FactKind:         facts.CICDWorkflowImageEvidenceFactKind,
		SchemaVersion:    facts.CICDSchemaVersion,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef:        facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"repository_id":   repositoryID,
			"commit_sha":      commitSHA,
			"workflow_path":   ".github/workflows/deploy.yml",
			"command_kind":    "docker_build",
			"image_ref":       imageRef,
			"evidence_class":  "workflow_image_ref",
			"source_category": "static_workflow",
		},
	}
}
