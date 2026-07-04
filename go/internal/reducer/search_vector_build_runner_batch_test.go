// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchVectorBuildRunnerUsesBatchBuilderForPendingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a"},
		{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b"},
		{ScopeID: "scope-c", GenerationID: "gen-c", RepoID: "repo-c"},
	}}
	builder := &fakeSearchVectorBatchBuilder{
		result: SearchVectorBuildResult{DocumentCount: 9, VectorCount: 9},
	}
	runner := &SearchVectorBuildRunner{
		Pending: pending,
		Builder: builder,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			DocumentLimit:      50,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 3, result.PendingScopes)
	require.Equal(t, 3, result.BuiltScopes)
	require.Equal(t, 9, result.DocumentCount)
	require.Equal(t, 9, result.VectorCount)
	require.Len(t, builder.batchRequests, 1)
	require.Empty(t, builder.requests, "batch-capable builders must avoid serial per-scope calls")
	require.Equal(t, []SearchVectorBuildRequest{
		{
			ScopeID:            "scope-a",
			GenerationID:       "gen-a",
			RepoID:             "repo-a",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			Limit:              50,
		},
		{
			ScopeID:            "scope-b",
			GenerationID:       "gen-b",
			RepoID:             "repo-b",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			Limit:              50,
		},
		{
			ScopeID:            "scope-c",
			GenerationID:       "gen-c",
			RepoID:             "repo-c",
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			Limit:              50,
		},
	}, builder.batchRequests[0])
}

// TestSearchVectorBuildRunnerBatchPathPublishesReadyAfterDrainingLastPendingScopes
// proves the search_vector_ready signal fires on the production FAST PATH
// (SearchVectorBatchBuilder, used by searchVectorBuilderAdapter in
// cmd/reducer) when a batch sweep drains the last pending scopes to zero —
// not just the serial per-scope path (#4673 review fix, bugs #4/#5). Before
// the fix, the batch path returned before the ready-publish call was ever
// reached, so production never published the signal.
func TestSearchVectorBuildRunnerBatchPathPublishesReadyAfterDrainingLastPendingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{
		scopes: []SearchVectorBuildPendingScope{
			{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a"},
		},
		postBuildScopes: nil,
	}
	builder := &fakeSearchVectorBatchBuilder{result: SearchVectorBuildResult{DocumentCount: 1, VectorCount: 1}}
	publisher := &fakeSearchVectorReadyPublisher{}
	runner := &SearchVectorBuildRunner{
		Pending:        pending,
		Builder:        builder,
		ReadyPublisher: publisher,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  testSearchVectorIdentity.ProviderProfileID,
			SourceClass:        testSearchVectorIdentity.SourceClass,
			EmbeddingModelID:   testSearchVectorIdentity.EmbeddingModelID,
			VectorIndexVersion: testSearchVectorIdentity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.PendingScopes, "pre-build listing was non-empty")
	require.Equal(t, 1, publisher.calls, "batch path drained the last pending scope; ready must publish")
	require.Equal(t, []SearchVectorBuildIdentity{testSearchVectorIdentity}, publisher.identities)
}

// TestSearchVectorBuildRunnerBatchPathDoesNotPublishReadyWithPendingScopes
// proves the batch fast path does NOT publish search_vector_ready when the
// post-build re-check still finds pending scopes.
func TestSearchVectorBuildRunnerBatchPathDoesNotPublishReadyWithPendingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{
		scopes: []SearchVectorBuildPendingScope{
			{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a"},
		},
		postBuildScopes: []SearchVectorBuildPendingScope{
			{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b"},
		},
	}
	builder := &fakeSearchVectorBatchBuilder{result: SearchVectorBuildResult{DocumentCount: 1, VectorCount: 1}}
	publisher := &fakeSearchVectorReadyPublisher{}
	runner := &SearchVectorBuildRunner{
		Pending:        pending,
		Builder:        builder,
		ReadyPublisher: publisher,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  testSearchVectorIdentity.ProviderProfileID,
			SourceClass:        testSearchVectorIdentity.SourceClass,
			EmbeddingModelID:   testSearchVectorIdentity.EmbeddingModelID,
			VectorIndexVersion: testSearchVectorIdentity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.PendingScopes)
	require.Equal(t, 0, publisher.calls)
}

type fakeSearchVectorBatchBuilder struct {
	fakeSearchVectorBuilder
	result        SearchVectorBuildResult
	err           error
	batchRequests [][]SearchVectorBuildRequest
}

func (f *fakeSearchVectorBatchBuilder) BuildSearchVectorsBatch(
	_ context.Context,
	reqs []SearchVectorBuildRequest,
) (SearchVectorBuildResult, error) {
	f.batchRequests = append(f.batchRequests, append([]SearchVectorBuildRequest(nil), reqs...))
	return f.result, f.err
}
