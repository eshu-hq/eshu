// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// repositoryQueryStageTimer emits repository read-stage timings so full-corpus
// hydration stalls can be diagnosed even when the client gives up first.
type repositoryQueryStageTimer struct {
	logger    *slog.Logger
	operation string
	repoID    string
	stage     string
	startedAt time.Time
}

func startRepositoryQueryStage(
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	repoID string,
	stage string,
) repositoryQueryStageTimer {
	timer := repositoryQueryStageTimer{
		logger:    logger,
		operation: operation,
		repoID:    repoID,
		stage:     stage,
		startedAt: time.Now(),
	}
	if logger != nil {
		logger.InfoContext(
			ctx, "repository query stage started",
			telemetry.EventAttr("repository_query.stage_started"),
			log.Operation(operation),
			slog.String("stage", stage),
			slog.String("repo_id", repoID),
		)
	}
	return timer
}

// Done emits a bounded completion event with duration and caller-owned counts.
func (t repositoryQueryStageTimer) Done(ctx context.Context, attrs ...slog.Attr) {
	if t.logger == nil {
		return
	}
	base := []slog.Attr{
		telemetry.EventAttr("repository_query.stage_completed"),
		log.Operation(t.operation),
		slog.String("stage", t.stage),
		slog.String("repo_id", t.repoID),
		slog.Float64("duration_seconds", time.Since(t.startedAt).Seconds()),
	}
	base = append(base, attrs...)
	t.logger.LogAttrs(ctx, slog.LevelInfo, "repository query stage completed", base...)
}

// logRepositoryDependencyClusterErrors emits a bounded warning for each failed
// dependency-edge query, carrying the error text that the
// repository_query.dependency_edges_degraded event (a boolean) does not. The
// list and catalog still degrade to non-cluster grouping and disclose
// dependency_marker_evidence_incomplete on a scan failure, so without this
// event a timed-out or failing pre-pass would have no operator-visible cause.
// A probe failure is not a degradation (the scan still runs) and is logged
// here only.
func logRepositoryDependencyClusterErrors(ctx context.Context, logger *slog.Logger, operation string, result repositoryDependencyEdgeRead) {
	if logger == nil {
		return
	}
	if result.ProbeErr != nil {
		logger.WarnContext(ctx, "repository dependency edge probe failed; running edge scan",
			telemetry.EventAttr("repository_query.dependency_cluster_probe_failed"),
			log.Operation(operation),
			slog.String("error", result.ProbeErr.Error()),
		)
	}
	if result.Err != nil {
		logger.WarnContext(ctx, "repository dependency edge scan failed; clusters omitted",
			telemetry.EventAttr("repository_query.dependency_cluster_scan_failed"),
			log.Operation(operation),
			slog.String("error", result.Err.Error()),
		)
	}
}
