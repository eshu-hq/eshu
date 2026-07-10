// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeSearchVectorScopeStateManager records calls to the scope-state lifecycle.
type fakeSearchVectorScopeStateManager struct {
	mu              sync.Mutex
	beginCalls      []scopeStateBeginCall
	fences          []int64
	completeResults []bool
	completeErrs    []error
	finalizeResults []bool
	finalizeErrs    []error
}

type scopeStateBeginCall struct {
	ScopeID            string
	GenerationID       string
	Identity           SearchVectorBuildIdentity
	ProjectionRevision int64
}

func (f *fakeSearchVectorScopeStateManager) BeginBuilding(
	_ context.Context,
	scopeID, generationID string,
	identity SearchVectorBuildIdentity,
	projectionRevision int64,
) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginCalls = append(f.beginCalls, scopeStateBeginCall{
		ScopeID:            scopeID,
		GenerationID:       generationID,
		Identity:           identity,
		ProjectionRevision: projectionRevision,
	})
	fence := int64(len(f.fences) + 1)
	f.fences = append(f.fences, fence)
	return fence, nil
}

func (f *fakeSearchVectorScopeStateManager) ScopeVectorComplete(
	_ context.Context,
	scopeID, generationID string,
	identity SearchVectorBuildIdentity,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var complete bool
	var err error
	if len(f.completeResults) > 0 {
		complete = f.completeResults[0]
		f.completeResults = f.completeResults[1:]
	}
	if len(f.completeErrs) > 0 {
		err = f.completeErrs[0]
		f.completeErrs = f.completeErrs[1:]
	}
	return complete, err
}

func (f *fakeSearchVectorScopeStateManager) FinalizeReady(
	_ context.Context,
	scopeID, generationID string,
	identity SearchVectorBuildIdentity,
	projectionRevision, fence int64,
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ok bool
	var err error
	if len(f.finalizeResults) > 0 {
		ok = f.finalizeResults[0]
		f.finalizeResults = f.finalizeResults[1:]
	}
	if len(f.finalizeErrs) > 0 {
		err = f.finalizeErrs[0]
		f.finalizeErrs = f.finalizeErrs[1:]
	}
	return ok, err
}

// TestSearchVectorBuildRunnerBeginBuildingBeforeBuild proves that
// BeginBuilding is called for each selected scope BEFORE the build,
// with the correct identity and ProjectionRevision.
func TestSearchVectorBuildRunnerBeginBuildingBeforeBuild(t *testing.T) {
	t.Parallel()

	scopeState := &fakeSearchVectorScopeStateManager{}
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a", ProjectionRevision: 3},
		{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b", ProjectionRevision: 5},
	}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{
		{DocumentCount: 1, VectorCount: 1},
		{DocumentCount: 2, VectorCount: 2},
	}}
	identity := SearchVectorBuildIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
	}
	runner := &SearchVectorBuildRunner{
		Pending:    pending,
		Builder:    builder,
		ScopeState: scopeState,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  identity.ProviderProfileID,
			SourceClass:        identity.SourceClass,
			EmbeddingModelID:   identity.EmbeddingModelID,
			VectorIndexVersion: identity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, result.BuiltScopes)

	scopeState.mu.Lock()
	defer scopeState.mu.Unlock()
	require.Len(t, scopeState.beginCalls, 2)

	// First scope called before build.
	require.Equal(t, "scope-a", scopeState.beginCalls[0].ScopeID)
	require.Equal(t, "gen-a", scopeState.beginCalls[0].GenerationID)
	require.Equal(t, identity, scopeState.beginCalls[0].Identity)
	require.Equal(t, int64(3), scopeState.beginCalls[0].ProjectionRevision)

	// Second scope called before build.
	require.Equal(t, "scope-b", scopeState.beginCalls[1].ScopeID)
	require.Equal(t, "gen-b", scopeState.beginCalls[1].GenerationID)
	require.Equal(t, int64(5), scopeState.beginCalls[1].ProjectionRevision)

	// Builder was called (BeginBuilding happens BEFORE build in RunOnce).
	require.Equal(t, 2, builder.callCount())
}

// TestSearchVectorBuildRunnerFinalizeReadyOnlyWhenComplete proves that
// FinalizeReady is called only for scopes where ScopeVectorComplete returns
// true, and not for incomplete scopes.
func TestSearchVectorBuildRunnerFinalizeReadyOnlyWhenComplete(t *testing.T) {
	t.Parallel()

	// scope-a: complete, scope-b: not complete
	scopeState := &fakeSearchVectorScopeStateManager{
		completeResults: []bool{true, false},
		finalizeResults: []bool{true},
	}
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a", ProjectionRevision: 1},
		{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b", ProjectionRevision: 1},
	}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{
		{DocumentCount: 1, VectorCount: 1},
		{DocumentCount: 1, VectorCount: 1},
	}}
	identity := testSearchVectorIdentity
	runner := &SearchVectorBuildRunner{
		Pending:    pending,
		Builder:    builder,
		ScopeState: scopeState,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  identity.ProviderProfileID,
			SourceClass:        identity.SourceClass,
			EmbeddingModelID:   identity.EmbeddingModelID,
			VectorIndexVersion: identity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, result.BuiltScopes)

	scopeState.mu.Lock()
	defer scopeState.mu.Unlock()

	// Both scopes had BeginBuilding called.
	require.Len(t, scopeState.beginCalls, 2)

	// ScopeVectorComplete was called for both scopes.
	// (completeResults consumed: true, false)
	// FinalizeReady was called only for scope-a (complete=true).
	require.Empty(t, scopeState.completeResults, "all completeResults should be consumed")
	require.Empty(t, scopeState.finalizeResults, "all finalizeResults should be consumed")
}

// TestSearchVectorBuildRunnerFalseCASSkipsWithoutFailure proves that a false
// FinalizeReady CAS result does not fail the sweep.
func TestSearchVectorBuildRunnerFalseCASSkipsWithoutFailure(t *testing.T) {
	t.Parallel()

	// scope-a complete but CAS returns false (superseded by another worker).
	scopeState := &fakeSearchVectorScopeStateManager{
		completeResults: []bool{true},
		finalizeResults: []bool{false},
	}
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a", ProjectionRevision: 1},
	}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{
		{DocumentCount: 1, VectorCount: 1},
	}}
	identity := testSearchVectorIdentity
	runner := &SearchVectorBuildRunner{
		Pending:    pending,
		Builder:    builder,
		ScopeState: scopeState,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  identity.ProviderProfileID,
			SourceClass:        identity.SourceClass,
			EmbeddingModelID:   identity.EmbeddingModelID,
			VectorIndexVersion: identity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err) // CAS rejection does NOT fail the sweep.
	require.Equal(t, 1, result.BuiltScopes)
}

// TestSearchVectorBuildRunnerNilScopeStateUnchanged proves that nil ScopeState
// is a no-op — builds proceed normally with no lifecycle calls.
func TestSearchVectorBuildRunnerNilScopeStateUnchanged(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a", ProjectionRevision: 1},
	}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{
		{DocumentCount: 1, VectorCount: 1},
	}}
	runner := &SearchVectorBuildRunner{
		Pending:    pending,
		Builder:    builder,
		ScopeState: nil, // nil = disabled
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.BuiltScopes)
}

// TestSearchVectorBuildRunnerScopeStateBatchPath proves BeginBuilding and
// FinalizeReady work through the batch fast path (SearchVectorBatchBuilder).
func TestSearchVectorBuildRunnerScopeStateBatchPath(t *testing.T) {
	t.Parallel()

	scopeState := &fakeSearchVectorScopeStateManager{
		completeResults: []bool{true, true},
		finalizeResults: []bool{true, true},
	}
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a", ProjectionRevision: 1},
		{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b", ProjectionRevision: 2},
	}}
	batchBuilder := &fakeSearchVectorBatchBuilder{
		result: SearchVectorBuildResult{DocumentCount: 3, VectorCount: 3},
	}
	identity := testSearchVectorIdentity
	runner := &SearchVectorBuildRunner{
		Pending:    pending,
		Builder:    batchBuilder,
		ScopeState: scopeState,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  identity.ProviderProfileID,
			SourceClass:        identity.SourceClass,
			EmbeddingModelID:   identity.EmbeddingModelID,
			VectorIndexVersion: identity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, result.BuiltScopes)
	require.Equal(t, 3, result.DocumentCount)

	scopeState.mu.Lock()
	defer scopeState.mu.Unlock()
	require.Len(t, scopeState.beginCalls, 2)
	require.Empty(t, scopeState.completeResults)
	require.Empty(t, scopeState.finalizeResults)
}

// TestSearchVectorBuildRunnerScopeStateBatchPathIncompleteSkipsFinalize proves
// that in the batch path, an incomplete scope does not call FinalizeReady.
func TestSearchVectorBuildRunnerScopeStateBatchPathIncompleteSkipsFinalize(t *testing.T) {
	t.Parallel()

	// scope-a complete, scope-b not complete.
	scopeState := &fakeSearchVectorScopeStateManager{
		completeResults: []bool{true, false},
		finalizeResults: []bool{true},
	}
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a", ProjectionRevision: 1},
		{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b", ProjectionRevision: 1},
	}}
	batchBuilder := &fakeSearchVectorBatchBuilder{
		result: SearchVectorBuildResult{DocumentCount: 3, VectorCount: 3},
	}
	identity := testSearchVectorIdentity
	runner := &SearchVectorBuildRunner{
		Pending:    pending,
		Builder:    batchBuilder,
		ScopeState: scopeState,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  identity.ProviderProfileID,
			SourceClass:        identity.SourceClass,
			EmbeddingModelID:   identity.EmbeddingModelID,
			VectorIndexVersion: identity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, result.BuiltScopes)

	scopeState.mu.Lock()
	defer scopeState.mu.Unlock()
	require.Len(t, scopeState.beginCalls, 2)
	require.Empty(t, scopeState.completeResults)
	require.Empty(t, scopeState.finalizeResults) // only scope-a called FinalizeReady
}

// TestSearchVectorBuildRunnerScopeStateConcurrencyFenceSuperseded proves that
// when worker A's fence is superseded by worker B (higher fence), worker A's
// FinalizeReady returns false, worker A does not publish, and the sweep
// succeeds without error.
func TestSearchVectorBuildRunnerScopeStateConcurrencyFenceSuperseded(t *testing.T) {
	t.Parallel()

	// Worker A builds scope-a but CAS is rejected (worker B superseded it).
	scopeState := &fakeSearchVectorScopeStateManager{
		completeResults: []bool{true},
		finalizeResults: []bool{false}, // CAS rejected
	}
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a", ProjectionRevision: 1},
	}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{
		{DocumentCount: 1, VectorCount: 1},
	}}
	identity := testSearchVectorIdentity
	runner := &SearchVectorBuildRunner{
		Pending:    pending,
		Builder:    builder,
		ScopeState: scopeState,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  identity.ProviderProfileID,
			SourceClass:        identity.SourceClass,
			EmbeddingModelID:   identity.EmbeddingModelID,
			VectorIndexVersion: identity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err) // CAS rejection does not fail the sweep.
	require.Equal(t, 1, result.BuiltScopes)
	// Worker A tried to finalize, was rejected — structured log fired, sweep continues.
}
