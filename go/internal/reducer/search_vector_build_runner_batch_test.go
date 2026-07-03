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
