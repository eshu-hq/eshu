// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// logResult emits the operator-facing "search vector build sweep completed"
// log line for one bounded sweep, including the #4430 split-timing fields.
func (r *SearchVectorBuildRunner) logResult(ctx context.Context, result SearchVectorBuildRunnerResult, started time.Time) {
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx, "search vector build sweep completed",
		slog.Int("pending_scopes", result.PendingScopes),
		slog.Int("built_scopes", result.BuiltScopes),
		slog.Int("finalized_scopes", result.FinalizedScopes),
		slog.Int("document_count", result.DocumentCount),
		slog.Int("vector_count", result.VectorCount),
		slog.Int("disabled_count", result.DisabledCount),
		slog.Int("failed_count", result.FailedCount),
		slog.String("provider_profile_id", r.Config.ProviderProfileID),
		slog.String("source_class", r.Config.SourceClass),
		slog.String("embedding_model_id", r.Config.EmbeddingModelID),
		slog.String("vector_index_version", r.Config.VectorIndexVersion),
		slog.Float64("duration_seconds", time.Since(started).Seconds()),
		// Split timing (#4430): isolates the search-vector sweep cost into
		// scheduling (pending-scope selection), query/load (active document
		// listing), embed/build (embedding compute), and write/upsert
		// (batched metadata+value persistence) so the dominant slice of the
		// reducer-tail sweep cost is visible without re-deriving it from the
		// single aggregate duration_seconds field.
		slog.Float64("scheduling_wait_seconds", result.SchedulingWaitDuration.Seconds()),
		slog.Float64("query_load_seconds", result.QueryLoadDuration.Seconds()),
		slog.Float64("embed_build_seconds", result.EmbedBuildDuration.Seconds()),
		slog.Float64("write_upsert_seconds", result.WriteUpsertDuration.Seconds()),
		slog.String("phase", "reduction"),
	)
}

// recordPhaseMetrics emits eshu_dp_search_vector_build_phase_seconds for each
// of the four bounded sweep phases so an operator can isolate the dominant
// slice of the search-vector sweep cost without recomputing it from logs
// (#4430).
func (r *SearchVectorBuildRunner) recordPhaseMetrics(ctx context.Context, result SearchVectorBuildRunnerResult) {
	if r.Instruments == nil || r.Instruments.SearchVectorBuildPhaseDuration == nil {
		return
	}
	record := func(phase string, d time.Duration) {
		r.Instruments.SearchVectorBuildPhaseDuration.Record(ctx, d.Seconds(), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainSearchVectorBuild)),
			telemetry.AttrWritePhase(phase),
		))
	}
	record(SearchVectorBuildPhaseSchedulingWait, result.SchedulingWaitDuration)
	record(SearchVectorBuildPhaseQueryLoad, result.QueryLoadDuration)
	record(SearchVectorBuildPhaseEmbedBuild, result.EmbedBuildDuration)
	record(SearchVectorBuildPhaseWriteUpsert, result.WriteUpsertDuration)
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

// logPublishFailure records a failed search_vector_ready publish attempt. The
// bounded sweep itself already succeeded (zero pending scopes); only the
// completion-signal write failed, so this is logged as a distinct failure
// class rather than surfaced as a RunOnce error.
func (r *SearchVectorBuildRunner) logPublishFailure(ctx context.Context, err error) {
	if r.Logger == nil {
		return
	}
	r.Logger.ErrorContext(
		ctx, "search vector build ready signal publish failed",
		log.Err(err),
		log.FailureClass("search_vector_ready_publish_error"),
		slog.String("phase", "reduction"),
	)
}

// logNoProgress warns that a bounded sweep selected pending scopes but produced
// no durable output, so the runner is backing off instead of hot-looping. It
// reuses the sweep's existing telemetry fields; no new metric instrument is
// introduced. Operators can alert on this WARN (stall_reason=no_durable_output)
// or on the existing completion log showing a non-zero pending_scopes with a
// zero finalized_scopes/document_count/vector_count.
func (r *SearchVectorBuildRunner) logNoProgress(ctx context.Context, result SearchVectorBuildRunnerResult) {
	if r.Logger == nil {
		return
	}
	r.Logger.WarnContext(
		ctx, "search vector build sweep made no progress; backing off",
		slog.Int("pending_scopes", result.PendingScopes),
		slog.Int("built_scopes", result.BuiltScopes),
		slog.Int("finalized_scopes", result.FinalizedScopes),
		slog.Int("document_count", result.DocumentCount),
		slog.Int("vector_count", result.VectorCount),
		slog.Int("disabled_count", result.DisabledCount),
		slog.Int("failed_count", result.FailedCount),
		slog.String("provider_profile_id", r.Config.ProviderProfileID),
		slog.String("source_class", r.Config.SourceClass),
		slog.String("embedding_model_id", r.Config.EmbeddingModelID),
		slog.String("vector_index_version", r.Config.VectorIndexVersion),
		slog.String("stall_reason", "no_durable_output"),
		slog.String("phase", "reduction"),
	)
}
