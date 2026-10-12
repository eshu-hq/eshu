// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// deploymentArtifactsReadDegradedReason names a failed content or graph read
// of the deployment-artifact stage (#7126). The stage still answers with the
// artifacts from the loads that succeeded, as it always has, and lacks the
// artifacts of the load that failed; until this reason existed the failure was
// dropped, so a partial answer looked complete.
// Story responses carry it in limitations and context responses in
// partial_reasons, the same fields the other degraded reads use.
const deploymentArtifactsReadDegradedReason = "deployment_artifacts_read_degraded"

// DeploymentArtifactsReadCountAttrs are the read counts of one deployment
// artifact load, as stage-log attributes. All values are counts, seconds or
// booleans; none names a path, repository or content. The repository story
// logs them on its deployment_artifacts stage, the repository context on
// content_infrastructure_overview, and the service routes on
// deployment_evidence. The candidate counts (#7951) are summed per request:
// candidates_unhydrated above zero means the request budget bit, and
// repositories_list_truncated above zero means a related file list was cut at
// the 5,000-file window.
func DeploymentArtifactsReadCountAttrs(stats artifacts.ConfigArtifactReadStats) []slog.Attr {
	return []slog.Attr{
		slog.Int("related_repository_count", stats.RelatedRepositoryCount),
		slog.Int("files_listed", stats.FilesListed),
		slog.Int("files_hydrated", stats.FilesHydrated),
		slog.Int("files_skipped", stats.FilesSkipped),
		slog.Int("candidate_count", stats.CandidateCount),
		slog.Int("candidates_hydrated", stats.CandidatesHydrated),
		slog.Int("candidates_unhydrated", stats.CandidatesUnhydrated),
		slog.Int("repositories_budget_capped", stats.RepositoriesBudgetCapped),
		slog.Int("repositories_list_truncated", stats.RepositoriesListTruncated),
		slog.Int("hydration_budget", stats.HydrationBudget),
		slog.Int("hydration_floor", stats.HydrationFloor),
		slog.Int64("hydrated_bytes", stats.HydratedBytes),
		slog.Int("statements", stats.Statements),
		slog.Int("blank_check_statements", stats.BlankCheckStatements),
		slog.Int("batch_statements", stats.BatchStatements),
		slog.Int("batch_chunk_size", stats.BatchChunkSize),
		slog.Int("batch_chunks_in_flight_max", stats.BatchChunksInFlightMax),
		slog.Float64("list_seconds", stats.ListDuration.Seconds()),
		slog.Float64("hydrate_seconds", stats.HydrateDuration.Seconds()),
		slog.Float64("parse_seconds", stats.ParseDuration.Seconds()),
	}
}

// deploymentArtifactsReadCountAttrs keeps the in-package spelling.
func deploymentArtifactsReadCountAttrs(stats artifacts.ConfigArtifactReadStats) []slog.Attr {
	return DeploymentArtifactsReadCountAttrs(stats)
}

// RecordDeploymentArtifactCandidates adds one deployment-artifact load's
// candidate outcomes to eshu_dp_config_artifact_candidates_total (#7951):
// hydrated and unhydrated candidates, and one list_truncated per related
// repository whose file list was cut. A zero outcome is not added, so a
// series exists only once its outcome happened. Nil instruments record
// nothing. It carries no repository label.
func RecordDeploymentArtifactCandidates(ctx context.Context, instruments *telemetry.Instruments, stats artifacts.ConfigArtifactReadStats) {
	if instruments == nil || instruments.ConfigArtifactCandidates == nil {
		return
	}
	for _, outcome := range []struct {
		name  string
		count int
	}{
		{telemetry.ConfigArtifactCandidateOutcomeHydrated, stats.CandidatesHydrated},
		{telemetry.ConfigArtifactCandidateOutcomeUnhydrated, stats.CandidatesUnhydrated},
		{telemetry.ConfigArtifactCandidateOutcomeListTruncated, stats.RepositoriesListTruncated},
	} {
		if outcome.count > 0 {
			instruments.ConfigArtifactCandidates.Add(ctx, int64(outcome.count), metric.WithAttributes(telemetry.AttrOutcome(outcome.name)))
		}
	}
}

// deploymentArtifactsStageAttrs builds the attributes the story's
// deployment_artifacts stage timer attaches: whether an overview was
// assembled, the failure when there was one, and the read counts.
func deploymentArtifactsStageAttrs(found bool, err error, stats artifacts.ConfigArtifactReadStats) []slog.Attr {
	attrs := []slog.Attr{slog.Bool("found", found), slog.Bool("error", err != nil)}
	attrs = append(attrs, deploymentArtifactsReadCountAttrs(stats)...)
	if err != nil {
		attrs = append(attrs, slog.String("failure_class", deploymentArtifactsReadDegradedReason))
	}
	return attrs
}

// reportDeploymentArtifactsFailure makes a failed deployment-artifact load
// visible to an operator: a WARN log carrying the error text and the read
// counts reached, and an error status on the request span. The response text
// is unchanged apart from the degraded reason the caller appends; the error
// text stays in the log because it can name a database object.
func reportDeploymentArtifactsFailure(
	ctx context.Context,
	logger *slog.Logger,
	operation, repoID string,
	err error,
	stats artifacts.ConfigArtifactReadStats,
) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, deploymentArtifactsReadDegradedReason)
	if logger == nil {
		return
	}
	attrs := []slog.Attr{
		telemetry.EventAttr("repository_query.deployment_artifacts_degraded"),
		log.Operation(operation),
		slog.String("stage", "deployment_artifacts"),
		slog.String("repo_id", repoID),
		slog.String("error", err.Error()),
	}
	attrs = append(attrs, deploymentArtifactsReadCountAttrs(stats)...)
	attrs = append(attrs, slog.String("failure_class", deploymentArtifactsReadDegradedReason))
	logger.LogAttrs(ctx, slog.LevelWarn, "repository deployment artifacts read failed; answering without the failed load's artifacts", attrs...)
}
