// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

type fakeScopeRepositoryReader struct {
	repos []string
	err   error
}

func (f *fakeScopeRepositoryReader) ListScopeRepositoryIDs(_ context.Context, _, _ string) ([]string, error) {
	return f.repos, f.err
}

func ownershipTestFacts() []relationships.EvidenceFact {
	return []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindDockerComposeDependsOn,
			RelationshipType: relationships.RelDependsOn,
			SourceRepoID:     "repo-own",
			TargetRepoID:     "repo-foreign",
			Confidence:       0.9,
		},
		{
			EvidenceKind:     relationships.EvidenceKindDockerComposeDependsOn,
			RelationshipType: relationships.RelDependsOn,
			SourceRepoID:     "repo-foreign",
			TargetRepoID:     "repo-own",
			Confidence:       0.9,
		},
	}
}

// TestCrossRepoResolutionEmitsOnlyOwnedEdges proves the single-writer
// partition: a scope that sees both its own and backward evidence emits only
// the edges sourced in its own repositories. Without it, two scopes resolve
// the same logical edge and the graph writer MERGEs them last-writer-wins on
// the generation stamp, so worker-count scheduling decides the byte-exact
// digest (#6184: multi-source→multi-target stamped multi-source in N=1 and
// multi-target in N=4).
func TestCrossRepoResolutionEmitsOnlyOwnedEdges(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	persister := &fakeResolutionPersister{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: ownershipTestFacts()},
		IntentWriter:   intentWriter,
		Persister:      persister,
		ScopeRepos:     &fakeScopeRepositoryReader{repos: []string{"repo-own"}},
	}

	count, err := handler.Resolve(context.Background(), "scope-own", "gen-own")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1 (only the owned edge)", count)
	}
	if len(intentWriter.rows) != 1 || len(intentWriter.rows[0]) != 1 {
		t.Fatalf("intent writes = %#v, want exactly 1 row", intentWriter.rows)
	}
	row := intentWriter.rows[0][0]
	if got, want := stringValue(row.Payload["repo_id"]), "repo-own"; got != want {
		t.Fatalf("repo_id = %q, want %q (foreign edge must not emit)", got, want)
	}
	if len(persister.resolved) != 1 {
		t.Fatalf("persisted resolved = %d, want 1", len(persister.resolved))
	}
	if got := persister.resolved[0].SourceRepoID; got != "repo-own" {
		t.Fatalf("persisted resolved source = %q, want repo-own", got)
	}
	if len(persister.activatedGenerations) != 1 || persister.activatedGenerations[0] != "gen-own" {
		t.Fatalf("activated = %v, want [gen-own] (activation is unaffected by the partition)", persister.activatedGenerations)
	}
}

// TestCrossRepoResolutionOwnershipLegacyWithoutReader proves a nil reader
// keeps the legacy emit-all behavior for callers that do not wire it.
func TestCrossRepoResolutionOwnershipLegacyWithoutReader(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: ownershipTestFacts()},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-own", "gen-own")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("Resolve() = %d, want 2 (legacy emit-all)", count)
	}
}

// TestCrossRepoResolutionOwnershipLegacyWhenUnclassifiable proves a scope
// with no repository facts keeps emit-all rather than silently dropping
// edges it cannot attribute.
func TestCrossRepoResolutionOwnershipLegacyWhenUnclassifiable(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: ownershipTestFacts()},
		IntentWriter:   intentWriter,
		ScopeRepos:     &fakeScopeRepositoryReader{repos: nil},
	}

	count, err := handler.Resolve(context.Background(), "scope-own", "gen-own")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("Resolve() = %d, want 2 (legacy emit-all)", count)
	}
}

// TestCrossRepoResolutionOwnershipLookupFailure proves a repository lookup
// failure is fatal rather than guessed: emitting unpartitioned would
// reintroduce the stamp race, and emitting nothing would strand edges.
func TestCrossRepoResolutionOwnershipLookupFailure(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: ownershipTestFacts()},
		IntentWriter:   intentWriter,
		ScopeRepos:     &fakeScopeRepositoryReader{err: errors.New("boom")},
	}

	if _, err := handler.Resolve(context.Background(), "scope-own", "gen-own"); err == nil {
		t.Fatal("Resolve() error = nil, want lookup failure")
	}
	if len(intentWriter.rows) != 0 {
		t.Fatalf("intent writes = %d, want 0 (nothing emitted on lookup failure)", len(intentWriter.rows))
	}
}
