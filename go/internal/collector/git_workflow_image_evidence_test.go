// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestBuildStreamingGenerationEmitsWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	collected := buildStreamingGeneration(
		"/tmp/repo",
		repositoryidentity.Metadata{ID: "repo://example/api", Name: "api"},
		"source-run",
		time.Date(2026, time.June, 7, 12, 0, 0, 0, time.UTC),
		RepositorySnapshot{
			ContentFiles: []ContentFileSnapshot{{
				RelativePath: ".github/workflows/deploy.yml",
				CommitSHA:    "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c",
				Body: `name: deploy
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Build image
        run: docker build -t registry.example.com/team/api:prod .
      - name: Push image
        run: docker push registry.example.com/team/api:prod
`,
				ArtifactType: "github_actions_workflow",
				Language:     "yaml",
			}},
		},
		false,
		"",
	)
	envelopes := drainCollectorFacts(t, collected)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	var workflowImageFacts []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.CICDWorkflowImageEvidenceFactKind {
			workflowImageFacts = append(workflowImageFacts, envelope)
		}
	}
	if len(workflowImageFacts) != 2 {
		t.Fatalf("workflow image fact count = %d, want 2", len(workflowImageFacts))
	}
	got := workflowImageFacts[0]
	if got.CollectorKind != "git" {
		t.Fatalf("CollectorKind = %q, want git", got.CollectorKind)
	}
	if got.SourceConfidence != facts.SourceConfidenceObserved {
		t.Fatalf("SourceConfidence = %q, want observed", got.SourceConfidence)
	}
	if got.SchemaVersion != facts.CICDSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", got.SchemaVersion, facts.CICDSchemaVersion)
	}
	if got.Payload["repository_id"] != "repo://example/api" {
		t.Fatalf("repository_id = %#v, want repo://example/api", got.Payload["repository_id"])
	}
	if got.Payload["workflow_path"] != ".github/workflows/deploy.yml" {
		t.Fatalf("workflow_path = %#v, want workflow path", got.Payload["workflow_path"])
	}
	if got.Payload["image_ref"] != "registry.example.com/team/api:prod" {
		t.Fatalf("image_ref = %#v, want registry.example.com/team/api:prod", got.Payload["image_ref"])
	}
	if got.Payload["evidence_class"] != "workflow_image_ref" {
		t.Fatalf("evidence_class = %#v, want workflow_image_ref", got.Payload["evidence_class"])
	}
	if got.Payload["commit_sha"] != "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c" {
		t.Fatalf("commit_sha = %#v, want the snapshot commit stamped for commit-scoped correlation (#5424)", got.Payload["commit_sha"])
	}
	if _, ok := got.Payload["command"]; ok {
		t.Fatalf("payload contains raw command: %#v", got.Payload)
	}
}

func drainCollectorFacts(t *testing.T, collected CollectedGeneration) []facts.Envelope {
	t.Helper()
	envelopes := make([]facts.Envelope, 0, collected.FactCount())
	for envelope := range collected.Facts {
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}
