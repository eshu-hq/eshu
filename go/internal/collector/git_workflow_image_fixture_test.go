// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestContainerCILineageFixtureEmitsWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	repoPath, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "container-ci-lineage"))
	if err != nil {
		t.Fatalf("resolve container-ci-lineage fixture path: %v", err)
	}
	const workflowPath = ".github/workflows/build-image.yml"
	collected := buildStreamingGeneration(
		repoPath,
		repositoryidentity.Metadata{ID: "repository:r_19519f37", Name: "container-ci-lineage"},
		"golden-container-ci-lineage",
		time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount: 2,
			ContentFileMetas: []ContentFileMeta{{
				RelativePath: workflowPath,
				Digest:       "sha256:container-ci-lineage-workflow",
				Language:     "yaml",
				ArtifactType: "github_actions_workflow",
			}},
		},
		false,
		"",
	)

	var workflowFacts []facts.Envelope
	for _, envelope := range drainCollectorFacts(t, collected) {
		if envelope.FactKind == facts.CICDWorkflowImageEvidenceFactKind {
			workflowFacts = append(workflowFacts, envelope)
		}
	}
	if got, want := len(workflowFacts), 1; got != want {
		t.Fatalf("workflow image fact count = %d, want %d", got, want)
	}

	got := workflowFacts[0]
	for key, want := range map[string]any{
		"repository_id":   "repository:r_19519f37",
		"workflow_path":   workflowPath,
		"command_kind":    "docker_buildx",
		"evidence_class":  "workflow_image_ref",
		"source_category": "static_workflow",
		"image_ref":       "ghcr.io/acme/container-ci-lineage:1.0.0",
	} {
		if value := got.Payload[key]; value != want {
			t.Errorf("payload[%q] = %#v, want %#v", key, value, want)
		}
	}
	if got.CollectorKind != "git" {
		t.Errorf("CollectorKind = %q, want git", got.CollectorKind)
	}
	if _, ok := got.Payload["commit_sha"]; ok {
		t.Fatalf("payload contains commit_sha for the plain staged fixture: %#v", got.Payload)
	}
}
