// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
package git

// Empty-scope declaration predicate for eshu-hq/eshu#8657: the collector
// declares `empty_scope` on the repository fact only when the snapshot it
// just walked is genuinely fileless. The gate is strict on purpose: any
// discovered file (FileCount), any parsed or carried file content, any
// deleted path, or a delta generation withholds the declaration. A scope
// whose files were discovered but dropped before emission must NEVER
// declare: downstream the build would carry zero file facts either way,
// and only the absence of the declaration keeps the writer-side retract
// check loud for that shape.

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
)

func TestSnapshotDeclaresEmptyScope(t *testing.T) {
	t.Parallel()

	withFiles := RepositorySnapshot{FileCount: 2}

	withCarrier := RepositorySnapshot{
		ContentFileMetas: []model.ContentFileMeta{{RelativePath: "src/a.go"}},
	}

	withDeleted := RepositorySnapshot{DeletedRelativePaths: []string{"gone.go"}}

	withDelta := RepositorySnapshot{Delta: true}

	withReconcile := RepositorySnapshot{Reconcile: true}

	withEntities := RepositorySnapshot{
		ContentEntities: []model.ContentEntitySnapshot{{}},
	}

	withDocsMetas := RepositorySnapshot{
		DocumentationFileMetas: []model.ContentFileMeta{{RelativePath: "docs/a.md"}},
	}

	withWorkflowMetas := RepositorySnapshot{
		WorkflowImageFileMetas: []model.ContentFileMeta{{RelativePath: ".github/workflows/a.yml"}},
	}

	withDeltaPaths := RepositorySnapshot{
		DeltaRelativePaths: []string{"src/a.go"},
	}

	cases := []struct {
		name     string
		snapshot RepositorySnapshot
		want     bool
	}{
		{"zero snapshot declares", RepositorySnapshot{}, true},
		{"discovered files withhold", withFiles, false},
		{"carried content withholds", withCarrier, false},
		{"deleted paths withhold", withDeleted, false},
		{"delta withholds", withDelta, false},
		{"reconcile of empty still declares", withReconcile, true},
		{"content entities withhold", withEntities, false},
		{"documentation metas withhold", withDocsMetas, false},
		{"workflow image metas withhold", withWorkflowMetas, false},
		{"delta paths withhold", withDeltaPaths, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := snapshotDeclaresEmptyScope(&tc.snapshot); got != tc.want {
				t.Fatalf("snapshotDeclaresEmptyScope() = %v, want %v", got, tc.want)
			}
		})
	}
}

// An empty walked snapshot declares `empty_scope` on the streamed
// repository fact through the production streamFacts path; a snapshot
// carrying files does not.
func TestBuildStreamingGenerationDeclaresEmptyScopeOnlyWhenFileless(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	empty := buildStreamingGeneration(repoPath, repo, "run-empty", observedAt, RepositorySnapshot{}, false, "")
	var emptyRepoPayload map[string]any
	for _, envelope := range drainFactChannel(empty.Facts) {
		if envelope.FactKind == "repository" {
			emptyRepoPayload = envelope.Payload
		}
	}
	if emptyRepoPayload == nil {
		t.Fatal("missing repository fact for the empty snapshot")
	}
	if got, want := emptyRepoPayload["empty_scope"], true; got != want {
		t.Fatalf("empty_scope = %#v, want %#v; a walked-empty scope must declare", got, want)
	}

	withFiles := RepositorySnapshot{FileCount: 1}
	full := buildStreamingGeneration(repoPath, repo, "run-full", observedAt, withFiles, false, "")
	for _, envelope := range drainFactChannel(full.Facts) {
		if envelope.FactKind == "repository" {
			if _, ok := envelope.Payload["empty_scope"]; ok {
				t.Fatalf("empty_scope declared for a snapshot with FileCount 1; discovered files must withhold")
			}
		}
	}
}
