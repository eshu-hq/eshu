// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// stubScopeReposReader records ownership lookups and returns a fixed repo set.
type stubScopeReposReader struct {
	calls int
	repos []string
}

func (s *stubScopeReposReader) ListScopeRepositoryIDs(
	_ context.Context,
	_, _ string,
) ([]string, error) {
	s.calls++
	return s.repos, nil
}

// stubCrossRepoEvidenceLoader satisfies the resolver branch's evidence gate
// with no rows.
type stubCrossRepoEvidenceLoader struct{}

func (stubCrossRepoEvidenceLoader) ListEvidenceFacts(
	context.Context,
	string,
) ([]relationships.EvidenceFact, error) {
	return nil, nil
}

// stubCrossRepoIntentWriter accepts any intent rows.
type stubCrossRepoIntentWriter struct{}

func (stubCrossRepoIntentWriter) UpsertIntents(
	context.Context,
	[]sharedintent.Row,
) error {
	return nil
}

// TestDeploymentMappingRegistrationCarriesScopeRepos guards the wiring, not
// the logic.
//
// The ownership partition is only worth anything if the registered resolver
// actually carries the scope-reader seam. A partition that is correct in
// isolation and unwired in DefaultHandlers ships inert: every unit test
// passes, production falls back to legacy emit-all, and only the
// determinism matrix (which compares worker-count digests) notices. This
// test fails if the field is dropped from the registration.
func TestDeploymentMappingRegistrationCarriesScopeRepos(t *testing.T) {
	t.Parallel()

	repos := &stubScopeReposReader{repos: []string{"repo-a"}}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		EvidenceFactLoader:         stubCrossRepoEvidenceLoader{},
		RepoDependencyIntentWriter: stubCrossRepoIntentWriter{},
		ScopeRepos:                 repos,
	})

	var resolver CrossRepoRelationshipResolver
	found := false
	for _, definition := range definitions {
		if definition.Domain != DomainDeploymentMapping {
			continue
		}
		typed, ok := definition.Handler.(PlatformMaterializationHandler)
		if !ok {
			t.Fatalf("handler for %s = %T, want PlatformMaterializationHandler", definition.Domain, definition.Handler)
		}
		if typed.CrossRepoResolver == nil {
			t.Fatal("CrossRepoResolver is nil on the registered handler: the ownership partition would ship inert")
		}
		resolver, found = typed.CrossRepoResolver, true
	}
	if !found {
		t.Fatalf("no %s registration found", DomainDeploymentMapping)
	}

	concrete, ok := resolver.(*crossrepo.CrossRepoRelationshipHandler)
	if !ok {
		t.Fatalf("CrossRepoResolver = %T, want *crossrepo.CrossRepoRelationshipHandler", resolver)
	}

	// Prove it is the seam we passed, not some other non-nil reader, by
	// identity and then by observing the call.
	if concrete.ScopeRepos != crossrepo.ScopeRepositoryReader(repos) {
		t.Fatal("ScopeRepos is not the reader passed to DefaultHandlers: resolution would classify against a different scope set")
	}
	if _, err := concrete.ScopeRepos.ListScopeRepositoryIDs(
		context.Background(), "scope:wiring", "gen:wiring",
	); err != nil {
		t.Fatalf("ListScopeRepositoryIDs() error = %v", err)
	}
	if repos.calls != 1 {
		t.Fatalf("scope reader calls = %d, want 1: the registration wired a different seam", repos.calls)
	}
}
