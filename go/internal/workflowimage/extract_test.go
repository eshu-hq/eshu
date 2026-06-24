// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflowimage

import "testing"

func TestExtractGitHubActionsClassifiesDockerBuildPushAndUnresolved(t *testing.T) {
	t.Parallel()

	rows := ExtractGitHubActions(".github/workflows/deploy.yml", `name: deploy
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Build
        run: docker build -t registry.example.com/team/api:prod .
      - name: Push
        run: docker push registry.example.com/team/api:prod
      - name: Templated
        run: docker build -t ${{ env.REGISTRY }}/team/api:${{ github.sha }} .
`)

	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3: %#v", len(rows), rows)
	}
	if rows[0].ImageRef != "registry.example.com/team/api:prod" ||
		rows[0].EvidenceClass != EvidenceClassImageRef ||
		rows[0].CommandKind != "docker_build" {
		t.Fatalf("rows[0] = %#v, want exact docker build", rows[0])
	}
	if rows[1].ImageRef != "registry.example.com/team/api:prod" ||
		rows[1].EvidenceClass != EvidenceClassImageRef ||
		rows[1].CommandKind != "docker_push" {
		t.Fatalf("rows[1] = %#v, want exact docker push", rows[1])
	}
	if rows[2].EvidenceClass != EvidenceClassUnresolved {
		t.Fatalf("rows[2] = %#v, want unresolved", rows[2])
	}
}

func TestExtractCommandClassifiesMultipleTagsAsAmbiguous(t *testing.T) {
	t.Parallel()

	rows := ExtractCommand(
		".github/workflows/deploy.yml",
		"build",
		"Build",
		"docker build -t registry.example.com/team/api:prod -t registry.example.com/team/worker:prod .",
	)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].EvidenceClass != EvidenceClassAmbiguous {
		t.Fatalf("EvidenceClass = %q, want %q", rows[0].EvidenceClass, EvidenceClassAmbiguous)
	}
	if len(rows[0].ImageRefs) != 2 {
		t.Fatalf("ImageRefs = %#v, want two refs", rows[0].ImageRefs)
	}
}

func TestExtractGitHubActionsClassifiesReusableWorkflowImageInput(t *testing.T) {
	t.Parallel()

	rows := ExtractGitHubActions(".github/workflows/deploy.yml", `name: deploy
jobs:
  deploy:
    uses: org/platform/.github/workflows/deploy-image.yml@v1
    with:
      image_ref: registry.example.com/team/api:prod
`)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	got := rows[0]
	if got.EvidenceClass != EvidenceClassImageRef {
		t.Fatalf("EvidenceClass = %q, want %q", got.EvidenceClass, EvidenceClassImageRef)
	}
	if got.CommandKind != "reusable_workflow_input" {
		t.Fatalf("CommandKind = %q, want reusable_workflow_input", got.CommandKind)
	}
	if got.JobName != "deploy" {
		t.Fatalf("JobName = %q, want deploy", got.JobName)
	}
	if got.ImageRef != "registry.example.com/team/api:prod" {
		t.Fatalf("ImageRef = %q, want registry.example.com/team/api:prod", got.ImageRef)
	}
}
