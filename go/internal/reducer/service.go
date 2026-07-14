// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer //nolint:filelength // 502 lines: hosted and local service orchestrator wiring. Tracked for split in audit § T8.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const defaultPollInterval = time.Second

// WorkSource claims one reducer intent at a time.
type WorkSource interface {
	Claim(context.Context) (Intent, bool, error)
}

// Executor executes one claimed reducer intent.
type Executor interface {
	Execute(context.Context, Intent) (Result, error)
}

// WorkSink acknowledges or fails one claimed reducer intent.
type WorkSink interface {
	Ack(context.Context, Intent, Result) error
	Fail(context.Context, Intent, error) error
}

// WorkHeartbeater extends the claim on one long-running reducer intent.
type WorkHeartbeater interface {
	Heartbeat(context.Context, Intent) error
}

// BatchWorkSource claims up to N reducer intents in a single Postgres
// round-trip. Implementations MUST use FOR UPDATE SKIP LOCKED semantics.
type BatchWorkSource interface {
	ClaimBatch(ctx context.Context, limit int) ([]Intent, error)
}

// BatchWorkSink acknowledges multiple intents in one round-trip.
type BatchWorkSink interface {
	AckBatch(ctx context.Context, intents []Intent, results []Result) error
}

// Service coordinates reducer polling and one-intent-at-a-time execution.
type Service struct {
	PollInterval      time.Duration
	WorkSource        WorkSource
	Executor          Executor
	WorkSink          WorkSink
	Heartbeater       WorkHeartbeater
	HeartbeatInterval time.Duration
	Wait              func(context.Context, time.Duration) error

	// SharedProjectionEdgeWriter is the Neo4j edge writer used by the shared
	// projection worker loop (ProcessPartitionOnce). Nil until Neo4j is wired.
	SharedProjectionEdgeWriter SharedProjectionEdgeWriter

	// SharedProjectionRunner runs the shared projection intent processing loop
	// concurrently with the main claim/execute/ack loop. Nil disables the runner.
	SharedProjectionRunner *SharedProjectionRunner

	// SupplyChainImpactWinnersMaintainer keeps the
	// supply_chain_impact_canonical_winners read model reconciled with the active
	// impact facts (#3389) via a lease-guarded periodic atomic resweep. Nil
	// disables the maintainer.
	SupplyChainImpactWinnersMaintainer *SupplyChainImpactWinnersMaintainer

	// CollectorEvidenceSummaryMaintainer keeps the collector_evidence_summary read
	// model reconciled with the active fact set (#3466) via a lease-guarded
	// periodic atomic resweep, so the collector-readiness API read joins a small
	// materialized table instead of scanning fact_records. Nil disables it.
	CollectorEvidenceSummaryMaintainer *CollectorEvidenceSummaryMaintainer

	// CodeCallProjectionRunner runs the controlled code-call projection lane
	// concurrently with the main claim/execute/ack loop. Nil disables the lane.
	CodeCallProjectionRunner *CodeCallProjectionRunner

	// RepoDependencyProjectionRunner runs the source-repo-owned repo dependency
	// projection lane concurrently with the main reducer loop. Nil disables it.
	RepoDependencyProjectionRunner *RepoDependencyProjectionRunner

	// CodeReachabilityProjectionRunner maintains the materialized code
	// reachable-set read model. Nil disables it.
	CodeReachabilityProjectionRunner *CodeReachabilityProjectionRunner

	// GraphProjectionPhaseRepairer retries exact readiness publications that
	// failed after the underlying graph write already committed.
	GraphProjectionPhaseRepairer *GraphProjectionPhaseRepairer

	// GenerationRetentionRunner prunes superseded source-generation history in
	// bounded transactions. Nil disables automated cleanup.
	GenerationRetentionRunner *GenerationRetentionRunner

	// GenerationLivenessRunner re-drives active generations that wedge past the
	// activation deadline and supersedes orphaned older actives. Nil disables
	// generation lifecycle self-healing.
	GenerationLivenessRunner *GenerationLivenessRunner

	// PoisonLivenessRunner bounds-recovers the dead-letter/poison class (#4740):
	// fact_work_items rows that are terminally 'dead_letter' with no newer
	// scope generation, a class GenerationLivenessRunner does not reach. Nil
	// when bounded auto-retry is disabled (the default, surface-only posture);
	// the stuck-gauge remains active independently of this field.
	PoisonLivenessRunner *PoisonLivenessRunner

	// GraphOrphanSweepRunner marks and deletes aged zero-relationship graph
	// nodes in bounded batches. Nil disables automated cleanup.
	GraphOrphanSweepRunner *GraphOrphanSweepRunner

	// CodeValueFlowStaleCleanupRunner removes reducer-owned value-flow evidence
	// from older source generations in bounded batches. Nil disables cleanup.
	CodeValueFlowStaleCleanupRunner *CodeValueFlowStaleCleanupRunner

	// SearchVectorBuildRunner builds derived search-vector rows for active
	// curated search documents. Nil disables vector build work.
	SearchVectorBuildRunner *SearchVectorBuildRunner

	// QuarantineWriter persists durable per-fact input_invalid quarantine rows
	// (issue #4630) to the reducer_input_invalid_facts read surface.
	// executeWithTelemetry stashes it on the execution context via
	// WithQuarantineWriter once per claimed intent so every domain handler's
	// recordQuarantinedFacts call (quarantine_writer.go, factschema_decode.go)
	// can reach it without a per-handler field. Nil disables durable
	// persistence; the existing eshu_dp_reducer_input_invalid_facts_total
	// counter and structured error log are unaffected.
	QuarantineWriter QuarantinedFactWriter

	// Telemetry fields (optional)
	Tracer         trace.Tracer
	Instruments    *telemetry.Instruments
	Logger         *slog.Logger
	Workers        int // concurrent worker count; 0 or 1 means sequential
	BatchClaimSize int // items per ClaimBatch call; 0 uses default (Workers * 4, max 64)
}

// Run polls for reducer work until the context is canceled. If a
// SharedProjectionRunner is configured, it runs concurrently as a goroutine.
func (s Service) Run(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Logger != nil {
		s.Logger.Info("starting reducer", slog.Int("workers", s.Workers))
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg        sync.WaitGroup
		errMu     sync.Mutex
		firstErr  error
		recordErr = func(err error) {
			if err == nil {
				return
			}
			errMu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			errMu.Unlock()
			cancel()
		}
	)

	s.startSideRunners(ctx, &wg, recordErr)

	err := s.runMainLoop(ctx)
	if err != nil {
		recordErr(err)
	}

	cancel()
	wg.Wait()

	errMu.Lock()
	defer errMu.Unlock()
	return firstErr
}

// runMainLoop is the main claim/execute/ack loop extracted for concurrent use.
func (s Service) runMainLoop(ctx context.Context) error {
	if s.Workers <= 1 {
		return s.runSequential(ctx)
	}
	return s.runConcurrent(ctx)
}

// runSequential processes intents one at a time.
func (s Service) runSequential(ctx context.Context) error {
	for {
		claimStart := time.Now()
		intent, ok, err := s.WorkSource.Claim(ctx)
		if s.Instruments != nil {
			s.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
				attribute.String("queue", "reducer"),
			))
		}
		if err != nil {
			return fmt.Errorf("claim reducer work: %w", err)
		}
		if !ok {
			if err := s.wait(ctx, s.pollInterval()); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for reducer work: %w", err)
			}
			continue
		}

		if err := s.executeWithTelemetry(ctx, intent, 0); err != nil {
			return err
		}
	}
}

// runConcurrent spawns N worker goroutines that compete for reducer intents.
// If the WorkSource implements BatchWorkSource (and WorkSink implements
// BatchWorkSink), it uses batch claiming to reduce Postgres round-trips.
// Otherwise each worker independently claims, executes, and acknowledges work.
func (s Service) runConcurrent(ctx context.Context) error {
	batchSource, canBatch := s.WorkSource.(BatchWorkSource)
	batchSink, canBatchAck := s.WorkSink.(BatchWorkSink)
	if canBatch && canBatchAck {
		return s.runBatchConcurrent(ctx, batchSource, batchSink)
	}
	return s.runPerItemConcurrent(ctx)
}

func (s Service) runPerItemConcurrent(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	for i := 0; i < s.Workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}

				claimStart := time.Now()
				intent, ok, err := s.WorkSource.Claim(ctx)
				if s.Instruments != nil {
					s.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
						attribute.String("queue", "reducer"),
					))
				}
				if err != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("claim reducer work (worker %d): %w", workerID, err))
					mu.Unlock()
					cancel()
					return
				}
				if !ok {
					if err := s.wait(ctx, s.pollInterval()); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
							return
						}
						mu.Lock()
						errs = append(errs, fmt.Errorf("wait for reducer work (worker %d): %w", workerID, err))
						mu.Unlock()
						cancel()
						return
					}
					continue
				}

				if err := s.executeWithTelemetry(ctx, intent, workerID); err != nil {
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
	return errors.Join(errs...)
}

func (s Service) validate() error {
	if s.WorkSource == nil {
		return errors.New("work source is required")
	}
	if s.Executor == nil {
		return errors.New("executor is required")
	}
	if s.WorkSink == nil {
		return errors.New("work sink is required")
	}

	return nil
}

func (s Service) executeWithTelemetry(ctx context.Context, intent Intent, workerID int) error {
	start := time.Now()
	queueWait := reducerQueueWaitSeconds(start, intent.AvailableAt)

	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanReducerRun)
		defer span.End()
	}

	execCtx, stopHeartbeat := s.startHeartbeat(ctx, intent, workerID)
	var stopOnce sync.Once
	var stopHeartbeatErr error
	stopHeartbeatOnce := func() error {
		stopOnce.Do(func() { stopHeartbeatErr = stopHeartbeat() })
		return stopHeartbeatErr
	}
	defer func() {
		_ = stopHeartbeatOnce()
	}()

	// If the immediate pre-heartbeat already failed, startHeartbeat cancelled
	// execCtx before returning it, and no handler work has run yet. Detect
	// that via execCtx.Err() rather than calling stopHeartbeat() here: the
	// stop function is a single-shot close over the heartbeat goroutine, and
	// calling it before Executor.Execute would tear down a healthy periodic
	// heartbeat loop before the handler ever starts. stopHeartbeatOnce above
	// caches the first call's result so every call site below -- this one
	// included -- observes the same error instead of losing it to a second,
	// no-op call. Do not call Executor.Execute (it would only observe the
	// cancellation and return context.Canceled) and do not route through
	// WorkSink.Fail: neither IsRetryable nor the dead-letter triage path
	// knows this claim never started real work, so Fail here can wrongly
	// dead-letter an intent that simply lost its lease before starting.
	// Leaving the lease unrenewed lets the expired-lease reclaim path
	// (#4464) pick it back up (#4447 follow-up).
	if execCtx.Err() != nil {
		var preFailure *reducerPreHeartbeatFailure
		err := stopHeartbeatOnce()
		if errors.As(err, &preFailure) {
			duration := time.Since(start).Seconds()
			s.recordReducerResult(ctx, intent, Result{}, duration, queueWait, "lease_lost_before_start", workerID, err)
			return nil
		}
	}

	execCtx = WithQuarantineWriter(execCtx, s.QuarantineWriter)
	result, err := s.Executor.Execute(execCtx, intent)
	duration := time.Since(start).Seconds()
	status := "succeeded"

	if err != nil {
		if heartbeatErr := stopHeartbeatOnce(); heartbeatErr != nil {
			err = errors.Join(err, heartbeatErr)
		}
		status = "failed"
		s.recordReducerResult(ctx, intent, Result{}, duration, queueWait, status, workerID, err)
		if failErr := s.WorkSink.Fail(ctx, intent, err); failErr != nil {
			return errors.Join(err, fmt.Errorf("fail reducer work: %w", failErr))
		}
		return nil
	}

	if result.Status == ResultStatusSuperseded {
		status = "superseded"
	}

	if heartbeatErr := stopHeartbeatOnce(); heartbeatErr != nil {
		s.recordReducerResult(ctx, intent, Result{}, duration, queueWait, "ack_failed", workerID, heartbeatErr)
		return fmt.Errorf("heartbeat reducer work: %w", heartbeatErr)
	}

	if err := s.WorkSink.Ack(ctx, intent, result); err != nil {
		s.recordReducerResult(ctx, intent, Result{}, duration, queueWait, "ack_failed", workerID, err)
		return fmt.Errorf("ack reducer work: %w", err)
	}

	s.recordReducerResult(ctx, intent, result, duration, queueWait, status, workerID, nil)
	return nil
}

func (s Service) recordReducerResult(ctx context.Context, intent Intent, result Result, duration float64, queueWait float64, status string, workerID int, execErr error) {
	if s.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
			attribute.String("queue", "reducer"),
			attribute.String("status", status),
		)
		s.Instruments.ReducerRunDuration.Record(ctx, duration, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
		s.Instruments.ReducerQueueWaitDuration.Record(ctx, queueWait, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
		s.Instruments.ReducerExecutions.Add(ctx, 1, attrs)
	}

	if s.Logger != nil {
		partitionKey := ""
		if len(intent.EntityKeys) > 0 {
			partitionKey = intent.EntityKeys[0]
		}
		domainAttrs := telemetry.DomainAttrs(string(intent.Domain), partitionKey)
		logAttrs := make([]any, 0, len(domainAttrs)+4)
		for _, a := range domainAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs, log.Queue("reducer"))
		logAttrs = append(logAttrs, log.IntentID(intent.IntentID))
		logAttrs = append(logAttrs, log.Status(status))
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("handler_duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("queue_wait_seconds", queueWait))
		// Emit per-phase sub-timings when the handler populated them. Keys match
		// the workload materialization log attribute names so operators can
		// correlate the service-level log line with the handler-level log line
		// without reading two separate log streams.
		for k, v := range result.SubDurations {
			logAttrs = append(logAttrs, slog.Float64("sub_duration_"+k+"_seconds", v))
		}
		// Emit non-duration diagnostic signals (counts and flags such as
		// input_ready and written_rows) under a separate sub_signal_<key> prefix
		// with NO _seconds suffix, so an operator never misreads a row count or a
		// boolean flag as a wall-time measurement.
		for k, v := range result.SubSignals {
			logAttrs = append(logAttrs, slog.Float64("sub_signal_"+k, v))
		}
		logAttrs = append(logAttrs, log.WorkerID(fmt.Sprintf("%d", workerID)))
		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseReduction))
		switch status {
		case "failed", "ack_failed":
			message := "reducer execution failed"
			failureClass := reducerExecutionFailureClass(execErr)
			if status == "ack_failed" {
				failureClass = "ack_failure"
				message = "reducer ack failed"
			}
			logAttrs = append(logAttrs, telemetry.FailureClassAttr(failureClass))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.ErrorContext(ctx, message, logAttrs...)
		case "superseded":
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("generation_superseded"))
			s.Logger.InfoContext(ctx, "reducer intent superseded", logAttrs...)
		case "lease_lost_before_start":
			// No handler work ran under this claim; the lease is left
			// unrenewed for the expired-lease reclaim path (#4464) rather
			// than dead-lettered, so this is an operator-visible warning, not
			// a terminal failure.
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("lease_heartbeat_failure"))
			if execErr != nil {
				logAttrs = append(logAttrs, log.Err(execErr))
			}
			s.Logger.WarnContext(ctx, "reducer claim lost its lease before handler start", logAttrs...)
		default:
			s.Logger.InfoContext(ctx, "reducer execution succeeded", logAttrs...)
		}
	}
}

type reducerClassifiedFailure interface {
	FailureClass() string
}

func reducerExecutionFailureClass(err error) string {
	var classified reducerClassifiedFailure
	if errors.As(err, &classified) {
		if failureClass := strings.TrimSpace(classified.FailureClass()); failureClass != "" {
			return failureClass
		}
	}
	return "reducer_failure"
}

type reducerHeartbeatStop func() error

// reducerPreHeartbeatFailure marks a heartbeat failure that happened before
// Executor.Execute ever ran, so the caller can tell it apart from a failure
// during or after real handler work. No handler work ran under this claim,
// so the intent must never be routed through WorkSink.Fail (which can
// dead-letter it): the correct recovery is to leave the lease unrenewed so
// the expired-lease reclaim path (#4464) picks it back up, or a retry.
type reducerPreHeartbeatFailure struct {
	err error
}

func (e *reducerPreHeartbeatFailure) Error() string { return e.err.Error() }
func (e *reducerPreHeartbeatFailure) Unwrap() error { return e.err }

// startHeartbeat starts the reducer lease heartbeat loop for a claimed
// intent. It emits one heartbeat synchronously, before returning, so a
// worker that stalls (GC pause, slow first graph write) immediately after
// claim cannot let the lease expire before any heartbeat has landed (#4447).
// HeartbeatInterval = LeaseDuration/2 and the periodic ticker only fires
// after a full interval has elapsed, leaving that startup window open
// without this immediate pre-heartbeat.
//
// If the immediate pre-heartbeat itself fails, the returned stop function's
// error is wrapped in reducerPreHeartbeatFailure so executeWithTelemetry can
// skip Executor.Execute and WorkSink.Fail entirely (#4447 follow-up): no
// handler work has run yet, so there is nothing to execute or dead-letter.
func (s Service) startHeartbeat(ctx context.Context, intent Intent, workerID int) (context.Context, reducerHeartbeatStop) {
	if s.Heartbeater == nil || s.HeartbeatInterval <= 0 {
		return ctx, func() error { return nil }
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)

	if err := s.Heartbeater.Heartbeat(heartbeatCtx, intent); err != nil {
		heartbeatErr := fmt.Errorf("heartbeat reducer work: %w", err)
		s.recordReducerHeartbeatMissed(heartbeatCtx, intent, workerID, heartbeatErr)
		cancel()
		preFailure := &reducerPreHeartbeatFailure{err: heartbeatErr}
		return heartbeatCtx, func() error { return preFailure }
	}

	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(s.HeartbeatInterval)
		defer ticker.Stop()

		var heartbeatErr error
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				if err := s.Heartbeater.Heartbeat(heartbeatCtx, intent); err != nil {
					heartbeatErr = fmt.Errorf("heartbeat reducer work: %w", err)
					s.recordReducerHeartbeatMissed(heartbeatCtx, intent, workerID, heartbeatErr)
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

// recordReducerHeartbeatMissed logs and increments the operator-facing
// missed-heartbeat signal for a reducer lease heartbeat failure, whether it
// came from the immediate pre-heartbeat or a later periodic tick.
func (s Service) recordReducerHeartbeatMissed(ctx context.Context, intent Intent, workerID int, heartbeatErr error) {
	if s.Instruments != nil {
		s.Instruments.ReducerHeartbeatMissed.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(intent.Domain)),
		))
	}
	if s.Logger != nil {
		domainAttrs := telemetry.DomainAttrs(string(intent.Domain), firstReducerPartitionKey(intent))
		logAttrs := make([]any, 0, len(domainAttrs)+5)
		for _, a := range domainAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(
			logAttrs,
			log.Queue("reducer"),
			log.IntentID(intent.IntentID),
			log.WorkerID(fmt.Sprintf("%d", workerID)),
			slog.Duration("heartbeat_interval", s.HeartbeatInterval),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
			telemetry.FailureClassAttr("lease_heartbeat_failure"),
			log.Err(heartbeatErr),
		)
		s.Logger.ErrorContext(ctx, "reducer lease heartbeat failed", logAttrs...)
	}
}

func firstReducerPartitionKey(intent Intent) string {
	if len(intent.EntityKeys) == 0 {
		return ""
	}
	keys := append([]string(nil), intent.EntityKeys...)
	slices.Sort(keys)
	return keys[0]
}
