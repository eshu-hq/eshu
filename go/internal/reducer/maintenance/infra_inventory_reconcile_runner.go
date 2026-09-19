// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultInfraInventoryReconcilePollInterval = 5 * time.Minute
	defaultInfraInventoryReconcileRepoBudget   = 500
)

// InfraInventoryReconcileRepo is the result of checking one repository.
// Outcome is match, suspect (differed once; re-checked next cycle), repaired,
// fenced (re-derived because a binary that does not derive wrote its content;
// readers stay on the graph until it is), or error.
type InfraInventoryReconcileRepo struct {
	RepoID      string
	Outcome     string
	ContentRows int64
	TableRows   int64
	Duration    time.Duration
	Err         error
}

// InfraInventoryReconcileBatch is one bounded reconcile cycle. Ready is false
// when the read model is not in use yet (no backfill marker, or migration 109
// not applied); the backfill owns that state and nothing was checked.
type InfraInventoryReconcileBatch struct {
	Ready      bool
	Repos      []InfraInventoryReconcileRepo
	NextCursor string
}

// InfraInventoryReconcileRequest is one cycle's input.
type InfraInventoryReconcileRequest struct {
	// Cursor is the repo_id the walk resumes after; "" starts at the beginning.
	// The runner leaves it empty: with Persist the reconciler claims its page
	// from the shared persisted cursor.
	Cursor string
	// Budget bounds the repositories the cycle checks, suspects included.
	Budget int
	// Suspects are the repositories the previous cycle found drifted; they are
	// re-checked first and repaired only if they still differ.
	Suspects []string
	// Persist asks the reconciler to claim the cycle's page from the shared
	// persisted walk cursor, so replicas take disjoint pages and a restarted
	// process continues the walk. The runner always sets it.
	Persist bool
}

// InfraInventoryReconciler runs one bounded reconcile cycle: it re-checks the
// suspects, then checks up to the rest of the budget of repositories after
// the cursor. NextCursor is "" when the walk reached the end.
type InfraInventoryReconciler interface {
	ReconcileInfraInventory(ctx context.Context, req InfraInventoryReconcileRequest) (InfraInventoryReconcileBatch, error)
}

// InfraInventoryReconcileRunnerConfig configures the reconcile loop.
type InfraInventoryReconcileRunnerConfig struct {
	// PollInterval is the wait between cycles.
	PollInterval time.Duration
	// RepoBudget bounds how many repositories one cycle checks.
	RepoBudget int
}

func (c InfraInventoryReconcileRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultInfraInventoryReconcilePollInterval
	}
	return c.PollInterval
}

func (c InfraInventoryReconcileRunnerConfig) repoBudget() int {
	if c.RepoBudget <= 0 {
		return defaultInfraInventoryReconcileRepoBudget
	}
	return c.RepoBudget
}

// InfraInventoryReconcileRunner keeps the infra read model (#6793) equal to
// content_entities when some content writer did not derive it: an older
// binary during a rolling upgrade, a manual SQL change, or a restore. Each
// cycle claims a page of at most RepoBudget repositories in repo_id order from
// the walk's shared persisted cursor, so replicas take disjoint pages and a
// restarted process resumes the walk, then waits PollInterval; the walk wraps
// at the end. A repository that differs is repaired only when the next cycle finds it still differing, so a
// Write caught between its content commit and its derive is not repaired or
// reported as drift.
type InfraInventoryReconcileRunner struct {
	Reconciler InfraInventoryReconciler
	Config     InfraInventoryReconcileRunnerConfig
	Wait       func(context.Context, time.Duration) error

	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Logger      *slog.Logger

	suspects []string
}

// Run reconciles one budget of repositories per poll interval until the
// context is cancelled. A failed cycle is counted, logged, and retried on the
// next interval.
func (r *InfraInventoryReconcileRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := r.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			r.recordFailure(ctx, err)
		}
		if err := r.wait(ctx, r.Config.pollInterval()); err != nil {
			if generationRetentionContextDone(ctx, err) {
				return nil
			}
			return fmt.Errorf("wait for infra inventory reconcile: %w", err)
		}
	}
}

// RunOnce runs one bounded reconcile cycle; the page comes from the shared
// persisted walk cursor.
func (r *InfraInventoryReconcileRunner) RunOnce(ctx context.Context) (InfraInventoryReconcileBatch, error) {
	if err := r.validate(); err != nil {
		return InfraInventoryReconcileBatch{}, err
	}
	if r.Tracer != nil {
		var span trace.Span
		ctx, span = r.Tracer.Start(ctx, telemetry.SpanReducerInfraInventoryReconcile)
		defer span.End()
	}
	start := time.Now()
	batch, err := r.Reconciler.ReconcileInfraInventory(ctx, InfraInventoryReconcileRequest{
		Budget:   r.Config.repoBudget(),
		Suspects: r.suspects,
		Persist:  true,
	})
	span := trace.SpanFromContext(ctx)
	if err != nil {
		if r.Instruments != nil && ctx.Err() == nil {
			r.Instruments.InfraInventoryReconcileDuration.Record(ctx, time.Since(start).Seconds())
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "reconcile cycle failed")
		return InfraInventoryReconcileBatch{}, fmt.Errorf("reconcile infra inventory: %w", err)
	}
	if !batch.Ready {
		r.suspects = nil
		span.SetAttributes(attribute.Bool("eshu.infra_inventory.ready", false))
		if r.Logger != nil {
			r.Logger.DebugContext(ctx, "infra inventory reconcile skipped: read model not in use yet",
				telemetry.PhaseAttr(telemetry.PhaseReduction))
		}
		return batch, nil
	}
	var suspects []string
	for _, repo := range batch.Repos {
		if repo.Outcome == "suspect" {
			suspects = append(suspects, repo.RepoID)
		}
	}
	r.suspects = suspects
	r.recordBatch(ctx, span, batch, time.Since(start))
	return batch, nil
}

func (r *InfraInventoryReconcileRunner) recordBatch(
	ctx context.Context, span trace.Span, batch InfraInventoryReconcileBatch, elapsed time.Duration,
) {
	counts := map[string]int{}
	for _, repo := range batch.Repos {
		counts[repo.Outcome]++
		if r.Instruments != nil {
			r.Instruments.InfraInventoryReconcile.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome(repo.Outcome)))
		}
		r.logRepo(ctx, repo)
	}
	if r.Instruments != nil {
		r.Instruments.InfraInventoryReconcileDuration.Record(ctx, elapsed.Seconds())
	}
	span.SetAttributes(
		attribute.Int("eshu.infra_inventory.repos_checked", len(batch.Repos)),
		attribute.Int("eshu.infra_inventory.repos_suspect", counts["suspect"]),
		attribute.Int("eshu.infra_inventory.repos_repaired", counts["repaired"]),
		attribute.Int("eshu.infra_inventory.repos_fenced", counts["fenced"]),
		attribute.Int("eshu.infra_inventory.repos_failed", counts["error"]),
		attribute.Bool("eshu.infra_inventory.walk_wrapped", batch.NextCursor == ""),
	)
	if r.Logger != nil {
		r.Logger.InfoContext(ctx, "infra inventory reconcile cycle completed",
			slog.Int("repos_checked", len(batch.Repos)),
			slog.Int("repos_matched", counts["match"]),
			slog.Int("repos_suspect", counts["suspect"]),
			slog.Int("repos_repaired", counts["repaired"]),
			slog.Int("repos_fenced", counts["fenced"]),
			slog.Int("repos_failed", counts["error"]),
			slog.Bool("walk_wrapped", batch.NextCursor == ""),
			slog.Float64("duration_seconds", elapsed.Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func (r *InfraInventoryReconcileRunner) logRepo(ctx context.Context, repo InfraInventoryReconcileRepo) {
	if r.Logger == nil {
		return
	}
	switch repo.Outcome {
	case "repaired":
		r.Logger.WarnContext(ctx, "infra inventory drift repaired after two checks",
			slog.String("event_name", "infra_inventory.reconcile.drift"),
			slog.String("repo_id", repo.RepoID),
			slog.Int64("content_rows", repo.ContentRows),
			slog.Int64("table_rows", repo.TableRows),
			slog.Float64("duration_seconds", repo.Duration.Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	case "fenced":
		r.Logger.WarnContext(ctx, "infra inventory repository re-derived after a write from a binary that does not derive",
			slog.String("event_name", "infra_inventory.reconcile.fenced"),
			slog.String("repo_id", repo.RepoID),
			slog.Float64("duration_seconds", repo.Duration.Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	case "error":
		r.Logger.ErrorContext(ctx, "infra inventory reconcile failed for repository",
			slog.String("event_name", "infra_inventory.reconcile.failed"),
			slog.String("repo_id", repo.RepoID),
			log.Err(repo.Err),
			telemetry.FailureClassAttr("infra_inventory_reconcile_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func (r *InfraInventoryReconcileRunner) validate() error {
	if r.Reconciler == nil {
		return errors.New("infra inventory reconciler is required")
	}
	return nil
}

func (r *InfraInventoryReconcileRunner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *InfraInventoryReconcileRunner) recordFailure(ctx context.Context, err error) {
	if r.Instruments != nil {
		r.Instruments.InfraInventoryReconcile.Add(ctx, 1, metric.WithAttributes(telemetry.AttrOutcome("error")))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(ctx, "infra inventory reconcile cycle failed",
			slog.String("event_name", "infra_inventory.reconcile.cycle_failed"),
			log.Err(err),
			telemetry.FailureClassAttr("infra_inventory_reconcile_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
