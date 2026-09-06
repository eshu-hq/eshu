// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eshu-hq/eshu/go/internal/reducer/searchvector"
)

// TestServiceStartsSearchVectorBuildRunner proves Service.startSideRunners
// starts the [searchvector.SearchVectorBuildRunner] goroutine — the runner
// moved to [searchvector] in #6061 but Service.Run's side-runner startup
// stays a root concern. The runner-behavior tests that used to live beside
// this one moved to
// go/internal/reducer/searchvector/search_vector_build_runner_test.go; only
// the wiring proof — that Service actually launches this specific runner —
// belongs at root.
func TestServiceStartsSearchVectorBuildRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pending := &fakeRootSearchVectorPendingLister{scopes: []searchvector.SearchVectorBuildPendingScope{{ScopeID: "scope-a"}}}
	builder := &fakeRootSearchVectorBuilder{results: []searchvector.SearchVectorBuildResult{{DocumentCount: 1, VectorCount: 1}}}
	started := make(chan struct{}, 1)
	runner := &searchvector.SearchVectorBuildRunner{
		Pending: pending,
		Builder: builder,
		Config: searchvector.SearchVectorBuildRunnerConfig{
			ProviderProfileID:  "local",
			SourceClass:        "search_documents",
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
		Wait: func(ctx context.Context, _ time.Duration) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{SearchVectorBuildRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})

	require.Eventually(t, func() bool {
		return builder.callCount() == 1
	}, time.Second, 10*time.Millisecond)
	<-started
	cancel()
	wg.Wait()

	require.NoError(t, gotErr)
}

// fakeRootSearchVectorPendingLister is a minimal single-use double for
// searchvector.SearchVectorBuildPendingLister, scoped to this file's one
// wiring test. The
// full-featured fake used by the runner's own behavior tests lives beside
// them in searchvector, unexported and out of this package's reach.
type fakeRootSearchVectorPendingLister struct {
	mu     sync.Mutex
	scopes []searchvector.SearchVectorBuildPendingScope
}

func (f *fakeRootSearchVectorPendingLister) ListPendingSearchVectorScopes(
	_ context.Context,
	_ searchvector.SearchVectorBuildPendingRequest,
) ([]searchvector.SearchVectorBuildPendingScope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	scopes := f.scopes
	f.scopes = nil
	return scopes, nil
}

// fakeRootSearchVectorBuilder is a minimal single-use double for
// searchvector.SearchVectorBuilder, scoped to this file's one wiring test.
type fakeRootSearchVectorBuilder struct {
	mu      sync.Mutex
	results []searchvector.SearchVectorBuildResult
	calls   int
}

func (f *fakeRootSearchVectorBuilder) BuildSearchVectors(
	_ context.Context,
	_ searchvector.SearchVectorBuildRequest,
) (searchvector.SearchVectorBuildResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var result searchvector.SearchVectorBuildResult
	if len(f.results) > 0 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	return result, nil
}

func (f *fakeRootSearchVectorBuilder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
