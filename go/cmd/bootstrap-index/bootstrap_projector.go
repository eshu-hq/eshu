// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// drainProjector runs projection workers concurrently. Each worker claims
// work items from the queue, loads facts, projects, and acks independently.
//
// Concurrency model:
//   - N goroutine workers compete for work via workSource.Claim (Postgres
//     SELECT ... FOR UPDATE SKIP LOCKED ensures exactly-once delivery).
//   - A per-item projection failure is routed to the queue Fail path
//     (retry/dead-letter) and isolated: the worker continues and sibling
//     workers are NOT canceled (#4464 — a single slow/timed-out canonical write
//     must not abort the whole run). Only a fatal error (Claim failure, or a
//     Fail-path write failure) cancels the shared context; those are joined.
//   - An atomic counter tracks completed items for structured log output.
//
// Tuning: set ESHU_PROJECTION_WORKERS to control parallelism. Default is
// min(NumCPU, 8). Monitor eshu_dp_projector_run_duration_seconds and
// eshu_dp_queue_claim_duration_seconds{queue=projector} to identify whether
// the bottleneck is CPU (increase workers) or I/O (tune Postgres connections).
func drainProjector(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	factStore projector.FactStore,
	runner projector.ProjectionRunner,
	workSink projector.ProjectorWorkSink,
	heartbeater projector.ProjectorWorkHeartbeater,
	heartbeatInterval time.Duration,
	workers int,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	if workers <= 1 {
		return drainProjectorSequential(ctx, workSource, factStore, runner, workSink, heartbeater, heartbeatInterval, tracer, instruments, logger)
	}

	overallStart := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu        sync.Mutex
		errs      []error
		wg        sync.WaitGroup
		completed atomic.Int64
		failed    atomic.Int64
	)

	for i := 0; i < workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}

				if err := drainProjectorWorkItem(
					ctx, workSource, factStore, runner, workSink,
					heartbeater, heartbeatInterval,
					workerID, &completed, tracer, instruments, logger,
				); err != nil {
					if errors.Is(err, errProjectorDrained) {
						return
					}
					if errors.Is(err, errProjectorItemFailed) {
						failed.Add(1)
						continue
					}
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					cancel()
					return
				}
			}
		}()
	}

	wg.Wait()

	totalCompleted := completed.Load()
	totalFailed := failed.Load()
	if logger != nil {
		logger.InfoContext(
			ctx, "bootstrap projection complete",
			slog.Int64("items_projected", totalCompleted),
			slog.Int64("items_failed", totalFailed),
			slog.Int("workers", workers),
			slog.Float64("total_duration_seconds", time.Since(overallStart).Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseProjection),
		)
	}
	if joined := errors.Join(errs...); joined != nil {
		return joined
	}
	if totalFailed > 0 {
		return fmt.Errorf(
			"bootstrap projection incomplete: %d work item(s) failed and were routed to retry/dead-letter; graph truth is not fully materialized",
			totalFailed,
		)
	}
	return nil
}

// errProjectorDrained is a sentinel indicating the work queue is empty.
var errProjectorDrained = errors.New("projector queue drained")

// errProjectorItemFailed is a sentinel indicating a single work item failed and
// was routed to the queue Fail path (retry/dead-letter). The worker counts it
// and continues; it never cancels siblings. It is not fatal to the run, but the
// drain reports the run incomplete if any item failed, so bootstrap does not
// claim clean completion while work is deferred to retry (#4464 review).
var errProjectorItemFailed = errors.New("projector work item failed (isolated)")

// isolateBootstrapProjectorFailure routes a per-item projection failure to the
// queue Fail path (retry/dead-letter) and isolates it, so the worker continues
// and sibling workers are not aborted by a shared-context cancel (#4464). A
// genuine shutdown cancellation (parent context canceled) is not dead-lettered;
// the claim is left to be reclaimed on restart. A per-write timeout leaves the
// parent context healthy (ctx.Err() == nil) and falls through to the Fail path.
// It returns errProjectorItemFailed on a routed (isolated) failure, nil on a
// shutdown cancellation, and a fatal error only when the Fail-path write itself
// failed.
func isolateBootstrapProjectorFailure(
	ctx context.Context,
	workSink projector.ProjectorWorkSink,
	work projector.ScopeGenerationWork,
	workerID int,
	cause error,
) error {
	if ctx.Err() != nil && (errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)) {
		return nil
	}
	if failErr := workSink.Fail(ctx, work, cause); failErr != nil {
		return fmt.Errorf("bootstrap projector fail item (worker %d): %w", workerID, errors.Join(cause, failErr))
	}
	return errProjectorItemFailed
}

// drainProjectorWorkItem processes a single projection work item with full
// OTEL tracing, metric recording, and structured logging.
func drainProjectorWorkItem(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	factStore projector.FactStore,
	runner projector.ProjectionRunner,
	workSink projector.ProjectorWorkSink,
	heartbeater projector.ProjectorWorkHeartbeater,
	heartbeatInterval time.Duration,
	workerID int,
	completed *atomic.Int64,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	// Claim
	claimStart := time.Now()
	work, ok, err := workSource.Claim(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap projector claim (worker %d): %w", workerID, err)
	}
	if instruments != nil {
		instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
			attribute.String("queue", "projector"),
		))
	}
	if !ok {
		return errProjectorDrained
	}

	// Start span for the full project cycle
	itemStart := time.Now()
	itemCtx := ctx
	var span trace.Span
	if tracer != nil {
		itemCtx, span = tracer.Start(
			ctx, telemetry.SpanProjectorRun,
			trace.WithAttributes(
				attribute.String("scope_id", work.Scope.ScopeID),
				attribute.String("generation_id", work.Generation.GenerationID),
				attribute.Int("worker_id", workerID),
			),
		)
	}

	heartbeatCtx, stopHeartbeat := startBootstrapProjectorHeartbeat(
		itemCtx,
		work,
		heartbeater,
		heartbeatInterval,
		workerID,
		logger,
	)
	defer func() {
		_ = stopHeartbeat()
	}()

	// Load facts
	factsForGeneration, loadErr := factStore.LoadFacts(heartbeatCtx, work)
	if loadErr != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if errors.Is(heartbeatErr, projector.ErrWorkSuperseded) {
				recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "superseded", 0, nil, span, instruments, logger)
				return nil
			}
			loadErr = errors.Join(loadErr, heartbeatErr)
		}
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", 0, loadErr, span, instruments, logger)
		return isolateBootstrapProjectorFailure(itemCtx, workSink, work, workerID, loadErr)
	}

	// Project
	result, projectErr := runner.Project(heartbeatCtx, work.Scope, work.Generation, factsForGeneration)
	if projectErr != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if errors.Is(heartbeatErr, projector.ErrWorkSuperseded) {
				recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "superseded", len(factsForGeneration), nil, span, instruments, logger)
				return nil
			}
			projectErr = errors.Join(projectErr, heartbeatErr)
		}
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", len(factsForGeneration), projectErr, span, instruments, logger)
		return isolateBootstrapProjectorFailure(itemCtx, workSink, work, workerID, projectErr)
	}
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		if errors.Is(heartbeatErr, projector.ErrWorkSuperseded) {
			recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "superseded", len(factsForGeneration), nil, span, instruments, logger)
			return nil
		}
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", len(factsForGeneration), heartbeatErr, span, instruments, logger)
		return isolateBootstrapProjectorFailure(itemCtx, workSink, work, workerID, heartbeatErr)
	}

	// Ack. An Ack failure is fatal, not isolated: Project already committed the
	// graph/content/reducer writes, so routing an ack-write error to Fail could
	// dead-letter successful work and mark the scope generation failed
	// (graph-vs-scope corruption). The steady-state projector treats Ack failure
	// as fatal; match it (#4464 review).
	if ackErr := workSink.Ack(itemCtx, work, result); ackErr != nil {
		recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "failed", len(factsForGeneration), ackErr, span, instruments, logger)
		return fmt.Errorf("bootstrap projector ack (worker %d): %w", workerID, ackErr)
	}

	completed.Add(1)
	recordBootstrapProjectionResult(itemCtx, work, workerID, itemStart, "succeeded", len(factsForGeneration), nil, span, instruments, logger)
	return nil
}

type bootstrapProjectorHeartbeatStop func() error

// startBootstrapProjectorHeartbeat renews bootstrap projector leases during
// long source-local graph writes so a still-running worker is not re-claimed.
func startBootstrapProjectorHeartbeat(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	heartbeater projector.ProjectorWorkHeartbeater,
	interval time.Duration,
	workerID int,
	logger *slog.Logger,
) (context.Context, bootstrapProjectorHeartbeatStop) {
	if heartbeater == nil || interval <= 0 {
		return ctx, func() error { return nil }
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var heartbeatErr error
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				if err := heartbeater.Heartbeat(heartbeatCtx, work); err != nil {
					if heartbeatCtx.Err() != nil && errors.Is(err, heartbeatCtx.Err()) {
						done <- nil
						return
					}
					heartbeatErr = fmt.Errorf("heartbeat bootstrap projector work: %w", err)
					if logger != nil {
						scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
						logAttrs := make([]any, 0, len(scopeAttrs)+5)
						for _, attr := range scopeAttrs {
							logAttrs = append(logAttrs, attr)
						}
						logAttrs = append(
							logAttrs,
							log.WorkerID(strconv.Itoa(workerID)),
							slog.Duration("heartbeat_interval", interval),
							telemetry.PhaseAttr(telemetry.PhaseProjection),
							telemetry.FailureClassAttr("lease_heartbeat_failure"),
							log.Err(heartbeatErr),
						)
						logger.ErrorContext(heartbeatCtx, "bootstrap projector lease heartbeat failed", logAttrs...)
					}
					cancel()
				}
			}
		}
	}()

	var once sync.Once
	return heartbeatCtx, func() error {
		var heartbeatErr error
		once.Do(func() {
			cancel()
			heartbeatErr = <-done
		})
		return heartbeatErr
	}
}

// recordBootstrapProjectionResult records metrics and logs for a single
// projection work item, matching the pattern in projector.Service.
func recordBootstrapProjectionResult(
	ctx context.Context,
	work projector.ScopeGenerationWork,
	workerID int,
	start time.Time,
	status string,
	factCount int,
	err error,
	span trace.Span,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) {
	duration := time.Since(start).Seconds()

	if instruments != nil {
		instruments.ProjectorRunDuration.Record(ctx, duration, metric.WithAttributes(
			telemetry.AttrScopeKind(string(work.Scope.ScopeKind)),
		))
		instruments.ProjectionsCompleted.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrScopeKind(string(work.Scope.ScopeKind)),
			attribute.String("status", status),
		))
	}

	if span != nil {
		span.SetAttributes(
			attribute.Int("fact_count", factCount),
			attribute.String("status", status),
		)
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}

	if logger != nil {
		scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
		logAttrs := make([]any, 0, len(scopeAttrs)+5)
		for _, a := range scopeAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(
			logAttrs,
			log.WorkerID(strconv.Itoa(workerID)),
			log.Status(status),
			slog.Int("fact_count", factCount),
			slog.Float64("duration_seconds", duration),
			telemetry.PhaseAttr(telemetry.PhaseProjection),
		)
		if err != nil {
			logAttrs = append(logAttrs, log.Err(err))
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("projection_failure"))
			logger.ErrorContext(ctx, "bootstrap projection failed", logAttrs...)
			return
		}
		message := "bootstrap projection succeeded"
		if status == "superseded" {
			message = "bootstrap projection superseded by newer generation"
		}
		logger.InfoContext(ctx, message, logAttrs...)
	}
}

// drainProjectorSequential is the single-worker fallback. It uses the same
// per-item instrumentation as the concurrent path for consistent telemetry.
func drainProjectorSequential(
	ctx context.Context,
	workSource projector.ProjectorWorkSource,
	factStore projector.FactStore,
	runner projector.ProjectionRunner,
	workSink projector.ProjectorWorkSink,
	heartbeater projector.ProjectorWorkHeartbeater,
	heartbeatInterval time.Duration,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) error {
	var completed atomic.Int64
	var failed atomic.Int64
	overallStart := time.Now()
	for {
		err := drainProjectorWorkItem(
			ctx, workSource, factStore, runner, workSink,
			heartbeater, heartbeatInterval,
			0, &completed, tracer, instruments, logger,
		)
		if err != nil {
			if errors.Is(err, errProjectorItemFailed) {
				failed.Add(1)
				continue
			}
			if errors.Is(err, errProjectorDrained) {
				totalFailed := failed.Load()
				if logger != nil {
					logger.InfoContext(
						ctx, "bootstrap projection complete",
						slog.Int64("items_projected", completed.Load()),
						slog.Int64("items_failed", totalFailed),
						slog.Int("workers", 1),
						slog.Float64("total_duration_seconds", time.Since(overallStart).Seconds()),
						telemetry.PhaseAttr(telemetry.PhaseProjection),
					)
				}
				if totalFailed > 0 {
					return fmt.Errorf(
						"bootstrap projection incomplete: %d work item(s) failed and were routed to retry/dead-letter; graph truth is not fully materialized",
						totalFailed,
					)
				}
				return nil
			}
			return err
		}
	}
}
