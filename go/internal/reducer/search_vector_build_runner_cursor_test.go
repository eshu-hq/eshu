// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchVectorBuildRunnerWrapsEmptyExhaustedCursorPage(t *testing.T) {
	t.Parallel()

	scopeState := &fakeSearchVectorScopeStateManager{completeResults: []bool{false}}
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{{
		ScopeID: "scope-a", GenerationID: "gen-a", ProjectionRevision: 7,
		DocumentCursor: "doc-last",
	}}}
	builder := &fakeSearchVectorBatchBuilder{result: SearchVectorBuildResult{
		ScopeProgress: []SearchVectorBuildScopeProgress{{
			ScopeID: "scope-a", GenerationID: "gen-a",
		}},
	}}
	runner := &SearchVectorBuildRunner{
		Pending: pending, Builder: builder, ScopeState: scopeState,
		Config: SearchVectorBuildRunnerConfig{
			DocumentLimit: 50, ProviderProfileID: "local",
			SourceClass: "search_documents", EmbeddingModelID: "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	}

	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, scopeState.advanceCalls)
	require.Len(t, scopeState.resetCalls, 1, "empty page at keyspace end must wrap to find stale rows behind the cursor")
}
