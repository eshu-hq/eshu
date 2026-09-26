// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// stubCorpusFencedResolvedLoader models a store whose by-repos read carries
// its own corpus-fence verdict from the same statement snapshot (#6740). It
// also serves the unfenced by-repos read so a loader that ignores the fused
// method still sees the same partial rows, exactly like the production
// RelationshipStore, which implements both.
type stubCorpusFencedResolvedLoader struct {
	repoResolved []relationships.ResolvedRelationship
	complete     bool
	err          error
	calls        int
	fencedCalls  int
	repoCalls    int
}

func (f *stubCorpusFencedResolvedLoader) GetResolvedRelationships(
	context.Context,
	string,
) ([]relationships.ResolvedRelationship, error) {
	f.calls++
	return nil, nil
}

func (f *stubCorpusFencedResolvedLoader) GetResolvedRelationshipsForRepos(
	context.Context,
	[]string,
) ([]relationships.ResolvedRelationship, error) {
	f.repoCalls++
	return f.repoResolved, nil
}

func (f *stubCorpusFencedResolvedLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	context.Context,
	[]string,
) ([]relationships.ResolvedRelationship, bool, error) {
	f.fencedCalls++
	if f.err != nil {
		return nil, false, f.err
	}
	return f.repoResolved, f.complete, nil
}

// partialForeignRelationships is the by-repos set a read returns while a
// foreign scope sits between retiring its generation and activating the
// next: the retired scope's rows are simply absent.
func partialForeignRelationships() []relationships.ResolvedRelationship {
	return []relationships.ResolvedRelationship{{
		SourceRepoID:     "repo-deploy",
		TargetRepoID:     "repo-service",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.9,
		EvidenceCount:    1,
		ResolutionSource: relationships.ResolutionSourceInferred,
	}}
}

// alwaysCompleteLookup is the separate-statement fence that a full
// advance-and-complete cycle between the pre-read check and the post-read
// recheck makes pass on both sides (#6740 residual).
func alwaysCompleteLookup(calls *int) func(context.Context) (bool, error) {
	return func(context.Context) (bool, error) {
		*calls++
		return true, nil
	}
}

func TestWorkloadProjectionInputsDeferWhenFencedReadReportsIncompleteCorpus(t *testing.T) {
	t.Parallel()

	lookupCalls := 0
	resolvedLoader := &stubCorpusFencedResolvedLoader{
		repoResolved: partialForeignRelationships(),
		complete:     false,
	}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:                &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader:            resolvedLoader,
		ResolutionActiveLookup:    stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
		ResolutionsCompleteLookup: alwaysCompleteLookup(&lookupCalls),
		IncompleteScopesLookup: func(context.Context) ([]string, error) {
			return []string{"scope-foreign"}, nil
		},
	}

	_, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), dockerfileLoaderIntent())
	if err == nil {
		t.Fatal("LoadWorkloadProjectionInputs() error = nil, want resolution-not-ready deferral when the fenced read reports an incomplete corpus")
	}
	var deferral workloadMaterializationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("LoadWorkloadProjectionInputs() error type = %T (%v), want workloadMaterializationResolutionNotReadyError", err, err)
	}
	if !deferral.Retryable() {
		t.Fatal("deferral Retryable() = false, want true so the queue re-runs on the settled corpus")
	}
	if !slices.Equal(deferral.holdingScopeIDs, []string{"scope-foreign"}) {
		t.Fatalf("deferral holdingScopeIDs = %v, want [scope-foreign]", deferral.holdingScopeIDs)
	}
	if resolvedLoader.fencedCalls != 1 {
		t.Fatalf("fenced read calls = %d, want 1", resolvedLoader.fencedCalls)
	}
	if resolvedLoader.repoCalls != 0 {
		t.Fatalf("unfenced by-repos read calls = %d, want 0 when the store offers the fenced read", resolvedLoader.repoCalls)
	}
}

func TestWorkloadProjectionInputsTrustFencedReadVerdictOverSeparateLookup(t *testing.T) {
	t.Parallel()

	lookupCalls := 0
	resolvedLoader := &stubCorpusFencedResolvedLoader{complete: true}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader:         resolvedLoader,
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
		ResolutionsCompleteLookup: func(context.Context) (bool, error) {
			lookupCalls++
			return false, nil
		},
	}

	if _, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), dockerfileLoaderIntent()); err != nil {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v, want nil: the fenced read's own snapshot verdict is authoritative", err)
	}
	if lookupCalls != 0 {
		t.Fatalf("separate fence lookup calls = %d, want 0: the fused statement replaces the pre-read check and the post-read recheck", lookupCalls)
	}
	if resolvedLoader.fencedCalls != 1 {
		t.Fatalf("fenced read calls = %d, want 1", resolvedLoader.fencedCalls)
	}
}

func TestWorkloadProjectionInputsFailOnFencedReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("fenced read unavailable")
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
		ResolvedLoader:         &stubCorpusFencedResolvedLoader{err: readErr},
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
	}

	_, _, err := loader.LoadWorkloadProjectionInputs(context.Background(), dockerfileLoaderIntent())
	if !errors.Is(err, readErr) {
		t.Fatalf("LoadWorkloadProjectionInputs() error = %v, want wrapped fenced read error", err)
	}
}

func TestDeployableUnitCorrelationHandleDefersWhenFencedReadReportsIncompleteCorpus(t *testing.T) {
	t.Parallel()

	lookupCalls := 0
	resolvedLoader := &stubCorpusFencedResolvedLoader{
		repoResolved: partialForeignRelationships(),
		complete:     false,
	}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:                &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:            resolvedLoader,
		ResolutionActiveLookup:    stubResolutionActiveLookup(map[string]bool{"generation-1": true}),
		ResolutionsCompleteLookup: alwaysCompleteLookup(&lookupCalls),
		IncompleteScopesLookup: func(context.Context) ([]string, error) {
			return []string{"scope-foreign"}, nil
		},
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err == nil {
		t.Fatal("Handle() error = nil, want resolution-not-ready deferral when the fenced read reports an incomplete corpus")
	}
	var deferral deployableUnitCorrelationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("Handle() error type = %T (%v), want deployableUnitCorrelationResolutionNotReadyError", err, err)
	}
	if !deferral.Retryable() {
		t.Fatal("deferral Retryable() = false, want true so the queue re-runs on the settled corpus")
	}
	if !slices.Equal(deferral.holdingScopeIDs, []string{"scope-foreign"}) {
		t.Fatalf("deferral holdingScopeIDs = %v, want [scope-foreign]", deferral.holdingScopeIDs)
	}
	if resolvedLoader.fencedCalls != 1 || resolvedLoader.repoCalls != 0 {
		t.Fatalf("fenced/unfenced read calls = %d/%d, want 1/0", resolvedLoader.fencedCalls, resolvedLoader.repoCalls)
	}
}
