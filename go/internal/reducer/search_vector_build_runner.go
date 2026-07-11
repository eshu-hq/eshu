// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultSearchVectorBuildPollInterval  = 30 * time.Second
	defaultSearchVectorBuildScopeLimit    = 100
	defaultSearchVectorBuildDocumentLimit = 500
	searchVectorBuildTailDocumentBudget   = 50000
)

// DomainSearchVectorBuild tags the search-vector build sweep's split-timing
// histogram (#4430). The sweep is a reducer-tail sidecar, not a
// fact-projecting reducer domain, so it has no corresponding Domain used for
// intent dispatch.
const DomainSearchVectorBuild Domain = "search_vector_build"

// Search vector build sweep phases, recorded on
// telemetry.Instruments.SearchVectorBuildPhaseDuration via
// telemetry.AttrWritePhase. Bounded, closed set of four phases.
const (
	SearchVectorBuildPhaseSchedulingWait = "scheduling_wait"
	SearchVectorBuildPhaseQueryLoad      = "query_load"
	SearchVectorBuildPhaseEmbedBuild     = "embed_build"
	SearchVectorBuildPhaseWriteUpsert    = "write_upsert"
)

// SearchVectorBuildPendingScope identifies one active scope that needs
// vector rows for its curated search documents.
type SearchVectorBuildPendingScope struct {
	ScopeID        string
	GenerationID   string
	RepoID         string
	DocumentCursor string
	// ProjectionRevision is the document-projection revision observed when the
	// scope was listed as pending (#4233). The build runner uses it to finalize
	// vector readiness with a revision/fence CAS.
	ProjectionRevision int64
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
	GenerationID       string
	RepoID             string
	AfterDocumentID    string
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
	// ProjectionRevision flows from the pending listing through the build runner
	// so the vector scope state manager can CAS-finalize by revision (#4233).
	ProjectionRevision int64
	// BuildFence is the vector-scope fence returned immediately before this
	// build. Postgres batch upserts reject rows whose fence is no longer current.
	BuildFence int64
}

// SearchVectorBuildResult summarizes a vector build attempt.
type SearchVectorBuildResult struct {
	DocumentCount int
	VectorCount   int
	DisabledCount int
	FailedCount   int
	ScopeProgress []SearchVectorBuildScopeProgress
	// QueryLoadDuration is time spent listing active search documents.
	QueryLoadDuration time.Duration
	// EmbedBuildDuration is time spent embedding document text.
	EmbedBuildDuration time.Duration
	// WriteUpsertDuration is time spent in batched metadata/value upserts.
	WriteUpsertDuration time.Duration
}

// SearchVectorBuilder runs one bounded vector build for a scope.
type SearchVectorBuilder interface {
	BuildSearchVectors(context.Context, SearchVectorBuildRequest) (SearchVectorBuildResult, error)
}

// SearchVectorBatchBuilder builds vector rows for several scopes with one
// batched document-selection path. Implementations must preserve the same
// per-scope idempotency and active-generation semantics as SearchVectorBuilder.
type SearchVectorBatchBuilder interface {
	BuildSearchVectorsBatch(context.Context, []SearchVectorBuildRequest) (SearchVectorBuildResult, error)
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

func (c SearchVectorBuildRunnerConfig) batchDocumentLimit(scopeCount int) int {
	base := c.documentLimit()
	if base != defaultSearchVectorBuildDocumentLimit || scopeCount <= 0 {
		return base
	}
	limit := searchVectorBuildTailDocumentBudget / scopeCount
	if limit < base {
		return base
	}
	return limit
}

// SearchVectorBuildIdentity names the vector-identity tuple a search_vector_ready
// signal is scoped to: the same (provider profile, source class, embedding
// model, vector index version) tuple ListPendingSearchVectorScopes and the
// builder ports key their work by. A ready publish for one identity tuple
// MUST NOT satisfy freshness for a different tuple — otherwise a provider,
// model, or index-version rollout (or two reducer/API configs sharing one
// Postgres) would serve stale-under-new-config as fresh.
type SearchVectorBuildIdentity struct {
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
}

// SearchVectorBuildReadyPublisher publishes the search_vector_ready
// completion signal, keyed by identity, once a bounded sweep finds zero
// pending scopes for that identity, so a downstream freshness evaluator
// (go/internal/query's pending_search_vector FreshnessCause) can clear the
// cause for that same identity. Implementations persist a maintainer
// watermark (mirroring the supply_chain_impact_winners_materialization
// pattern) so the signal survives across the reducer and query/API process
// boundary.
type SearchVectorBuildReadyPublisher interface {
	PublishSearchVectorReady(ctx context.Context, identity SearchVectorBuildIdentity) error
}

// SearchVectorScopeStateManager owns the vector-scope-state lifecycle:
// BeginBuilding before build, ScopeVectorComplete to check per-scope readiness,
// and FinalizeReady to CAS-publish the ready state. Nil disables the lifecycle
// (keeps legacy/local wiring byte-identical). Define this consumer interface
// in the reducer package so the reducer never imports storage/postgres.
type SearchVectorScopeStateManager interface {
	BeginBuilding(ctx context.Context, scopeID, generationID string, identity SearchVectorBuildIdentity, projectionRevision int64) (fence int64, err error)
	AdvanceDocumentCursor(ctx context.Context, scopeID, generationID string, identity SearchVectorBuildIdentity, projectionRevision, fence int64, documentID string) (bool, error)
	ResetDocumentCursor(ctx context.Context, scopeID, generationID string, identity SearchVectorBuildIdentity, projectionRevision, fence int64) (bool, error)
	ScopeVectorComplete(ctx context.Context, scopeID, generationID string, identity SearchVectorBuildIdentity) (bool, error)
	FinalizeReady(ctx context.Context, scopeID, generationID string, identity SearchVectorBuildIdentity, projectionRevision, fence int64) (bool, error)
}

// SearchVectorBuildRunner builds derived vector rows beside normal
// reducer work. It writes no graph truth and relies on the vector stores'
// deterministic upsert identity for duplicate/replayed work convergence.
type SearchVectorBuildRunner struct {
	Pending     SearchVectorBuildPendingLister
	Builder     SearchVectorBuilder
	Config      SearchVectorBuildRunnerConfig
	Wait        func(context.Context, time.Duration) error
	Logger      *slog.Logger
	Instruments *telemetry.Instruments
	// ReadyPublisher optionally publishes search_vector_ready when a bounded
	// sweep completes with zero pending scopes. Nil disables the signal
	// (legacy/local wiring without the Postgres-backed watermark) without
	// affecting build behavior.
	ReadyPublisher SearchVectorBuildReadyPublisher
	// ScopeState optionally wires the #4233 per-scope vector-scope-state
	// lifecycle: BeginBuilding before build, ScopeVectorComplete to gate
	// readiness, and FinalizeReady to CAS-publish. Nil disables it, keeping
	// legacy/local wiring byte-identical.
	ScopeState SearchVectorScopeStateManager
}

// SearchVectorBuildRunnerResult summarizes one bounded sweep.
type SearchVectorBuildRunnerResult struct {
	PendingScopes int
	BuiltScopes   int
	// FinalizedScopes counts successful vector-scope readiness CAS writes.
	// It is durable progress even when an upgrade sweep finds all vector rows
	// already present and therefore embeds no documents.
	FinalizedScopes int
	DocumentCount   int
	VectorCount     int
	DisabledCount   int
	FailedCount     int
	// QueryLoadDuration sums per-scope active-document listing time.
	QueryLoadDuration time.Duration
	// EmbedBuildDuration sums per-scope document-embedding time.
	EmbedBuildDuration time.Duration
	// WriteUpsertDuration sums per-scope batched metadata/value upsert time.
	WriteUpsertDuration time.Duration
	// SchedulingWaitDuration is time this sweep spent listing pending scopes
	// before any per-scope build work started.
	SchedulingWaitDuration time.Duration
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
			if !searchVectorBuildSweepMadeProgress(result) {
				// Pending scopes remain but the sweep produced no durable
				// output (no finalized scopes, documents, vectors, or disabled
				// rows). Re-looping
				// immediately would hot-loop on a never-draining pending set
				// (#4885) and pin Postgres with useless query load. Back off on
				// the poll interval and surface the stall so an operator can
				// investigate the pending/builder mismatch instead of spinning.
				r.logNoProgress(ctx, result)
				if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
					if searchVectorBuildContextDone(ctx, waitErr) {
						return nil
					}
					return fmt.Errorf("wait after no-progress search vector build sweep: %w", waitErr)
				}
			}
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
	schedulingStart := time.Now()
	scopes, err := r.Pending.ListPendingSearchVectorScopes(ctx, SearchVectorBuildPendingRequest{
		ProviderProfileID:  r.Config.ProviderProfileID,
		SourceClass:        r.Config.SourceClass,
		EmbeddingModelID:   r.Config.EmbeddingModelID,
		VectorIndexVersion: r.Config.VectorIndexVersion,
		Limit:              r.Config.scopeLimit(),
	})
	schedulingWait := time.Since(schedulingStart)
	if err != nil {
		return SearchVectorBuildRunnerResult{}, fmt.Errorf("list pending search vector scopes: %w", err)
	}
	result := SearchVectorBuildRunnerResult{PendingScopes: len(scopes), SchedulingWaitDuration: schedulingWait}

	// #4233: scope-state lifecycle — BeginBuilding before any mutation.
	identity := SearchVectorBuildIdentity{
		ProviderProfileID:  r.Config.ProviderProfileID,
		SourceClass:        r.Config.SourceClass,
		EmbeddingModelID:   r.Config.EmbeddingModelID,
		VectorIndexVersion: r.Config.VectorIndexVersion,
	}
	fences := make(map[string]int64, len(scopes))
	if r.ScopeState != nil && len(scopes) > 0 {
		for _, scope := range scopes {
			fence, err := r.ScopeState.BeginBuilding(ctx, scope.ScopeID, scope.GenerationID, identity, scope.ProjectionRevision)
			if err != nil {
				return result, fmt.Errorf("begin building vector scope state for scope %q generation %q: %w", scope.ScopeID, scope.GenerationID, err)
			}
			fences[vectorScopeStateKey(scope.ScopeID, scope.GenerationID)] = fence
		}
	}

	if len(scopes) > 0 {
		if batchBuilder, ok := r.Builder.(SearchVectorBatchBuilder); ok {
			build, err := batchBuilder.BuildSearchVectorsBatch(ctx, r.buildRequests(scopes, r.Config.batchDocumentLimit(len(scopes)), fences))
			result.BuiltScopes = len(scopes)
			result.DocumentCount += build.DocumentCount
			result.VectorCount += build.VectorCount
			result.DisabledCount += build.DisabledCount
			result.FailedCount += build.FailedCount
			result.QueryLoadDuration += build.QueryLoadDuration
			result.EmbedBuildDuration += build.EmbedBuildDuration
			result.WriteUpsertDuration += build.WriteUpsertDuration
			if err != nil {
				r.logResult(ctx, result, started)
				r.recordPhaseMetrics(ctx, result)
				return result, fmt.Errorf("build search vectors for %d scopes: %w", len(scopes), err)
			}
			// #4233: after build, per-scope completeness check + CAS-publish.
			result.FinalizedScopes = r.finalizeVectorScopeStates(ctx, scopes, identity, fences, build.ScopeProgress, r.Config.batchDocumentLimit(len(scopes)))
			r.logResult(ctx, result, started)
			r.recordPhaseMetrics(ctx, result)
			r.publishReadyIfCaughtUp(ctx)
			return result, nil
		}
	}
	var failures []error
	var progress []SearchVectorBuildScopeProgress
	for _, pending := range scopes {
		build, err := r.Builder.BuildSearchVectors(ctx, SearchVectorBuildRequest{
			ScopeID:            pending.ScopeID,
			GenerationID:       pending.GenerationID,
			RepoID:             pending.RepoID,
			AfterDocumentID:    pending.DocumentCursor,
			ProviderProfileID:  r.Config.ProviderProfileID,
			SourceClass:        r.Config.SourceClass,
			EmbeddingModelID:   r.Config.EmbeddingModelID,
			VectorIndexVersion: r.Config.VectorIndexVersion,
			Limit:              r.Config.documentLimit(),
			ProjectionRevision: pending.ProjectionRevision,
			BuildFence:         fences[vectorScopeStateKey(pending.ScopeID, pending.GenerationID)],
		})
		result.BuiltScopes++
		result.DocumentCount += build.DocumentCount
		result.VectorCount += build.VectorCount
		result.DisabledCount += build.DisabledCount
		result.FailedCount += build.FailedCount
		result.QueryLoadDuration += build.QueryLoadDuration
		result.EmbedBuildDuration += build.EmbedBuildDuration
		result.WriteUpsertDuration += build.WriteUpsertDuration
		if err != nil {
			failures = append(failures, fmt.Errorf("build search vectors for scope %q generation %q: %w", pending.ScopeID, pending.GenerationID, err))
		} else {
			progress = append(progress, build.ScopeProgress...)
		}
	}
	// #4233: after build, per-scope completeness check + CAS-publish (serial path).
	result.FinalizedScopes = r.finalizeVectorScopeStates(ctx, scopes, identity, fences, progress, r.Config.documentLimit())
	r.logResult(ctx, result, started)
	r.recordPhaseMetrics(ctx, result)
	buildErr := errors.Join(failures...)
	if buildErr == nil {
		r.publishReadyIfCaughtUp(ctx)
	}
	return result, buildErr
}

// publishReadyIfCaughtUp re-checks pending scopes AFTER the build (not the
// pre-build listing this sweep started with) and publishes search_vector_ready
// only when that post-build check finds zero pending scopes for the runner's
// vector-identity tuple. Gating on the post-build state (rather than the
// pre-build PendingScopes count) is required because a sweep that drains the
// LAST pending scopes has a non-zero pre-build count but a truly caught-up
// post-build state — the exact case the signal exists for (#4673 review
// fix). It is reached from both the batch-builder fast path and the serial
// per-scope path so production (which uses the batch path) actually
// publishes. A nil ReadyPublisher skips the check entirely (no extra
// Postgres round trip when nobody reads the signal). A re-check probe error
// or any remaining pending scope skips publish without treating the sweep
// itself as failed. A publish failure is logged, not returned, mirroring the
// maintainer-resweep pattern elsewhere (the sweep itself succeeded; only the
// completion signal failed to persist) so a transient watermark-write error
// does not turn a successful bounded sweep into a reported failure.
func (r *SearchVectorBuildRunner) publishReadyIfCaughtUp(ctx context.Context) {
	if r.ReadyPublisher == nil {
		return
	}
	remaining, err := r.Pending.ListPendingSearchVectorScopes(ctx, SearchVectorBuildPendingRequest{
		ProviderProfileID:  r.Config.ProviderProfileID,
		SourceClass:        r.Config.SourceClass,
		EmbeddingModelID:   r.Config.EmbeddingModelID,
		VectorIndexVersion: r.Config.VectorIndexVersion,
		Limit:              1,
	})
	if err != nil {
		r.logPublishFailure(ctx, fmt.Errorf("re-check pending search vector scopes: %w", err))
		return
	}
	if len(remaining) > 0 {
		return
	}
	identity := SearchVectorBuildIdentity{
		ProviderProfileID:  r.Config.ProviderProfileID,
		SourceClass:        r.Config.SourceClass,
		EmbeddingModelID:   r.Config.EmbeddingModelID,
		VectorIndexVersion: r.Config.VectorIndexVersion,
	}
	if err := r.ReadyPublisher.PublishSearchVectorReady(ctx, identity); err != nil {
		r.logPublishFailure(ctx, err)
	}
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

// searchVectorBuildSweepMadeProgress reports whether a bounded sweep produced
// any durable change: at least one finalized scope, built document, vector, or
// disabled-marked row. A sweep that selected pending scopes but produced none
// of these changed nothing, so re-running it immediately would hot-loop on a
// never-draining pending set (#4885). Failures are handled by the RunOnce error
// path, so this only distinguishes a silent no-op sweep from a productive one.
func searchVectorBuildSweepMadeProgress(result SearchVectorBuildRunnerResult) bool {
	return result.FinalizedScopes > 0 || result.DocumentCount > 0 || result.VectorCount > 0 || result.DisabledCount > 0
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
