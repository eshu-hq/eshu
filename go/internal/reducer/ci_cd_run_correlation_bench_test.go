// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkCICDRunCorrelationCorpus builds a synthetic corpus of runCount
// ci.run facts, each with one artifact, one environment observation, one
// trigger edge, one step, and one workflow-image-evidence fact sharing the
// run's repository — a realistic per-run CI/CD provider shape for
// BenchmarkBuildCICDRunCorrelationDecisions.
func benchmarkCICDRunCorrelationCorpus(runCount int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, runCount*6)
	for i := 0; i < runCount; i++ {
		runID := fmt.Sprintf("run-%d", i)
		repositoryID := fmt.Sprintf("github.com/example-org/service-%d", i)
		commitSHA := fmt.Sprintf("%040x", i)
		envelopes = append(envelopes,
			facts.Envelope{
				FactID:   runID,
				FactKind: facts.CICDRunFactKind,
				Payload: map[string]any{
					"provider":      "github_actions",
					"run_id":        runID,
					"run_attempt":   "1",
					"repository_id": repositoryID,
					"commit_sha":    commitSHA,
					"status":        "completed",
					"result":        "success",
					"branch":        "main",
				},
			},
			facts.Envelope{
				FactID:   runID + "-artifact",
				FactKind: facts.CICDArtifactFactKind,
				Payload: map[string]any{
					"provider":        "github_actions",
					"run_id":          runID,
					"run_attempt":     "1",
					"artifact_id":     runID + "-artifact-id",
					"artifact_type":   "container_image",
					"artifact_digest": fmt.Sprintf("sha256:%064x", i),
				},
			},
			facts.Envelope{
				FactID:   runID + "-environment",
				FactKind: facts.CICDEnvironmentObservationFactKind,
				Payload: map[string]any{
					"provider":    "github_actions",
					"run_id":      runID,
					"run_attempt": "1",
					"environment": "prod",
				},
			},
			facts.Envelope{
				FactID:   runID + "-trigger",
				FactKind: facts.CICDTriggerEdgeFactKind,
				Payload: map[string]any{
					"provider":        "github_actions",
					"run_id":          runID,
					"run_attempt":     "1",
					"trigger_kind":    "workflow_call",
					"source_provider": "github_actions",
					"source_run_id":   fmt.Sprintf("run-%d-source", i),
				},
			},
			facts.Envelope{
				FactID:   runID + "-step",
				FactKind: facts.CICDStepFactKind,
				Payload: map[string]any{
					"provider":    "github_actions",
					"run_id":      runID,
					"run_attempt": "1",
					"step_number": "1",
					"step_name":   "Build",
					"status":      "completed",
					"result":      "success",
				},
			},
			facts.Envelope{
				FactID:   runID + "-workflow-image",
				FactKind: facts.CICDWorkflowImageEvidenceFactKind,
				Payload: map[string]any{
					"repository_id":  repositoryID,
					"workflow_path":  ".github/workflows/build.yml",
					"evidence_class": "workflow_image_ref",
					"image_ref":      fmt.Sprintf("registry.example.com/team/service-%d:prod", i),
				},
			},
		)
	}
	return envelopes
}

// BenchmarkBuildCICDRunCorrelationDecisions is the No-Regression Evidence
// benchmark for the ci_cd_run family's typed-decode migration (Contract
// System v1, Wave 4d): it measures the cost of classifying a realistic
// 5,000-run corpus (run + artifact + environment + trigger + step +
// workflow-image-evidence per run — 30,000 facts total) into correlation
// decisions, before versus after the ci.run/ci.artifact/
// ci.environment_observation/ci.trigger_edge/ci.step/
// ci.workflow_image_evidence decode sites moved from raw payloadString
// lookups to the sdk/go/factschema seam.
func BenchmarkBuildCICDRunCorrelationDecisions(b *testing.B) {
	const runCount = 5000
	envelopes := benchmarkCICDRunCorrelationCorpus(runCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decisions := BuildCICDRunCorrelationDecisions(envelopes)
		if len(decisions) != runCount {
			b.Fatalf("len(decisions) = %d, want %d", len(decisions), runCount)
		}
	}
}

// benchmarkCICDSharedRepoWorkflowImageCorpus builds runCount ci.run facts that
// all share ONE repository, plus workflowImageCount ci.workflow_image_evidence
// facts for that same repository. This is the shape the copilot #4724 review
// flagged: attachWorkflowImagesToRuns fans every workflow-image envelope out to
// every run in the repo, so classifyCICDWorkflowImageEvidence re-decodes the
// same evidence O(runs x workflow_images) times. A per-repo shape (not the
// unique-repo-per-run shape of benchmarkCICDRunCorrelationCorpus) is required
// to exercise — and to measure the fix for — that quadratic re-decode.
func benchmarkCICDSharedRepoWorkflowImageCorpus(runCount, workflowImageCount int) []facts.Envelope {
	const repositoryID = "github.com/example-org/shared-service"
	envelopes := make([]facts.Envelope, 0, runCount+workflowImageCount)
	for i := 0; i < runCount; i++ {
		runID := fmt.Sprintf("run-%d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactID:   runID,
			FactKind: facts.CICDRunFactKind,
			Payload: map[string]any{
				"provider":      "github_actions",
				"run_id":        runID,
				"run_attempt":   "1",
				"repository_id": repositoryID,
				"commit_sha":    fmt.Sprintf("%040x", i),
				"status":        "completed",
				"result":        "success",
			},
		})
	}
	for i := 0; i < workflowImageCount; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("workflow-image-%d", i),
			FactKind: facts.CICDWorkflowImageEvidenceFactKind,
			Payload: map[string]any{
				"repository_id":  repositoryID,
				"workflow_path":  fmt.Sprintf(".github/workflows/build-%d.yml", i),
				"evidence_class": "workflow_image_ref",
				"image_ref":      fmt.Sprintf("registry.example.com/team/shared-service:v%d", i),
			},
		})
	}
	return envelopes
}

// BenchmarkBuildCICDRunCorrelationDecisionsSharedRepoWorkflowImages measures
// the per-run workflow-image decode cost the copilot #4724 review flagged: a
// single repo with many runs and many shared workflow-image-evidence facts.
// Before the once-decode cache, classifyCICDWorkflowImageEvidence re-decoded
// each workflow-image envelope once per run (O(runs x workflow_images) typed
// decodes). After, each workflow-image envelope is decoded once during the
// build phase and both attachWorkflowImagesToRuns and
// classifyCICDWorkflowImageEvidence read the cached typed value.
func BenchmarkBuildCICDRunCorrelationDecisionsSharedRepoWorkflowImages(b *testing.B) {
	const (
		runCount           = 500
		workflowImageCount = 50
	)
	envelopes := benchmarkCICDSharedRepoWorkflowImageCorpus(runCount, workflowImageCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decisions := BuildCICDRunCorrelationDecisions(envelopes)
		if len(decisions) != runCount {
			b.Fatalf("len(decisions) = %d, want %d", len(decisions), runCount)
		}
	}
}
