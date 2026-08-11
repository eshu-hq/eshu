// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/lib/pq"
)

func TestContentWriterReapsStaleWorkflowEntityAgainstFreshIdentity(t *testing.T) {
	t.Parallel()

	const (
		repoID = "repository:github-actions-reap"
		path   = ".github/workflows/ci.yml"
	)
	materialization := mustMaterializeWorkflow(t, repoID, shape.File{
		Path:         path,
		Body:         "name: ci\njobs: {}\n",
		ArtifactType: "ansible_playbook",
	})
	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	if _, err := writer.Write(context.Background(), materialization); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	_, args := findReapExec(t, db)
	freshIDs, ok := args[2].(pq.StringArray)
	if !ok {
		t.Fatalf("fresh entity ids type = %T, want pq.StringArray", args[2])
	}
	wantID := content.CanonicalEntityID(repoID, path, "File", "ci", 1)
	mustContain(t, freshIDs, wantID)
	mustNotContain(t, freshIDs, content.CanonicalEntityID(repoID, path, "File", "old-ci", 1))
}

func TestContentWriterPurgesLegacyWorkflowEntityWhenPathBecomesIneligible(t *testing.T) {
	t.Parallel()

	const path = ".github/workflows/team/ci.yml"
	materialization := mustMaterializeWorkflow(t, "repository:github-actions-purge", shape.File{
		Path:         path,
		Body:         "name: nested\n",
		ArtifactType: "github_actions_workflow",
	})
	if !materialization.Records[0].PurgeEntities {
		t.Fatal("PurgeEntities = false, want true for legacy artifact classification at an ineligible path")
	}
	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	if _, err := writer.Write(context.Background(), materialization); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	assertContentEntityPathDelete(t, db, path)
}

func TestContentWriterWorkflowRenameTombstonesOldPathAndKeepsFreshPath(t *testing.T) {
	t.Parallel()

	const (
		repoID  = "repository:github-actions-rename"
		oldPath = ".github/workflows/old.yml"
		newPath = ".github/workflows/new.yml"
	)
	materialization := mustMaterializeWorkflow(t, repoID,
		shape.File{Path: oldPath, Deleted: true},
		shape.File{Path: newPath, Body: "name: new\n"},
	)
	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	if _, err := writer.Write(context.Background(), materialization); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	assertContentEntityPathDelete(t, db, oldPath)
	_, args := findReapExec(t, db)
	paths, ok := args[1].(pq.StringArray)
	if !ok {
		t.Fatalf("reap paths type = %T, want pq.StringArray", args[1])
	}
	if len(paths) != 1 || paths[0] != newPath {
		t.Fatalf("reap paths = %v, want [%s]", []string(paths), newPath)
	}
}

func mustMaterializeWorkflow(t *testing.T, repoID string, files ...shape.File) content.Materialization {
	t.Helper()
	materialization, err := shape.Materialize(shape.Input{RepoID: repoID, Files: files})
	if err != nil {
		t.Fatalf("shape.Materialize() error = %v, want nil", err)
	}
	materialization.ScopeID = "test-scope"
	materialization.GenerationID = "test-generation"
	return materialization
}

func assertContentEntityPathDelete(t *testing.T, db *fakeExecQueryer, wantPath string) {
	t.Helper()
	for _, exec := range db.execs {
		if !strings.Contains(exec.query, "DELETE FROM content_entities") || strings.Contains(exec.query, "entity_id <> ALL") {
			continue
		}
		for _, arg := range exec.args {
			if value, ok := arg.(string); ok && value == wantPath {
				return
			}
		}
	}
	t.Fatalf("no content_entities path delete for %q among %d execs", wantPath, len(db.execs))
}
