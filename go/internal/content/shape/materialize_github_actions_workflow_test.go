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
			ArtifactType: "github_actions_workflow",
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
