// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fullSnapshotFixture is a repository-scoped generation with the repository
// fact and one file fact, the shape the git collector emits for a full
// snapshot. Each case below changes one thing about it.
func fullSnapshotFixture() (scope.IngestionScope, scope.ScopeGeneration, []facts.Envelope) {
	const repoID = "repo-full-snapshot"
	scopeValue, generation := makeTestScope("scope-full-snapshot", repoID, "org/full-snapshot")
	inputFacts := []facts.Envelope{
		{
			FactID:       "fact-repo",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			FactKind:     "repository",
			ObservedAt:   generation.ObservedAt,
			Payload: map[string]any{
				"repo_id": repoID,
				"name":    "full-snapshot",
				"path":    "org/full-snapshot",
			},
		},
		{
			FactID:       "fact-file",
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			FactKind:     "file",
			ObservedAt:   generation.ObservedAt,
			Payload: map[string]any{
				"repo_id":          repoID,
				"path":             "org/full-snapshot/main.go",
				"relative_path":    "main.go",
				"name":             "main.go",
				"language":         "go",
				"parsed_file_data": map[string]any{},
			},
		},
	}
	return scopeValue, generation, inputFacts
}

// TestBuildProjectionMarksContentFullSnapshot pins #7447 item 5's producer
// side. The content writer removes every path a materialization does not carry
// when FullSnapshot is set, so the flag must be true only when Records really
// are the complete file set of the repository: a repository-scoped, non-delta
// generation that carries the repository fact, using the same delta markers
// the canonical graph builder reads. Every other shape must leave it false, so
// a delta or a scope that is not a repository snapshot can never reap.
func TestBuildProjectionMarksContentFullSnapshot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*scope.IngestionScope, *scope.ScopeGeneration, *[]facts.Envelope)
		want   bool
	}{
		{
			name:   "full repository generation",
			mutate: func(*scope.IngestionScope, *scope.ScopeGeneration, *[]facts.Envelope) {},
			want:   true,
		},
		{
			name: "reconciliation full",
			mutate: func(_ *scope.IngestionScope, _ *scope.ScopeGeneration, inputFacts *[]facts.Envelope) {
				(*inputFacts)[0].Payload["reconciliation_generation"] = true
			},
			want: true,
		},
		{
			name: "delta generation flag on the generation",
			mutate: func(_ *scope.IngestionScope, generation *scope.ScopeGeneration, _ *[]facts.Envelope) {
				generation.IsDelta = true
				generation.DeltaBaselineCommitSHA = "abc123"
			},
			want: false,
		},
		{
			name: "delta_generation on the repository fact",
			mutate: func(_ *scope.IngestionScope, _ *scope.ScopeGeneration, inputFacts *[]facts.Envelope) {
				(*inputFacts)[0].Payload["delta_generation"] = true
				(*inputFacts)[0].Payload["delta_relative_paths"] = []any{"main.go"}
			},
			want: false,
		},
		{
			name: "delta_generation true with no listed paths is still not a full snapshot",
			mutate: func(_ *scope.IngestionScope, _ *scope.ScopeGeneration, inputFacts *[]facts.Envelope) {
				(*inputFacts)[0].Payload["delta_generation"] = true
			},
			want: false,
		},
		{
			name: "no repository fact in the generation",
			mutate: func(_ *scope.IngestionScope, _ *scope.ScopeGeneration, inputFacts *[]facts.Envelope) {
				*inputFacts = (*inputFacts)[1:]
			},
			want: false,
		},
		{
			name: "no repo_id on the scope",
			mutate: func(scopeValue *scope.IngestionScope, _ *scope.ScopeGeneration, _ *[]facts.Envelope) {
				scopeValue.Metadata = map[string]string{}
			},
			want: false,
		},
		{
			// A terraform-state scope can carry repo_id in its metadata but
			// emits no file facts; reaping its (empty) Records would delete the
			// whole repository's content.
			name: "non-repository scope kind that carries a repo_id",
			mutate: func(scopeValue *scope.IngestionScope, _ *scope.ScopeGeneration, _ *[]facts.Envelope) {
				scopeValue.ScopeKind = scope.ScopeKind("terraform_state")
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scopeValue, generation, inputFacts := fullSnapshotFixture()
			tc.mutate(&scopeValue, &generation, &inputFacts)

			projection, err := buildProjection(scopeValue, generation, inputFacts)
			if err != nil {
				t.Fatalf("buildProjection() error = %v", err)
			}
			if got := projection.contentMaterialization.FullSnapshot; got != tc.want {
				t.Fatalf("contentMaterialization.FullSnapshot = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildProjectionRefScopeNeverMarksContentFullSnapshot keeps the ref-scope
// gate honest: a repository_ref scope projects nothing, so it cannot ask the
// writer to reap the default branch's content.
func TestBuildProjectionRefScopeNeverMarksContentFullSnapshot(t *testing.T) {
	t.Parallel()

	scopeValue, generation, inputFacts := fullSnapshotFixture()
	scopeValue.ScopeKind = scope.KindRepositoryRef

	projection, err := buildProjection(scopeValue, generation, inputFacts)
	if err != nil {
		t.Fatalf("buildProjection() error = %v", err)
	}
	if projection.contentMaterialization.FullSnapshot {
		t.Fatal("a repository_ref scope produced a FullSnapshot content materialization")
	}
}

// TestBuildProjectionRetainsFilePathsOnlyForFullSnapshots pins where the
// retained paths come from. The fixture has a file fact and no content fact,
// which is what the collector produces when a body re-read fails: the path must
// be retained on a full snapshot, so the reap cannot delete its content, and
// must not be set on a delta, which never reaps.
func TestBuildProjectionRetainsFilePathsOnlyForFullSnapshots(t *testing.T) {
	t.Parallel()

	scopeValue, generation, inputFacts := fullSnapshotFixture()
	full, err := buildProjection(scopeValue, generation, inputFacts)
	if err != nil {
		t.Fatalf("buildProjection(full) error = %v", err)
	}
	if got := full.contentMaterialization.RetainedPaths; len(got) != 1 || got[0] != "main.go" {
		t.Fatalf("full snapshot RetainedPaths = %v, want [main.go]", got)
	}
	if len(full.contentMaterialization.Records) != 0 {
		t.Fatalf("fixture has no content fact, Records = %v, want none", full.contentMaterialization.Records)
	}

	scopeValue, generation, inputFacts = fullSnapshotFixture()
	generation.IsDelta = true
	generation.DeltaBaselineCommitSHA = "abc123"
	delta, err := buildProjection(scopeValue, generation, inputFacts)
	if err != nil {
		t.Fatalf("buildProjection(delta) error = %v", err)
	}
	if got := delta.contentMaterialization.RetainedPaths; len(got) != 0 {
		t.Fatalf("delta RetainedPaths = %v, want none", got)
	}
}
