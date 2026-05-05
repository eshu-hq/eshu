package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

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

	// CodeCallProjectionRunner runs the controlled code-call projection lane
	// concurrently with the main claim/execute/ack loop. Nil disables the lane.
	CodeCallProjectionRunner *CodeCallProjectionRunner

	// RepoDependencyProjectionRunner runs the source-repo-owned repo dependency
	// projection lane concurrently with the main reducer loop. Nil disables it.
	RepoDependencyProjectionRunner *RepoDependencyProjectionRunner

	// GraphProjectionPhaseRepairer retries exact readiness publications that
	// failed after the underlying graph write already committed.
	GraphProjectionPhaseRepairer *GraphProjectionPhaseRepairer

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

	if s.SharedProjectionRunner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.SharedProjectionRunner.Run(ctx); err != nil {
				recordErr(err)
			}
		}()
	}

	if s.CodeCallProjectionRunner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.CodeCallProjectionRunner.Run(ctx); err != nil {
				recordErr(err)
			}
		}()
	}

	if s.RepoDependencyProjectionRunner != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.RepoDependencyProjectionRunner.Run(ctx); err != nil {
				recordErr(err)
			}
		}()
	}

	if s.GraphProjectionPhaseRepairer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.GraphProjectionPhaseRepairer.Run(ctx); err != nil {
				recordErr(err)
			}
		}()
	}

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
	defer func() {
		_ = stopHeartbeat()
	}()

	result, err := s.Executor.Execute(execCtx, intent)
	duration := time.Since(start).Seconds()
	status := "succeeded"

	if err != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			err = errors.Join(err, heartbeatErr)
		}
		status = "failed"
		s.recordReducerResult(ctx, intent, duration, queueWait, status, workerID, err)
		if failErr := s.WorkSink.Fail(ctx, intent, err); failErr != nil {
			return errors.Join(err, fmt.Errorf("fail reducer work: %w", failErr))
		}
		return nil
	}

	if result.Status == ResultStatusSuperseded {
		status = "superseded"
	}

	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		s.recordReducerResult(ctx, intent, duration, queueWait, "ack_failed", workerID, heartbeatErr)
		return fmt.Errorf("heartbeat reducer work: %w", heartbeatErr)
	}

	if err := s.WorkSink.Ack(ctx, intent, result); err != nil {
		s.recordReducerResult(ctx, intent, duration, queueWait, "ack_failed", workerID, err)
		return fmt.Errorf("ack reducer work: %w", err)
	}

	s.recordReducerResult(ctx, intent, duration, queueWait, status, workerID, nil)
	return nil
}

func (s Service) recordReducerResult(ctx context.Context, intent Intent, duration float64, queueWait float64, status string, workerID int, execErr error) {
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
		logAttrs = append(logAttrs, slog.String("queue", "reducer"))
		logAttrs = append(logAttrs, slog.String("intent_id", intent.IntentID))
		logAttrs = append(logAttrs, slog.String("status", status))
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("handler_duration_seconds", duration))
		logAttrs = append(logAttrs, slog.Float64("queue_wait_seconds", queueWait))
		logAttrs = append(logAttrs, slog.Int("worker_id", workerID))
		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseReduction))
		switch status {
		case "failed", "ack_failed":
			failureClass := "reducer_failure"
			message := "reducer execution failed"
			if status == "ack_failed" {
				failureClass = "ack_failure"
				message = "reducer ack failed"
			}
			logAttrs = append(logAttrs, telemetry.FailureClassAttr(failureClass))
			if execErr != nil {
				logAttrs = append(logAttrs, slog.String("error", execErr.Error()))
			}
			s.Logger.ErrorContext(ctx, message, logAttrs...)
		case "superseded":
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("generation_superseded"))
			s.Logger.InfoContext(ctx, "reducer intent superseded", logAttrs...)
		default:
			s.Logger.InfoContext(ctx, "reducer execution succeeded", logAttrs...)
		}
	}
}

type reducerHeartbeatStop func() error

func (s Service) startHeartbeat(ctx context.Context, intent Intent, workerID int) (context.Context, reducerHeartbeatStop) {
	if s.Heartbeater == nil || s.HeartbeatInterval <= 0 {
		return ctx, func() error { return nil }
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
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
					if s.Logger != nil {
						domainAttrs := telemetry.DomainAttrs(string(intent.Domain), firstReducerPartitionKey(intent))
						logAttrs := make([]any, 0, len(domainAttrs)+5)
						for _, a := range domainAttrs {
							logAttrs = append(logAttrs, a)
						}
						logAttrs = append(logAttrs,
							slog.String("queue", "reducer"),
							slog.String("intent_id", intent.IntentID),
							slog.Int("worker_id", workerID),
							slog.Duration("heartbeat_interval", s.HeartbeatInterval),
							telemetry.PhaseAttr(telemetry.PhaseReduction),
							telemetry.FailureClassAttr("lease_heartbeat_failure"),
							slog.String("error", heartbeatErr.Error()),
						)
						s.Logger.ErrorContext(heartbeatCtx, "reducer lease heartbeat failed", logAttrs...)
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

func firstReducerPartitionKey(intent Intent) string {
	if len(intent.EntityKeys) == 0 {
		return ""
	}
	keys := append([]string(nil), intent.EntityKeys...)
	slices.Sort(keys)
	return keys[0]
}
