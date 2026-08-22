// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gitrepo/gitmodel"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestContainerCILineageFixtureEmitsWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	repoPath, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", "container-ci-lineage"))
	if err != nil {
		t.Fatalf("resolve container-ci-lineage fixture path: %v", err)
	}
	const (
		workflowPath  = ".github/workflows/build-image.yml"
		fixtureCommit = "fe05491e32178ca002832c23a04f9c061046ea94"
	)
	collected := buildStreamingGeneration(
		repoPath,
		repositoryidentity.Metadata{ID: "repository:r_19519f37", Name: "container-ci-lineage"},
		"golden-container-ci-lineage",
		time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount:     2,
			HeadCommitSHA: fixtureCommit,
			ContentFileMetas: []gitmodel.ContentFileMeta{{
				RelativePath: workflowPath,
				Digest:       "sha256:container-ci-lineage-workflow",
				Language:     "yaml",
				ArtifactType: "github_actions_workflow",
				CommitSHA:    fixtureCommit,
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
	if got, want := len(workflowFacts), 4; got != want {
		t.Fatalf("workflow image fact count = %d, want %d", got, want)
	}

	byOutcomeClass := make(map[string]facts.Envelope, len(workflowFacts))
	for _, envelope := range workflowFacts {
		key := strings.Join([]string{
			strings.TrimSpace(fmt.Sprint(envelope.Payload["evidence_class"])),
			strings.TrimSpace(fmt.Sprint(envelope.Payload["command_kind"])),
		}, "/")
		byOutcomeClass[key] = envelope
	}
	for _, key := range []string{
		"workflow_image_ref/docker_buildx",
		"workflow_image_ref/reusable_workflow_input",
		"workflow_image_unresolved/docker_build",
		"workflow_image_ambiguous/docker_build",
	} {
		if _, ok := byOutcomeClass[key]; !ok {
			t.Errorf("workflow image facts missing outcome class %q: %#v", key, byOutcomeClass)
		}
	}

	got := byOutcomeClass["workflow_image_ref/docker_buildx"]
	for key, want := range map[string]any{
		"repository_id":   "repository:r_19519f37",
		"workflow_path":   workflowPath,
		"command_kind":    "docker_buildx",
		"evidence_class":  "workflow_image_ref",
		"source_category": "static_workflow",
		"image_ref":       "ghcr.io/acme/container-ci-lineage:1.0.0",
		"commit_sha":      fixtureCommit,
	} {
		if value := got.Payload[key]; value != want {
			t.Errorf("payload[%q] = %#v, want %#v", key, value, want)
		}
	}
	if got.CollectorKind != "git" {
		t.Errorf("CollectorKind = %q, want git", got.CollectorKind)
	}
}

func TestGitHubActionsFixtureEmitsInputOnlyWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	repoPath, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", "github_actions_workflows"))
	if err != nil {
		t.Fatalf("resolve github_actions_workflows fixture path: %v", err)
	}
	const (
		workflowPath  = ".github/workflows/ci.yml"
		fixtureCommit = "882382ef307a373f12e666b4afee3fcd63aa5ee0"
	)
	collected := buildStreamingGeneration(
		repoPath,
		repositoryidentity.Metadata{ID: "repository:r_f252e384", Name: "github_actions_workflows"},
		"golden-github-actions-workflows",
		time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount:     3,
			HeadCommitSHA: fixtureCommit,
			ContentFileMetas: []gitmodel.ContentFileMeta{{
				RelativePath: workflowPath,
				Digest:       "sha256:github-actions-workflows-ci",
				Language:     "yaml",
				ArtifactType: "github_actions_workflow",
				CommitSHA:    fixtureCommit,
			}},
		},
		false,
		"",
	)

	var matched []facts.Envelope
	for _, envelope := range drainCollectorFacts(t, collected) {
		if envelope.FactKind != facts.CICDWorkflowImageEvidenceFactKind {
			continue
		}
		if envelope.Payload["command_kind"] == "reusable_workflow_input" {
			matched = append(matched, envelope)
		}
	}
	if got, want := len(matched), 1; got != want {
		t.Fatalf("reusable workflow image fact count = %d, want %d", got, want)
	}
	for key, want := range map[string]any{
		"repository_id":  "repository:r_f252e384",
		"workflow_path":  workflowPath,
		"command_kind":   "reusable_workflow_input",
		"evidence_class": "workflow_image_ref",
		"image_ref":      "ghcr.io/acme/container-ci-lineage:1.0.0",
		"commit_sha":     fixtureCommit,
	} {
		if value := matched[0].Payload[key]; value != want {
			t.Errorf("payload[%q] = %#v, want %#v", key, value, want)
		}
	}
}
