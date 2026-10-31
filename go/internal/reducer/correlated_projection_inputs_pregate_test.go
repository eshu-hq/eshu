// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
)

// TestWorkloadProjectionScopeInputsSkipBulkLoadWhileOwnGenerationInactive is
// the #9697 failing-first regression for the load-then-discard path: when the
// scope's own relationship generation is inactive the loader defers, so it
// must not pay for the bulk scope-local fact load first. 149/154
// workload-materialization loads on the OOM host were bulk loads discarded by
// exactly this gate.
func TestWorkloadProjectionScopeInputsSkipBulkLoadWhileOwnGenerationInactive(t *testing.T) {
	t.Parallel()

	factLoader := &stubFactLoader{envelopes: dockerfileLoaderEnvelopes(), mayHoldCandidates: true}
	resolvedLoader := &stubResolvedRelationshipLoader{}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:     factLoader,
		ResolvedLoader: resolvedLoader,
		// Own generation intentionally absent: resolution has not activated it.
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{}),
	}

	_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), dockerfileLoaderIntent())
	if err == nil {
		t.Fatal("LoadWorkloadProjectionScopeInputs() error = nil, want resolution-not-ready deferral")
	}
	var deferral workloadMaterializationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("LoadWorkloadProjectionScopeInputs() error type = %T, want workloadMaterializationResolutionNotReadyError", err)
	}
	if factLoader.calls != 0 {
		t.Fatalf("fact loader calls = %d, want 0: the readiness pre-gate must fire before the bulk fact load",
			factLoader.calls)
	}
	if resolvedLoader.calls != 0 || resolvedLoader.repoCalls != 0 {
		t.Fatalf("resolved loader calls = %d/%d, want 0/0: the gate must fire before the fail-open read",
			resolvedLoader.calls, resolvedLoader.repoCalls)
	}
}

// TestWorkloadProjectionScopeInputsSkipBulkLoadWhileCorpusIncomplete is the
// #9697 failing-first regression for the foreign-fence half of the
// load-then-discard path: when the corpus-wide fence is incomplete the loader
// defers, so it must not pay for the bulk scope-local fact load first.
func TestWorkloadProjectionScopeInputsSkipBulkLoadWhileCorpusIncomplete(t *testing.T) {
	t.Parallel()

	factLoader := &stubFactLoader{envelopes: dockerfileLoaderEnvelopes(), mayHoldCandidates: true}
	resolvedLoader := &stubResolvedRelationshipLoader{}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:     factLoader,
		ResolvedLoader: resolvedLoader,
		// Own generation is active, but a foreign scope's resolution has not
		// activated yet: the by-repos read would serve a partial set (#6184).
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
		ResolutionsCompleteLookup: func(context.Context) (bool, error) {
			return false, nil
		},
	}

	_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), dockerfileLoaderIntent())
	if err == nil {
		t.Fatal("LoadWorkloadProjectionScopeInputs() error = nil, want resolution-not-ready deferral while a foreign generation is inactive")
	}
	var deferral workloadMaterializationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("LoadWorkloadProjectionScopeInputs() error type = %T, want workloadMaterializationResolutionNotReadyError", err)
	}
	if factLoader.calls != 0 {
		t.Fatalf("fact loader calls = %d, want 0: the corpus pre-gate must fire before the bulk fact load",
			factLoader.calls)
	}
	if resolvedLoader.calls != 0 || resolvedLoader.repoCalls != 0 {
		t.Fatalf("resolved loader calls = %d/%d, want 0/0: the gate must fire before the fail-open read",
			resolvedLoader.calls, resolvedLoader.repoCalls)
	}
}

// TestWorkloadProjectionScopeInputsBulkLoadsFactsWhenReady guards the
// pre-gate against over-deferral: when own and corpus resolution are complete
// the loader still issues exactly one bulk fact load and succeeds.
func TestWorkloadProjectionScopeInputsBulkLoadsFactsWhenReady(t *testing.T) {
	t.Parallel()

	factLoader := &stubFactLoader{envelopes: dockerfileLoaderEnvelopes(), mayHoldCandidates: true}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             factLoader,
		ResolvedLoader:         &stubResolvedRelationshipLoader{},
		ResolutionActiveLookup: stubResolutionActiveLookup(map[string]bool{"gen-1": true}),
		ResolutionsCompleteLookup: func(context.Context) (bool, error) {
			return true, nil
		},
	}

	_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), dockerfileLoaderIntent())
	if err != nil {
		t.Fatalf("LoadWorkloadProjectionScopeInputs() error = %v, want nil when own and corpus resolution are complete", err)
	}
	if factLoader.calls != 1 {
		t.Fatalf("fact loader calls = %d, want 1: the ready path must still issue its single bulk fact load",
			factLoader.calls)
	}
}
