// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// supplyChainQueryStageTimer emits per-backing-read stage timings for
// supply-chain query routes, mirroring the repository_query/service_query
// stage-timer convention (go/internal/query/repository/query_timing.go,
// go/internal/query/service/query_timing.go). Issue #7007: the
// impact/findings route combined a Postgres findings read, a Postgres
// readiness-snapshot read, and up to three graph probes with no stage-level
// signal, so a 13-25s request had nothing to attribute the time to.
type supplyChainQueryStageTimer struct {
	logger    *slog.Logger
	operation string
	repoID    string
	stage     string
	startedAt time.Time
}

// startSupplyChainQueryStage logs a bounded stage start and returns a timer
// for the matching completion event. A nil logger makes every call a no-op.
func startSupplyChainQueryStage(
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	repoID string,
	stage string,
) supplyChainQueryStageTimer {
	timer := supplyChainQueryStageTimer{
		logger:    logger,
		operation: operation,
		repoID:    repoID,
		stage:     stage,
		startedAt: time.Now(),
	}
	if logger != nil {
		logger.InfoContext(
			ctx, "supply chain query stage started",
			telemetry.EventAttr("supply_chain_query.stage_started"),
			log.Operation(operation),
			slog.String("stage", stage),
			slog.String("repo_id", repoID),
		)
	}
	return timer
}

// Done emits a bounded completion event with duration and caller-owned
// attributes (row counts, truncation, error class, and similar bounded
// values — never raw payloads or unbounded identifiers).
func (t supplyChainQueryStageTimer) Done(ctx context.Context, attrs ...slog.Attr) {
	if t.logger == nil {
		return
	}
	base := []slog.Attr{
		telemetry.EventAttr("supply_chain_query.stage_completed"),
		log.Operation(t.operation),
		slog.String("stage", t.stage),
		slog.String("repo_id", t.repoID),
		slog.Float64("duration_seconds", time.Since(t.startedAt).Seconds()),
	}
	base = append(base, attrs...)
	t.logger.LogAttrs(ctx, slog.LevelInfo, "supply chain query stage completed", base...)
}
