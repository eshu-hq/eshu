// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/codeintel"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/reducer/searchvector"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	SupplyChainImpactWinnersMaintainer *supplychaincore.SupplyChainImpactWinnersMaintainer

	// CollectorEvidenceSummaryMaintainer keeps the collector_evidence_summary read
	// model reconciled with the active fact set (#3466) via a lease-guarded
	// periodic atomic resweep, so the collector-readiness API read joins a small
	// materialized table instead of scanning fact_records. Nil disables it.
	CollectorEvidenceSummaryMaintainer *maintenance.CollectorEvidenceSummaryMaintainer

	// CodeCallProjectionRunner runs the controlled code-call projection lane
	// concurrently with the main claim/execute/ack loop. Nil disables the lane.
	CodeCallProjectionRunner *CodeCallProjectionRunner

	// RepoDependencyProjectionRunner runs the source-repo-owned repo dependency
	// projection lane concurrently with the main reducer loop. Nil disables it.
	RepoDependencyProjectionRunner *RepoDependencyProjectionRunner

	// CodeReachabilityProjectionRunner maintains the materialized code
	// reachable-set read model. Nil disables it.
	CodeReachabilityProjectionRunner *codeintel.CodeReachabilityProjectionRunner

	// GraphProjectionPhaseRepairer retries exact readiness publications that
	// failed after the underlying graph write already committed.
	GraphProjectionPhaseRepairer *GraphProjectionPhaseRepairer

	// GenerationRetentionRunner prunes superseded source-generation history in
	// bounded transactions. Nil disables automated cleanup.
	GenerationRetentionRunner *maintenance.GenerationRetentionRunner

	// InfraInventoryReconcileRunner re-derives infra read model repositories
	// whose rows drifted from content_entities (#6793). Nil disables it.
	InfraInventoryReconcileRunner *maintenance.InfraInventoryReconcileRunner

	// GenerationLivenessRunner re-drives active generations that wedge past the
	// activation deadline and supersedes orphaned older actives. Nil disables
	// generation lifecycle self-healing.
	GenerationLivenessRunner *maintenance.GenerationLivenessRunner

	// PoisonLivenessRunner bounds-recovers the dead-letter/poison class (#4740):
	// fact_work_items rows that are terminally 'dead_letter' with no newer
	// scope generation, a class GenerationLivenessRunner does not reach. Nil
	// when bounded auto-retry is disabled (the default, surface-only posture);
	// the stuck-gauge remains active independently of this field.
	PoisonLivenessRunner *maintenance.PoisonLivenessRunner

	// GraphOrphanSweepRunner marks and deletes aged zero-relationship graph
	// nodes in bounded batches. Nil disables automated cleanup.
	GraphOrphanSweepRunner *maintenance.GraphOrphanSweepRunner

	// CodeValueFlowStaleCleanupRunner removes reducer-owned value-flow evidence
	// from older source generations in bounded batches. Nil disables cleanup.
	CodeValueFlowStaleCleanupRunner *CodeValueFlowStaleCleanupRunner

	// SearchVectorBuildRunner builds derived search-vector rows for active
	// curated search documents. Nil disables vector build work.
	SearchVectorBuildRunner *searchvector.SearchVectorBuildRunner

	// CrossScopeCompletionRunner converges producer completion by scheduling
	// current-generation canonical consumer rows. Nil disables completion fanout.
	CrossScopeCompletionRunner *CrossScopeCompletionRunner

	// WriterShapeUpgradeRunner executes a claimed graph-writer-shape upgrade
	// after the service is serving. Nil disables it, which is the common
	// case: the marker is already current and there is nothing to retire.
	// It must run beside serving, never inside startup: the upgrade's
	// refinalize waits for the reducer drain, which only completes while
	// this service processes work (issue #6868). The field is the
	// side-runner interface (concrete type recovery.WriterShapeUpgradeRunner)
	// so this package does not import the recovery tree it coordinates.
	WriterShapeUpgradeRunner serviceSideRunner

	// QuarantineWriter persists durable per-fact input_invalid quarantine rows
	// (issue #4630) to the reducer_input_invalid_facts read surface.
	// executeWithTelemetry stashes it on the execution context via
	// WithQuarantineWriter once per claimed intent so every domain handler's
	// recordQuarantinedFacts call (the quarantine stanza of compat_decode.go, factdecode/quarantine_record.go)
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
		if errors.Is(err, ErrExecutionClaimRejected) {
			s.recordReducerResult(ctx, intent, Result{}, duration, queueWait, "lease_lost_during_execution", workerID, err)
			return nil
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

	return s.ackReducerWork(ctx, intent, result, duration, queueWait, status, workerID)
}
