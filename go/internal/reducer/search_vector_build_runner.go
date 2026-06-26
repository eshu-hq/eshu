// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultSearchVectorBuildPollInterval  = 30 * time.Second
	defaultSearchVectorBuildScopeLimit    = 100
	defaultSearchVectorBuildDocumentLimit = 500
)

// SearchVectorBuildPendingScope identifies one active scope that needs
// vector rows for its curated search documents.
type SearchVectorBuildPendingScope struct {
	ScopeID      string
	GenerationID string
	RepoID       string
}

// SearchVectorBuildPendingRequest bounds pending vector build discovery.
type SearchVectorBuildPendingRequest struct {
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
}

// SearchVectorBuildPendingLister lists active scopes whose curated search
// documents do not yet have complete ready vector rows.
type SearchVectorBuildPendingLister interface {
	ListPendingSearchVectorScopes(context.Context, SearchVectorBuildPendingRequest) ([]SearchVectorBuildPendingScope, error)
}

// SearchVectorBuildRequest identifies one bounded vector build for a
// scope. The cmd/runtime layer adapts this port to the concrete searchvector
// builder to keep reducer free of storage package dependencies.
type SearchVectorBuildRequest struct {
	ScopeID            string
	RepoID             string
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
}

// SearchVectorBuildResult summarizes a vector build attempt.
type SearchVectorBuildResult struct {
	DocumentCount int
	VectorCount   int
	DisabledCount int
	FailedCount   int
}

// SearchVectorBuilder runs one bounded vector build for a scope.
type SearchVectorBuilder interface {
	BuildSearchVectors(context.Context, SearchVectorBuildRequest) (SearchVectorBuildResult, error)
}

// SearchVectorBuildRunnerConfig configures the reducer sidecar that builds
// derived vector rows for the semantic/hybrid search read path.
type SearchVectorBuildRunnerConfig struct {
	PollInterval       time.Duration
	ScopeLimit         int
	DocumentLimit      int
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

func (c SearchVectorBuildRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultSearchVectorBuildPollInterval
	}
	return c.PollInterval
}

func (c SearchVectorBuildRunnerConfig) scopeLimit() int {
	if c.ScopeLimit <= 0 {
		return defaultSearchVectorBuildScopeLimit
	}
	return c.ScopeLimit
}

func (c SearchVectorBuildRunnerConfig) documentLimit() int {
	if c.DocumentLimit <= 0 {
		return defaultSearchVectorBuildDocumentLimit
	}
	return c.DocumentLimit
}

// SearchVectorBuildRunner builds derived vector rows beside normal
// reducer work. It writes no graph truth and relies on the vector stores'
// deterministic upsert identity for duplicate/replayed work convergence.
type SearchVectorBuildRunner struct {
	Pending SearchVectorBuildPendingLister
	Builder SearchVectorBuilder
	Config  SearchVectorBuildRunnerConfig
	Wait    func(context.Context, time.Duration) error
	Logger  *slog.Logger
}

// SearchVectorBuildRunnerResult summarizes one bounded sweep.
type SearchVectorBuildRunnerResult struct {
	PendingScopes int
	BuiltScopes   int
	DocumentCount int
	VectorCount   int
	DisabledCount int
	FailedCount   int
}

// Run sweeps for pending vector builds until the context is canceled.
func (r *SearchVectorBuildRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := r.RunOnce(ctx)
		if err != nil {
			r.logFailure(ctx, err)
			if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
				if searchVectorBuildContextDone(ctx, waitErr) {
					return nil
				}
				return fmt.Errorf("wait for search vector build retry: %w", waitErr)
			}
			continue
		}
		if result.PendingScopes > 0 {
			continue
		}
		if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
			if searchVectorBuildContextDone(ctx, waitErr) {
				return nil
			}
			return fmt.Errorf("wait for search vector build work: %w", waitErr)
		}
	}
}

// RunOnce builds vectors for one bounded batch of pending active scopes.
func (r *SearchVectorBuildRunner) RunOnce(ctx context.Context) (SearchVectorBuildRunnerResult, error) {
	if err := r.validate(); err != nil {
		return SearchVectorBuildRunnerResult{}, err
	}
	started := time.Now()
	scopes, err := r.Pending.ListPendingSearchVectorScopes(ctx, SearchVectorBuildPendingRequest{
		ProviderProfileID:  r.Config.ProviderProfileID,
		SourceClass:        r.Config.SourceClass,
		EmbeddingModelID:   r.Config.EmbeddingModelID,
		VectorIndexVersion: r.Config.VectorIndexVersion,
		Limit:              r.Config.scopeLimit(),
	})
	if err != nil {
		return SearchVectorBuildRunnerResult{}, fmt.Errorf("list pending search vector scopes: %w", err)
	}
	result := SearchVectorBuildRunnerResult{PendingScopes: len(scopes)}
	var failures []error
	for _, pending := range scopes {
		build, err := r.Builder.BuildSearchVectors(ctx, SearchVectorBuildRequest{
			ScopeID:            pending.ScopeID,
			RepoID:             pending.RepoID,
			ProviderProfileID:  r.Config.ProviderProfileID,
			SourceClass:        r.Config.SourceClass,
			EmbeddingModelID:   r.Config.EmbeddingModelID,
			VectorIndexVersion: r.Config.VectorIndexVersion,
			Limit:              r.Config.documentLimit(),
		})
		result.BuiltScopes++
		result.DocumentCount += build.DocumentCount
		result.VectorCount += build.VectorCount
		result.DisabledCount += build.DisabledCount
		result.FailedCount += build.FailedCount
		if err != nil {
			failures = append(failures, fmt.Errorf("build search vectors for scope %q generation %q: %w", pending.ScopeID, pending.GenerationID, err))
		}
	}
	r.logResult(ctx, result, started)
	return result, errors.Join(failures...)
}

func (r *SearchVectorBuildRunner) validate() error {
	var problems []error
	if r.Pending == nil {
		problems = append(problems, errors.New("search vector pending lister is required"))
	}
	if r.Builder == nil {
		problems = append(problems, errors.New("search vector builder is required"))
	}
	if r.Config.EmbeddingModelID == "" {
		problems = append(problems, errors.New("search vector embedding model id is required"))
	}
	if r.Config.ProviderProfileID == "" {
		problems = append(problems, errors.New("search vector provider profile id is required"))
	}
	if r.Config.SourceClass == "" {
		problems = append(problems, errors.New("search vector source class is required"))
	}
	if r.Config.VectorIndexVersion == "" {
		problems = append(problems, errors.New("search vector index version is required"))
	}
	return errors.Join(problems...)
}

func (r *SearchVectorBuildRunner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	return searchVectorBuildSleep(ctx, d)
}

func (r *SearchVectorBuildRunner) logResult(ctx context.Context, result SearchVectorBuildRunnerResult, started time.Time) {
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx, "search vector build sweep completed",
		slog.Int("pending_scopes", result.PendingScopes),
		slog.Int("built_scopes", result.BuiltScopes),
		slog.Int("document_count", result.DocumentCount),
		slog.Int("vector_count", result.VectorCount),
		slog.Int("disabled_count", result.DisabledCount),
		slog.Int("failed_count", result.FailedCount),
		slog.String("provider_profile_id", r.Config.ProviderProfileID),
		slog.String("source_class", r.Config.SourceClass),
		slog.String("embedding_model_id", r.Config.EmbeddingModelID),
		slog.String("vector_index_version", r.Config.VectorIndexVersion),
		slog.Float64("duration_seconds", time.Since(started).Seconds()),
		slog.String("phase", "reduction"),
	)
}

func (r *SearchVectorBuildRunner) logFailure(ctx context.Context, err error) {
	if r.Logger == nil {
		return
	}
	r.Logger.ErrorContext(
		ctx, "search vector build sweep failed",
		log.Err(err),
		log.FailureClass("search_vector_build_error"),
		slog.String("phase", "reduction"),
	)
}

func searchVectorBuildContextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil
}

func searchVectorBuildSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
