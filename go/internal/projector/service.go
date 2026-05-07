package projector

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

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultPollInterval                = time.Second
	defaultProjectorAckShutdownTimeout = 5 * time.Second
)

// ScopeGenerationWork captures one claimed scope generation for projection.
type ScopeGenerationWork struct {
	Scope      scope.IngestionScope
	Generation scope.ScopeGeneration
	// AttemptCount is the durable number of claims already consumed for this
	// scope generation work item.
	AttemptCount int
}

// ProjectorWorkSource claims one scope generation at a time.
type ProjectorWorkSource interface {
	Claim(context.Context) (ScopeGenerationWork, bool, error)
}

// FactStore loads source-local facts for a claimed scope generation.
type FactStore interface {
	LoadFacts(context.Context, ScopeGenerationWork) ([]facts.Envelope, error)
}

// ProjectionRunner projects one scope generation worth of facts.
type ProjectionRunner interface {
	Project(context.Context, scope.IngestionScope, scope.ScopeGeneration, []facts.Envelope) (Result, error)
}

// ProjectorWorkSink acknowledges or fails claimed projector work.
type ProjectorWorkSink interface {
	Ack(context.Context, ScopeGenerationWork, Result) error
	Fail(context.Context, ScopeGenerationWork, error) error
}

// ProjectorWorkHeartbeater renews a claimed projector work item while a worker
// is still actively loading facts or projecting the generation.
type ProjectorWorkHeartbeater interface {
	Heartbeat(context.Context, ScopeGenerationWork) error
}

// FactCounter returns the fact count for a scope generation without loading data.
// Used by the large-generation semaphore to classify repos before loading.
type FactCounter interface {
	CountFacts(ctx context.Context, scopeID, generationID string) (int, error)
}

// Service coordinates the projector work loop without owning projection logic.
type Service struct {
	PollInterval time.Duration
	WorkSource   ProjectorWorkSource
	FactStore    FactStore
	Runner       ProjectionRunner
	WorkSink     ProjectorWorkSink
	Heartbeater  ProjectorWorkHeartbeater
	// HeartbeatInterval controls how often a claimed projector work item renews
	// its lease while projection is still running. Zero means no heartbeats.
	HeartbeatInterval time.Duration
	Wait              func(context.Context, time.Duration) error
	Tracer            trace.Tracer           // optional
	Instruments       *telemetry.Instruments // optional
	Logger            *slog.Logger           // optional
	Workers           int                    // concurrent worker count; 0 or 1 means sequential

	// Large-generation semaphore: limits how many large repos can be
	// projected concurrently while letting small/medium repos proceed
	// without blocking. Mirrors the collector-layer pattern in GitSource.
	FactCounter           FactCounter // optional; enables large-gen semaphore
	LargeGenThreshold     int         // fact count above which semaphore is required; 0 = 10000
	LargeGenMaxConcurrent int         // max concurrent large generations; 0 = 2
	largeSem              chan struct{}
}

// Run polls for projector work until the context is canceled.
func (s Service) Run(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}

	if s.Logger != nil {
		s.Logger.Info("starting projector", slog.Int("workers", s.Workers))
	}

	if s.Workers <= 1 {
		return s.runSequential(ctx)
	}
	return s.runConcurrent(ctx)
}

// runSequential processes work one at a time.
func (s Service) runSequential(ctx context.Context) error {
	for {
		claimStart := time.Now()
		work, ok, err := s.WorkSource.Claim(ctx)
		if s.Instruments != nil {
			s.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
				attribute.String("queue", "projector"),
			))
		}
		if err != nil {
			if projectorClaimCanceled(ctx, err) {
				return nil
			}
			return fmt.Errorf("claim projector work: %w", err)
		}
		if !ok {
			if err := s.wait(ctx, s.pollInterval()); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for projector work: %w", err)
			}
			continue
		}

		if err := s.processWork(ctx, work, 0); err != nil {
			return err
		}
	}
}

// runConcurrent spawns N worker goroutines that compete for projector work.
// Each worker independently claims, processes, and acknowledges work. On first
// fatal error (Claim or Ack failure), the shared context is canceled to drain
// siblings promptly.
func (s Service) runConcurrent(ctx context.Context) error {
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
				work, ok, err := s.WorkSource.Claim(ctx)
				if s.Instruments != nil {
					s.Instruments.QueueClaimDuration.Record(ctx, time.Since(claimStart).Seconds(), metric.WithAttributes(
						attribute.String("queue", "projector"),
					))
				}
				if err != nil {
					if projectorClaimCanceled(ctx, err) {
						return
					}
					mu.Lock()
					errs = append(errs, fmt.Errorf("claim projector work (worker %d): %w", workerID, err))
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
						errs = append(errs, fmt.Errorf("wait for projector work (worker %d): %w", workerID, err))
						mu.Unlock()
						cancel()
						return
					}
					continue
				}

				if err := s.processWork(ctx, work, workerID); err != nil {
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

func (s Service) processWork(ctx context.Context, work ScopeGenerationWork, workerID int) error {
	start := time.Now()
	workCtx := ctx

	if s.Tracer != nil {
		var span trace.Span
		workCtx, span = s.Tracer.Start(workCtx, telemetry.SpanProjectorRun)
		defer span.End()
	}

	projectCtx, stopHeartbeat := s.startHeartbeat(workCtx, work, workerID)
	defer func() {
		_ = stopHeartbeat()
	}()

	// Large-generation semaphore: count facts first, acquire sem if large.
	releaseSem := s.acquireLargeGenSem(projectCtx, work, workerID)
	if releaseSem != nil {
		defer releaseSem()
	}

	loadStart := time.Now()
	factsForGeneration, err := s.FactStore.LoadFacts(projectCtx, work)
	if err != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if s.recordSupersededWork(workCtx, work, start, 0, heartbeatErr, workerID) {
				return nil
			}
			return errors.Join(err, heartbeatErr)
		}
		if projectorShutdownCanceled(workCtx, err) {
			s.recordProjectionShutdownCanceled(workCtx, work, start, 0, err, workerID)
			return nil
		}
		s.recordProjectionResult(workCtx, work, start, "failed", 0, err, workerID)
		if failErr := s.WorkSink.Fail(workCtx, work, err); failErr != nil {
			return errors.Join(err, fmt.Errorf("fail projector work: %w", failErr))
		}
		return nil
	}
	s.recordWorkStage(projectCtx, work, "load_facts", loadStart, len(factsForGeneration), workerID)

	projectStart := time.Now()
	result, err := s.Runner.Project(projectCtx, work.Scope, work.Generation, factsForGeneration)
	if err != nil {
		if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
			if s.recordSupersededWork(workCtx, work, start, len(factsForGeneration), heartbeatErr, workerID) {
				return nil
			}
			return errors.Join(err, heartbeatErr)
		}
		if projectorShutdownCanceled(workCtx, err) {
			s.recordProjectionShutdownCanceled(workCtx, work, start, len(factsForGeneration), err, workerID)
			return nil
		}
		s.recordProjectionResult(workCtx, work, start, "failed", len(factsForGeneration), err, workerID)
		if failErr := s.WorkSink.Fail(workCtx, work, err); failErr != nil {
			return errors.Join(err, fmt.Errorf("fail projector work: %w", failErr))
		}
		return nil
	}
	s.recordWorkStage(projectCtx, work, "project_generation", projectStart, len(factsForGeneration), workerID)
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		if s.recordSupersededWork(workCtx, work, start, len(factsForGeneration), heartbeatErr, workerID) {
			return nil
		}
		return heartbeatErr
	}

	ackCtx, cancelAck := projectorAckContext(workCtx)
	defer cancelAck()
	if err := s.WorkSink.Ack(ackCtx, work, result); err != nil {
		s.recordProjectionResult(ackCtx, work, start, "ack_failed", len(factsForGeneration), err, workerID)
		return fmt.Errorf("ack projector work: %w", err)
	}

	s.recordProjectionResult(ackCtx, work, start, "succeeded", len(factsForGeneration), nil, workerID)
	return nil
}

// projectorAckContext lets a completed projection publish its durable queue ack
// during owner shutdown without inheriting a canceled worker context.
func projectorAckContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), defaultProjectorAckShutdownTimeout)
}

func projectorClaimCanceled(ctx context.Context, err error) bool {
	return ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

type projectorHeartbeatStop func() error

func (s Service) startHeartbeat(ctx context.Context, work ScopeGenerationWork, workerID int) (context.Context, projectorHeartbeatStop) {
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
				if err := s.Heartbeater.Heartbeat(heartbeatCtx, work); err != nil {
					if heartbeatCtx.Err() != nil && errors.Is(err, heartbeatCtx.Err()) {
						done <- nil
						return
					}
					heartbeatErr = fmt.Errorf("heartbeat projector work: %w", err)
					if s.Logger != nil {
						scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
						logAttrs := make([]any, 0, len(scopeAttrs)+4)
						for _, a := range scopeAttrs {
							logAttrs = append(logAttrs, a)
						}
						logAttrs = append(logAttrs, slog.Int("worker_id", workerID))
						logAttrs = append(logAttrs, slog.Duration("heartbeat_interval", s.HeartbeatInterval))
						logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseProjection))
						logAttrs = append(logAttrs, telemetry.FailureClassAttr("lease_heartbeat_failure"))
						logAttrs = append(logAttrs, slog.String("error", heartbeatErr.Error()))
						s.Logger.ErrorContext(heartbeatCtx, "projector lease heartbeat failed", logAttrs...)
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

// InitLargeGenSemaphore sets up the large-generation semaphore. Call after
// setting FactCounter, LargeGenThreshold, and LargeGenMaxConcurrent.
func (s *Service) InitLargeGenSemaphore() {
	if s.FactCounter == nil {
		return
	}
	maxConcurrent := s.LargeGenMaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	s.largeSem = make(chan struct{}, maxConcurrent)
}

func (s Service) largeGenThreshold() int {
	if s.LargeGenThreshold <= 0 {
		return 10000
	}
	return s.LargeGenThreshold
}

func (s Service) acquireLargeGenSem(ctx context.Context, work ScopeGenerationWork, workerID int) func() {
	if s.FactCounter == nil || s.largeSem == nil {
		return nil
	}

	count, err := s.FactCounter.CountFacts(ctx, work.Scope.ScopeID, work.Generation.GenerationID)
	if err != nil {
		// On error, skip semaphore (don't block).
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "fact count failed, skipping large-gen semaphore",
				slog.String("error", err.Error()),
				slog.Int("worker_id", workerID),
			)
		}
		return nil
	}

	if count <= s.largeGenThreshold() {
		return nil // small/medium repo, no semaphore needed
	}

	if s.Logger != nil {
		scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
		logAttrs := make([]any, 0, len(scopeAttrs)+3)
		for _, a := range scopeAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs,
			slog.Int("fact_count", count),
			slog.Int("threshold", s.largeGenThreshold()),
			slog.Int("worker_id", workerID),
		)
		s.Logger.InfoContext(ctx, "large generation detected, acquiring semaphore", logAttrs...)
	}

	// Record semaphore wait time.
	waitStart := time.Now()
	select {
	case s.largeSem <- struct{}{}:
		if s.Instruments != nil {
			s.Instruments.LargeRepoSemaphoreWait.Record(ctx, time.Since(waitStart).Seconds())
		}
		if s.Logger != nil {
			s.Logger.InfoContext(ctx, "large generation semaphore acquired",
				slog.Int("fact_count", count),
				slog.Int("worker_id", workerID),
			)
		}
		return func() { <-s.largeSem }
	case <-ctx.Done():
		return nil
	}
}

func (s Service) validate() error {
	if s.WorkSource == nil {
		return errors.New("work source is required")
	}
	if s.FactStore == nil {
		return errors.New("fact store is required")
	}
	if s.Runner == nil {
		return errors.New("runner is required")
	}
	if s.WorkSink == nil {
		return errors.New("work sink is required")
	}

	return nil
}

func (s Service) pollInterval() time.Duration {
	if s.PollInterval <= 0 {
		return defaultPollInterval
	}

	return s.PollInterval
}

func (s Service) wait(ctx context.Context, interval time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, interval)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
