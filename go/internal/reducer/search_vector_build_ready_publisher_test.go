// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSearchVectorBuildRunnerPublishesReadyWhenNoPendingScopesRemain proves
// RunOnce publishes the search_vector_ready completion signal exactly once,
// after a bounded sweep completes with zero pending scopes, so the
// pending_search_vector freshness cause can clear (#4673).
func TestSearchVectorBuildRunnerPublishesReadyWhenNoPendingScopesRemain(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: nil}
	builder := &fakeSearchVectorBuilder{}
	publisher := &fakeSearchVectorReadyPublisher{}
	runner := &SearchVectorBuildRunner{
		Pending:        pending,
		Builder:        builder,
		ReadyPublisher: publisher,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 0, result.PendingScopes)
	require.Equal(t, 1, publisher.calls)
}

// TestSearchVectorBuildRunnerDoesNotPublishReadyWithPendingScopes proves
// RunOnce does NOT publish search_vector_ready while pending scopes remain,
// so the pending_search_vector cause is never cleared while work is
// outstanding.
func TestSearchVectorBuildRunnerDoesNotPublishReadyWithPendingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a"},
	}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{{DocumentCount: 1, VectorCount: 1}}}
	publisher := &fakeSearchVectorReadyPublisher{}
	runner := &SearchVectorBuildRunner{
		Pending:        pending,
		Builder:        builder,
		ReadyPublisher: publisher,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.PendingScopes)
	require.Equal(t, 0, publisher.calls)
}

// TestSearchVectorBuildRunnerDoesNotPublishReadyOnBuildFailure proves a
// failed sweep (even with zero pending scopes reported by the batch builder
// error path) never publishes ready — a failure is not "caught up". This
// guards against ever mis-reporting readiness on the same bounded sweep that
// returned an error.
func TestSearchVectorBuildRunnerDoesNotPublishReadyOnBuildFailure(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a"},
	}}
	builder := &fakeSearchVectorBuilder{
		results: []SearchVectorBuildResult{{DocumentCount: 1, FailedCount: 1}},
		errs:    []error{errors.New("embed failed")},
	}
	publisher := &fakeSearchVectorReadyPublisher{}
	runner := &SearchVectorBuildRunner{
		Pending:        pending,
		Builder:        builder,
		ReadyPublisher: publisher,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	}

	_, err := runner.RunOnce(context.Background())

	require.Error(t, err)
	require.Equal(t, 0, publisher.calls)
}

// TestSearchVectorBuildRunnerNilPublisherIsNoop proves RunOnce tolerates a nil
// ReadyPublisher (legacy/local wiring without the Postgres-backed watermark)
// without panicking.
func TestSearchVectorBuildRunnerNilPublisherIsNoop(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: nil}
	builder := &fakeSearchVectorBuilder{}
	runner := &SearchVectorBuildRunner{
		Pending: pending,
		Builder: builder,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 0, result.PendingScopes)
}

type fakeSearchVectorReadyPublisher struct {
	calls int
	err   error
}

func (f *fakeSearchVectorReadyPublisher) PublishSearchVectorReady(context.Context) error {
	f.calls++
	return f.err
}
