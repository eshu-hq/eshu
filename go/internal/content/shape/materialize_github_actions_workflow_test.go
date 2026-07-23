// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func TestMaterializeGitHubActionsWorkflowCreatesFileEntity(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:github-actions"
		path   = ".github/workflows/ci.yml"
		body   = "name: ci\non: push\njobs:\n  build:\n    steps:\n      - uses: hashicorp/setup-terraform@v3\n"
	)

	input := Input{
		RepoID: repoID,
		Files: []File{{
			Path:         path,
			Body:         body,
			Language:     "yaml",
			ArtifactType: "ansible_playbook",
		}},
	}

	first, err := Materialize(input)
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if got, want := len(first.Entities), 1; got != want {
		t.Fatalf("len(Entities) = %d, want %d", got, want)
	}

	entity := first.Entities[0]
	if got, want := entity.EntityID, content.CanonicalEntityID(repoID, path, "File", "ci", 1); got != want {
		t.Fatalf("EntityID = %q, want %q", got, want)
	}
	if got, want := entity.EntityType, "File"; got != want {
		t.Fatalf("EntityType = %q, want %q", got, want)
	}
	if got, want := entity.EntityName, "ci"; got != want {
		t.Fatalf("EntityName = %q, want %q", got, want)
	}
	if got, want := entity.Path, path; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	if got, want := entity.StartLine, 1; got != want {
		t.Fatalf("StartLine = %d, want %d", got, want)
	}
	if got, want := entity.SourceCache, body; got != want {
		t.Fatalf("SourceCache = %q, want full workflow source %q", got, want)
	}
	if got, want := entity.ArtifactType, githubActionsWorkflowArtifactType; got != want {
		t.Fatalf("ArtifactType = %q, want canonical workflow type %q", got, want)
	}
	if got := first.Records[0].PurgeEntities; got {
		t.Fatal("PurgeEntities = true, want false because the fresh workflow entity drives normal path-scoped stale reaping")
	}

	second, err := Materialize(input)
	if err != nil {
		t.Fatalf("second Materialize() error = %v, want nil", err)
	}
	if got, want := second.Entities[0].EntityID, entity.EntityID; got != want {
		t.Fatalf("repeat EntityID = %q, want stable %q", got, want)
	}
}

func TestMaterializeGitHubActionsWorkflowPathGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		artifactType string
		wantEntity   bool
		wantPurge    bool
	}{
		{name: "direct yml", path: ".github/workflows/ci.yml", wantEntity: true},
		{name: "direct yaml", path: ".github/workflows/ci.yaml", artifactType: "ansible_playbook", wantEntity: true},
		{name: "nested", path: ".github/workflows/team/ci.yml", artifactType: githubActionsWorkflowArtifactType, wantPurge: true},
		{name: "substring", path: "examples/.github/workflows/ci.yml", artifactType: githubActionsWorkflowArtifactType, wantPurge: true},
		{name: "non yaml", path: ".github/workflows/ci.json", artifactType: githubActionsWorkflowArtifactType, wantPurge: true},
		{name: "ordinary yaml", path: "deploy/config.yaml"},
		// Regression for the isDirectGitHubActionsWorkflowPath /
		// isGitHubActionsArtifactPath path-gate drift: a bare extension with
		// no basename must be rejected here exactly as go/internal/query's
		// copy of this gate already rejected it (both now delegate to
		// ghactionsref.IsWorkflowPath).
		{name: "empty basename yml is rejected", path: ".github/workflows/.yml", artifactType: githubActionsWorkflowArtifactType, wantPurge: true},
		{name: "empty basename yaml is rejected", path: ".github/workflows/.yaml", artifactType: githubActionsWorkflowArtifactType, wantPurge: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Materialize(Input{RepoID: "repository:path-gate", Files: []File{{
				Path:         test.path,
				Body:         "name: test\n",
				ArtifactType: test.artifactType,
			}}})
			if err != nil {
				t.Fatalf("Materialize() error = %v, want nil", err)
			}
			if gotEntity := len(got.Entities) == 1; gotEntity != test.wantEntity {
				t.Fatalf("workflow entity present = %t, want %t; entities = %#v", gotEntity, test.wantEntity, got.Entities)
			}
			if gotPurge := got.Records[0].PurgeEntities; gotPurge != test.wantPurge {
				t.Fatalf("PurgeEntities = %t, want %t", gotPurge, test.wantPurge)
			}
		})
	}
}

func TestMaterializeGitHubActionsWorkflowRenameUsesTombstoneAndFreshIdentity(t *testing.T) {
	t.Parallel()

	const repoID = "repository:renamed-workflow"
	got, err := Materialize(Input{RepoID: repoID, Files: []File{
		{Path: ".github/workflows/old.yml", Deleted: true},
		{Path: ".github/workflows/new.yml", Body: "name: new\n"},
	}})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if !got.Records[0].Deleted {
		t.Fatal("old workflow record is not tombstoned")
	}
	if got, want := len(got.Entities), 1; got != want {
		t.Fatalf("len(Entities) = %d, want %d", got, want)
	}
	if got, want := got.Entities[0].EntityID, content.CanonicalEntityID(repoID, ".github/workflows/new.yml", "File", "new", 1); got != want {
		t.Fatalf("renamed EntityID = %q, want %q", got, want)
	}
}

func TestMaterializeOrdinaryYAMLDoesNotCreateWorkflowFileEntity(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID: "repository:ordinary-yaml",
		Files: []File{{
			Path:         "deploy/config.yaml",
			Body:         "name: ordinary\n",
			Language:     "yaml",
			ArtifactType: "",
		}},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if got, want := len(got.Entities), 0; got != want {
		t.Fatalf("len(Entities) = %d, want %d for ordinary YAML", got, want)
	}
}

func TestMaterializeDeletedGitHubActionsWorkflowRetractsFileEntity(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID: "repository:deleted-workflow",
		Files: []File{{
			Path:         ".github/workflows/ci.yml",
			ArtifactType: "github_actions_workflow",
			Deleted:      true,
		}},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if got, want := len(got.Entities), 0; got != want {
		t.Fatalf("len(Entities) = %d, want %d for deleted workflow", got, want)
	}
	if got := got.Records[0].Deleted; !got {
		t.Fatal("Record.Deleted = false, want true so the writer retracts the stale workflow entity")
	}
}
