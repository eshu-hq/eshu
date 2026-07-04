// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var testSearchVectorIdentity = SearchVectorBuildIdentity{
	ProviderProfileID:  "local",
	SourceClass:        "search_documents",
	EmbeddingModelID:   "local-hash-v1",
	VectorIndexVersion: "vector-v1",
}

// TestSearchVectorBuildRunnerPublishesReadyWhenNoPendingScopesRemain proves
// RunOnce publishes the search_vector_ready completion signal, keyed by the
// runner's vector identity, after a bounded sweep completes and a post-build
// re-check finds zero pending scopes, so the pending_search_vector freshness
// cause can clear (#4673).
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
			ProviderProfileID:  testSearchVectorIdentity.ProviderProfileID,
			SourceClass:        testSearchVectorIdentity.SourceClass,
			EmbeddingModelID:   testSearchVectorIdentity.EmbeddingModelID,
			VectorIndexVersion: testSearchVectorIdentity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 0, result.PendingScopes)
	require.Equal(t, 1, publisher.calls)
	require.Equal(t, []SearchVectorBuildIdentity{testSearchVectorIdentity}, publisher.identities)
}

// TestSearchVectorBuildRunnerDoesNotPublishReadyWithPendingScopes proves
// RunOnce does NOT publish search_vector_ready when the POST-build re-check
// still finds pending scopes, so the pending_search_vector cause is never
// cleared while work is outstanding.
func TestSearchVectorBuildRunnerDoesNotPublishReadyWithPendingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{
		scopes: []SearchVectorBuildPendingScope{
			{ScopeID: "scope-a", GenerationID: "gen-a"},
		},
		// The post-build re-check still finds a scope pending (e.g. more work
		// arrived, or the scope limit did not cover every pending scope).
		postBuildScopes: []SearchVectorBuildPendingScope{
			{ScopeID: "scope-b", GenerationID: "gen-b"},
		},
	}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{{DocumentCount: 1, VectorCount: 1}}}
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

// TestSearchVectorBuildRunnerPublishesReadyAfterDrainingLastPendingScopes
// proves RunOnce publishes search_vector_ready when the sweep drains the
// LAST pending scopes: the pre-build listing is non-empty (PendingScopes>0)
// but the POST-build re-check finds zero remaining. Gating on the pre-build
// count alone would have missed this exact case — the one the signal exists
// for (#4673 review fix, bug #3).
func TestSearchVectorBuildRunnerPublishesReadyAfterDrainingLastPendingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{
		scopes: []SearchVectorBuildPendingScope{
			{ScopeID: "scope-a", GenerationID: "gen-a"},
		},
		postBuildScopes: nil,
	}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{{DocumentCount: 1, VectorCount: 1}}}
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
	require.Equal(t, 1, publisher.calls, "post-build re-check found zero remaining, so ready must publish")
	require.Len(t, pending.requests, 2, "expected the pre-build listing plus the post-build re-check")
	require.Equal(t, 1, pending.requests[1].Limit, "the post-build re-check only needs to know whether ANY scope remains")
}

// TestSearchVectorBuildRunnerDoesNotPublishReadyOnBuildFailure proves a
// failed sweep never publishes ready — a failure is not "caught up". This
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
			ProviderProfileID:  testSearchVectorIdentity.ProviderProfileID,
			SourceClass:        testSearchVectorIdentity.SourceClass,
			EmbeddingModelID:   testSearchVectorIdentity.EmbeddingModelID,
			VectorIndexVersion: testSearchVectorIdentity.VectorIndexVersion,
		},
	}

	_, err := runner.RunOnce(context.Background())

	require.Error(t, err)
	require.Equal(t, 0, publisher.calls)
	require.Len(t, pending.requests, 1, "a failed build must not even issue the post-build re-check")
}

// TestSearchVectorBuildRunnerNilPublisherIsNoop proves RunOnce tolerates a nil
// ReadyPublisher (legacy/local wiring without the Postgres-backed watermark)
// without panicking, and skips the post-build re-check entirely (no extra
// Postgres round trip when nobody reads the signal).
func TestSearchVectorBuildRunnerNilPublisherIsNoop(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: nil}
	builder := &fakeSearchVectorBuilder{}
	runner := &SearchVectorBuildRunner{
		Pending: pending,
		Builder: builder,
		Config: SearchVectorBuildRunnerConfig{
			ProviderProfileID:  testSearchVectorIdentity.ProviderProfileID,
			SourceClass:        testSearchVectorIdentity.SourceClass,
			EmbeddingModelID:   testSearchVectorIdentity.EmbeddingModelID,
			VectorIndexVersion: testSearchVectorIdentity.VectorIndexVersion,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 0, result.PendingScopes)
	require.Len(t, pending.requests, 1, "no ReadyPublisher means no post-build re-check")
}

type fakeSearchVectorReadyPublisher struct {
	calls      int
	identities []SearchVectorBuildIdentity
	err        error
}

func (f *fakeSearchVectorReadyPublisher) PublishSearchVectorReady(_ context.Context, identity SearchVectorBuildIdentity) error {
	f.calls++
	f.identities = append(f.identities, identity)
	return f.err
}
