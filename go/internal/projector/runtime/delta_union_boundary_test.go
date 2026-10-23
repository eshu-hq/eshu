// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// The #8779 negative cases for the projection fact boundary. Accepting a
// delta's union base (#7797) must not widen the boundary to any generation or
// any scope: a fact from a third generation, or from another scope, still
// fails the whole projection before any write. The production union read can
// only deliver delta and base rows, so these inputs are built here, one layer
// below the live proof in storage/postgres/delta_union_projection_live_test.go.

const (
	boundaryScopeID    = "git-repository-scope:boundary-8779"
	boundaryDeltaGenID = "gen-8779-delta-b"
	boundaryThirdGenID = "gen-8779-third-c"
	boundaryBaseGenID  = "gen-8779-base-a"
)

func boundaryDeltaScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       boundaryScopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-8779",
		Metadata:      map[string]string{"repo_id": "repo-8779"},
	}
}

func boundaryDeltaGeneration() scope.ScopeGeneration {
	observed := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	return scope.ScopeGeneration{
		GenerationID:           boundaryDeltaGenID,
		ScopeID:                boundaryScopeID,
		ObservedAt:             observed,
		IngestedAt:             observed,
		Status:                 scope.GenerationStatusPending,
		TriggerKind:            scope.TriggerKindSnapshot,
		IsDelta:                true,
		DeltaBaselineCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa8779",
	}
}

// boundaryDeltaRepositoryFact is the delta's own repository fact.
func boundaryDeltaRepositoryFact() facts.Envelope {
	return facts.Envelope{
		FactID:        "fact-8779-delta-repo",
		ScopeID:       boundaryScopeID,
		GenerationID:  boundaryDeltaGenID,
		FactKind:      "repository",
		StableFactKey: "repository:repo-8779",
		ObservedAt:    time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"graph_id": "repo-8779", "graph_kind": "repository", "name": "boundary-8779",
			"repo_id": "repo-8779", "delta_generation": true,
			"delta_relative_paths": []any{"keep.go"}, "delta_deleted_relative_paths": []any{},
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: "fact-8779-delta-repo"},
	}
}

// boundaryFileFact is a file fact for keep.go under the given scope and
// generation.
func boundaryFileFact(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "file",
		StableFactKey: "file:repo-8779:keep.go",
		ObservedAt:    time.Date(2026, time.October, 22, 12, 0, 1, 0, time.UTC),
		Payload: map[string]any{
			"graph_id": "repo-8779:keep.go", "graph_kind": "file", "repo_id": "repo-8779",
			"relative_path": "keep.go", "is_dependency": false,
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: factID},
	}
}

func TestProjectRejectsThirdGenerationFactInDeltaProjection(t *testing.T) {
	t.Parallel()
	assertBoundaryRejects(t, boundaryFileFact("fact-8779-third-c", boundaryScopeID, boundaryThirdGenID),
		"fact-8779-third-c", boundaryThirdGenID)
}

// With the union base named, a third generation is still rejected: the
// boundary widens to exactly {delta, base}.
func TestProjectRejectsThirdGenerationFactWithUnionBaseSet(t *testing.T) {
	t.Parallel()
	generation := boundaryDeltaGeneration()
	generation.DeltaUnionBaseGenerationID = boundaryBaseGenID
	assertProjectRejects(t, generation,
		[]facts.Envelope{boundaryDeltaRepositoryFact(), boundaryFileFact("fact-8779-third-c", boundaryScopeID, boundaryThirdGenID)},
		"fact-8779-third-c", boundaryThirdGenID)
}

// A base fact is accepted when the generation names that base as its union
// base, and only then (the committed RED covers the unnamed case).
func TestProjectAcceptsUnionBaseFact(t *testing.T) {
	t.Parallel()
	generation := boundaryDeltaGeneration()
	generation.DeltaUnionBaseGenerationID = boundaryBaseGenID
	runtime := Runtime{CanonicalWriter: &recordingCanonicalWriter{}, ContentWriter: &recordingContentWriter{}, IntentWriter: &recordingIntentWriter{}}
	if _, err := runtime.Project(context.Background(), boundaryDeltaScope(), generation,
		[]facts.Envelope{boundaryDeltaRepositoryFact(), boundaryFileFact("fact-8779-base-a", boundaryScopeID, boundaryBaseGenID)}); err != nil {
		t.Fatalf("Project(delta + its union base fact) error = %v, want nil", err)
	}
	generation.DeltaUnionBaseGenerationID = ""
	assertProjectRejects(t, generation,
		[]facts.Envelope{boundaryDeltaRepositoryFact(), boundaryFileFact("fact-8779-base-a", boundaryScopeID, boundaryBaseGenID)},
		"fact-8779-base-a", boundaryBaseGenID)
}

// A union base on a non-delta generation, or naming the generation itself, is
// a programming error and fails before any fact is read.
func TestProjectRejectsMisusedUnionBase(t *testing.T) {
	t.Parallel()
	nonDelta := boundaryDeltaGeneration()
	nonDelta.IsDelta = false
	nonDelta.DeltaUnionBaseGenerationID = boundaryBaseGenID
	assertProjectRejects(t, nonDelta, []facts.Envelope{boundaryDeltaRepositoryFact()},
		"delta_union_base_generation_id", boundaryBaseGenID)

	self := boundaryDeltaGeneration()
	self.DeltaUnionBaseGenerationID = boundaryDeltaGenID
	assertProjectRejects(t, self, []facts.Envelope{boundaryDeltaRepositoryFact()},
		"delta_union_base_generation_id", boundaryDeltaGenID)
}

// A union base with no delta_generation repository fact among the delta's own
// facts is the #7797 union_scope_mismatch shape: the overlay ran additively,
// so projecting it would run a full retract over a duplicated set. It must
// fail before any write.
func TestProjectRejectsUnionScopeMismatch(t *testing.T) {
	t.Parallel()
	generation := boundaryDeltaGeneration()
	generation.DeltaUnionBaseGenerationID = boundaryBaseGenID
	unmarked := boundaryDeltaRepositoryFact()
	unmarked.Payload = map[string]any{
		"graph_id": "repo-8779", "graph_kind": "repository", "name": "boundary-8779", "repo_id": "repo-8779",
	}
	assertProjectRejects(t, generation,
		[]facts.Envelope{unmarked, boundaryFileFact("fact-8779-base-a", boundaryScopeID, boundaryBaseGenID)},
		"delta_generation", boundaryBaseGenID)
}

func TestProjectRejectsForeignScopeFactInDeltaProjection(t *testing.T) {
	t.Parallel()
	assertBoundaryRejects(t, boundaryFileFact("fact-8779-foreign", "git-repository-scope:other", boundaryDeltaGenID),
		"fact-8779-foreign", "git-repository-scope:other")
}

// assertBoundaryRejects projects the delta's repository fact plus stray and
// requires a boundary error naming stray, with no canonical, content, or
// reducer write.
func assertBoundaryRejects(t *testing.T, stray facts.Envelope, wantFactID, wantForeign string) {
	t.Helper()
	assertProjectRejects(t, boundaryDeltaGeneration(),
		[]facts.Envelope{boundaryDeltaRepositoryFact(), stray}, wantFactID, wantForeign)
}

// assertProjectRejects projects input under generation and requires an error
// naming both want strings, with no canonical, content, or reducer write.
func assertProjectRejects(t *testing.T, generation scope.ScopeGeneration, input []facts.Envelope, want, wantForeign string) {
	t.Helper()
	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{}
	intentWriter := &recordingIntentWriter{}
	runtime := Runtime{CanonicalWriter: canonicalWriter, ContentWriter: contentWriter, IntentWriter: intentWriter}

	_, err := runtime.Project(context.Background(), boundaryDeltaScope(), generation, input)
	if err == nil {
		t.Fatalf("Project() error = nil, want a rejection naming %q", want)
	}
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), wantForeign) {
		t.Fatalf("Project() error = %q, want it to name %q and %q", err, want, wantForeign)
	}
	if len(canonicalWriter.calls) != 0 || len(contentWriter.calls) != 0 || len(intentWriter.calls) != 0 {
		t.Fatalf("writes after a boundary rejection: canonical=%d content=%d intents=%d, want none",
			len(canonicalWriter.calls), len(contentWriter.calls), len(intentWriter.calls))
	}
}
