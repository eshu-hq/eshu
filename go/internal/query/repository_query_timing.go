package query

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
		logger.InfoContext(ctx, "repository query stage started",
			telemetry.EventAttr("repository_query.stage_started"),
			slog.String("operation", operation),
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
		slog.String("operation", t.operation),
		slog.String("stage", t.stage),
		slog.String("repo_id", t.repoID),
		slog.Float64("duration_seconds", time.Since(t.startedAt).Seconds()),
	}
	base = append(base, attrs...)
	t.logger.LogAttrs(ctx, slog.LevelInfo, "repository query stage completed", base...)
}
