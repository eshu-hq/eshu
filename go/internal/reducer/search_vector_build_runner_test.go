package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSearchVectorBuildRunnerBuildsPendingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a", RepoID: "repo-a"},
		{ScopeID: "scope-b", GenerationID: "gen-b", RepoID: "repo-b"},
	}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{
		{DocumentCount: 2, VectorCount: 2},
		{DocumentCount: 1, VectorCount: 1},
	}}
	runner := &SearchVectorBuildRunner{
		Pending: pending,
		Builder: builder,
		Config: SearchVectorBuildRunnerConfig{
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
			ScopeLimit:         25,
			DocumentLimit:      50,
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, SearchVectorBuildRunnerResult{
		PendingScopes: 2,
		BuiltScopes:   2,
		DocumentCount: 3,
		VectorCount:   3,
	}, result)
	require.Equal(t, SearchVectorBuildPendingRequest{
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              25,
	}, pending.requests[0])
	require.Equal(t, SearchVectorBuildRequest{
		ScopeID:            "scope-a",
		RepoID:             "repo-a",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
		Limit:              50,
	}, builder.requests[0])
}

func TestSearchVectorBuildRunnerReturnsBuildFailuresAfterContinuingScopes(t *testing.T) {
	t.Parallel()

	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{
		{ScopeID: "scope-a", GenerationID: "gen-a"},
		{ScopeID: "scope-b", GenerationID: "gen-b"},
	}}
	builder := &fakeSearchVectorBuilder{
		results: []SearchVectorBuildResult{
			{DocumentCount: 1, FailedCount: 1},
			{DocumentCount: 1, VectorCount: 1},
		},
		errs: []error{errors.New("embed failed"), nil},
	}
	runner := &SearchVectorBuildRunner{
		Pending: pending,
		Builder: builder,
		Config: SearchVectorBuildRunnerConfig{
			EmbeddingModelID:   "local-hash-v1",
			VectorIndexVersion: "vector-v1",
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.ErrorContains(t, err, "scope-a")
	require.Equal(t, 2, result.BuiltScopes)
	require.Equal(t, 2, result.DocumentCount)
	require.Equal(t, 1, result.VectorCount)
	require.Equal(t, 1, result.FailedCount)
}

func TestSearchVectorBuildRunnerValidation(t *testing.T) {
	t.Parallel()

	_, err := (&SearchVectorBuildRunner{}).RunOnce(context.Background())

	require.ErrorContains(t, err, "search vector pending lister is required")
	require.ErrorContains(t, err, "search vector builder is required")
}

func TestServiceStartsSearchVectorBuildRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pending := &fakeSearchVectorPendingLister{scopes: []SearchVectorBuildPendingScope{{ScopeID: "scope-a"}}}
	builder := &fakeSearchVectorBuilder{results: []SearchVectorBuildResult{{DocumentCount: 1, VectorCount: 1}}}
	started := make(chan struct{}, 1)
	runner := &SearchVectorBuildRunner{
		Pending: pending,
		Builder: builder,
		Config: SearchVectorBuildRunnerConfig{
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

type fakeSearchVectorPendingLister struct {
	scopes   []SearchVectorBuildPendingScope
	err      error
	requests []SearchVectorBuildPendingRequest
}

func (f *fakeSearchVectorPendingLister) ListPendingSearchVectorScopes(
	_ context.Context,
	req SearchVectorBuildPendingRequest,
) ([]SearchVectorBuildPendingScope, error) {
	f.requests = append(f.requests, req)
	scopes := f.scopes
	f.scopes = nil
	return scopes, f.err
}

type fakeSearchVectorBuilder struct {
	mu       sync.Mutex
	requests []SearchVectorBuildRequest
	results  []SearchVectorBuildResult
	errs     []error
}

func (f *fakeSearchVectorBuilder) BuildSearchVectors(
	_ context.Context,
	req SearchVectorBuildRequest,
) (SearchVectorBuildResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	var result SearchVectorBuildResult
	if len(f.results) > 0 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	var err error
	if len(f.errs) > 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	return result, err
}

func (f *fakeSearchVectorBuilder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}
