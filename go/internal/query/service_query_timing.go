package query

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// serviceQueryStageTimer emits per-stage service read timings so slow graph or
// content hydration paths can be diagnosed even when the client times out.
type serviceQueryStageTimer struct {
	logger    *slog.Logger
	operation string
	service   string
	repoID    string
	stage     string
	startedAt time.Time
}

// startServiceQueryStage logs a bounded stage start and returns a timer for the
// matching completion event.
func startServiceQueryStage(
	ctx context.Context,
	logger *slog.Logger,
	operation string,
	service string,
	repoID string,
	stage string,
) serviceQueryStageTimer {
	timer := serviceQueryStageTimer{
		logger:    logger,
		operation: operation,
		service:   service,
		repoID:    repoID,
		stage:     stage,
		startedAt: time.Now(),
	}
	if logger != nil {
		logger.InfoContext(ctx, "service query stage started",
			telemetry.EventAttr("service_query.stage_started"),
			slog.String("operation", operation),
			slog.String("stage", stage),
			slog.String("target_service", service),
			slog.String("repo_id", repoID),
		)
	}
	return timer
}

// Done emits the completion event with duration and caller-supplied row/result
// attributes.
func (t serviceQueryStageTimer) Done(ctx context.Context, attrs ...slog.Attr) {
	if t.logger == nil {
		return
	}
	base := []slog.Attr{
		telemetry.EventAttr("service_query.stage_completed"),
		slog.String("operation", t.operation),
		slog.String("stage", t.stage),
		slog.String("target_service", t.service),
		slog.String("repo_id", t.repoID),
		slog.Float64("duration_seconds", time.Since(t.startedAt).Seconds()),
	}
	base = append(base, attrs...)
	t.logger.LogAttrs(ctx, slog.LevelInfo, "service query stage completed", base...)
}
