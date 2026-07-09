// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Source yields one collected scope generation at a time for durable commit.
type Source interface {
	Next(context.Context) (CollectedGeneration, bool, error)
}

// StartObserveFunc starts a collector observe span around source work that has
// proven it is attempting a generation instead of reporting an idle poll.
type StartObserveFunc func(context.Context) CollectorObservation

// CollectorObservation carries the context and start time for one
// collector.observe span. Sources that implement ObservedSource return this
// value so Service can finish the same span after durable commit.
type CollectorObservation struct {
	Context   context.Context
	Span      trace.Span
	StartedAt time.Time
}

// ObservedSource lets a source delay collector.observe creation until it knows
// the poll is a real collection attempt. This avoids emitting trace spans for
// idle polls while still allowing synchronous sources to include source reads
// in the same span as durable commit.
type ObservedSource interface {
	NextObserved(context.Context, StartObserveFunc) (CollectedGeneration, bool, CollectorObservation, error)
}

// CollectedGeneration is one repo-scoped source generation gathered by the
// collector boundary. Facts are streamed through a channel so memory stays
// proportional to the batch size, not the total number of facts per repo.
type CollectedGeneration struct {
	Scope      scope.IngestionScope
	Generation scope.ScopeGeneration
	Facts      <-chan facts.Envelope
	// EstimatedFactCount is a conservative pre-computed estimate from metadata
	// counts. Use FactCount() for the best available count (exact after the
	// stream drains).
	EstimatedFactCount int
	// factCountAtomic is set by streaming collectors that emit through a
	// goroutine. The goroutine increments it per emitted envelope; after the
	// Facts channel is drained, Load() returns the exact total.
	factCountAtomic *atomic.Int64
	// FactStreamErr reports asynchronous fact stream failures after Facts has
	// closed. Committers that receive this callback must check it before
	// committing durable state.
	FactStreamErr func() error
	// Unchanged means a claimed source proved the work item has no new facts to
	// commit, but the durable claim should still be completed.
	Unchanged bool
	// DiscoveryAdvisory is optional focused-run tuning evidence for the
	// collected repository. It is not persisted with facts.
	DiscoveryAdvisory *DiscoveryAdvisoryReport
}

// FactCount returns the best available fact count. When a streaming goroutine
// is active, this returns the larger of the pre-computed estimate and the atomic
// counter (which starts at 0 and is incremented per emitted envelope). Before
// the goroutine sends its first envelope the estimate is returned; after the
// Facts channel drains the atomic holds the exact total.
func (cg CollectedGeneration) FactCount() int {
	if cg.factCountAtomic != nil {
		atomicVal := int(cg.factCountAtomic.Load())
		if atomicVal > cg.EstimatedFactCount {
			return atomicVal
		}
	}
	return cg.EstimatedFactCount
}

// FactsFromSlice creates a CollectedGeneration with facts from a pre-built
// slice. The returned channel is pre-filled and closed, so it can be consumed
// immediately without a background goroutine. Used in tests and for small
// fact sets where streaming overhead isn't warranted.
func FactsFromSlice(
	s scope.IngestionScope,
	g scope.ScopeGeneration,
	envs []facts.Envelope,
) CollectedGeneration {
	ch := make(chan facts.Envelope, len(envs))
	for _, e := range envs {
		ch <- e
	}
	close(ch)
	return CollectedGeneration{Scope: s, Generation: g, Facts: ch, EstimatedFactCount: len(envs)}
}

// Committer owns the collector durable write boundary.
type Committer interface {
	CommitScopeGeneration(
		context.Context,
		scope.IngestionScope,
		scope.ScopeGeneration,
		<-chan facts.Envelope,
	) error
}

// StreamErrorCommitter persists generations and can fail the same transaction
// if a producer reports a fact stream error after closing the fact channel.
type StreamErrorCommitter interface {
	CommitScopeGenerationWithStreamError(
		context.Context,
		scope.IngestionScope,
		scope.ScopeGeneration,
		<-chan facts.Envelope,
		func() error,
	) error
}

// ClaimedCommitter can verify workflow claim fencing in the same durable
// transaction that persists facts for a claimed collector item.
type ClaimedCommitter interface {
	CommitClaimedScopeGeneration(
		context.Context,
		workflow.ClaimMutation,
		scope.IngestionScope,
		scope.ScopeGeneration,
		<-chan facts.Envelope,
	) error
}

// StreamErrorClaimedCommitter persists claimed generations and can fail the
// same transaction if a producer reports a fact stream error after closing the
// fact channel.
type StreamErrorClaimedCommitter interface {
	CommitClaimedScopeGenerationWithStreamError(
		context.Context,
		workflow.ClaimMutation,
		scope.IngestionScope,
		scope.ScopeGeneration,
		<-chan facts.Envelope,
		func() error,
	) error
}

// Service coordinates collector-owned collection with the durable commit seam.
type Service struct {
	Source       Source
	Committer    Committer
	DeadLetters  GenerationDeadLetterSink // optional durable quarantine for commit failures
	PollInterval time.Duration
	// AfterBatchDrained runs once after at least one committed generation and
	// the current source batch is exhausted.
	AfterBatchDrained func(context.Context) error
	// AfterEmptyBatchDrained also runs AfterBatchDrained once when the current
	// source batch is exhausted without commits. Repeated idle polls are
	// suppressed until another generation is committed. Use only for runtimes
	// that need configured empty shards to participate in a fleet barrier.
	AfterEmptyBatchDrained bool
	Tracer                 trace.Tracer           // optional — nil means no tracing
	Instruments            *telemetry.Instruments // optional — nil means no metrics
	Logger                 *slog.Logger           // optional — nil means no structured logging
}

// Run polls the source and commits each collected generation atomically.
func (s Service) Run(ctx context.Context) error {
	if s.Source == nil {
		return errors.New("collector source is required")
	}
	if s.Committer == nil {
		return errors.New("collector committer is required")
	}
	if s.PollInterval <= 0 {
		return errors.New("collector poll interval must be positive")
	}

	committedSinceDrain := false
	emptyDrainObserved := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		collected, ok, observation, err := s.nextWithObservation(ctx)
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				s.endCollectorObserve(observation, nil)
				return nil
			}
			s.endCollectorObserve(observation, err)
			return fmt.Errorf("collect scope generation: %w", err)
		}
		if !ok {
			s.endCollectorObserve(observation, nil)
			shouldDrain := committedSinceDrain || (s.AfterEmptyBatchDrained && !emptyDrainObserved)
			if shouldDrain && s.AfterBatchDrained != nil {
				if err := s.AfterBatchDrained(ctx); err != nil {
					return fmt.Errorf("after collector batch drained: %w", err)
				}
				committedSinceDrain = false
				emptyDrainObserved = true
			}
			if err := waitForNextPoll(ctx, s.PollInterval); err != nil {
				return nil
			}
			continue
		}

		if observation.Context == nil {
			observation = s.startCollectorObserve(ctx)
		}
		s.annotateCollectorObserve(observation, collected)
		if commitErr := s.commitWithTelemetry(observation.Context, collected, observation.StartedAt); commitErr != nil {
			retryable := IsRetryable(commitErr)
			storeErr := s.recordGenerationDeadLetterOnly(observation.Context, collected, "commit_failure", commitErr)
			terminal := errors.Join(commitErr, storeErr)
			s.endCollectorObserve(observation, terminal)
			// A retryable commit failure is quarantined for durable replay and
			// must not tear down the collector (and, through compositeRunner,
			// the projector running alongside it). Continue polling so a
			// transient fault in one generation cannot stop ingestion. A
			// dead-letter store failure is fatal infrastructure breakage and
			// still propagates regardless of the commit error's class.
			//
			// Require an actual durable quarantine: s.DeadLetters must be
			// non-nil so there is a real replay record. Without a sink there is
			// nothing to replay; silently continuing would drop the commit error.
			if retryable && s.DeadLetters != nil && storeErr == nil {
				s.logRetryableCommit(observation.Context, collected, commitErr)
				if err := waitForNextPoll(ctx, s.PollInterval); err != nil {
					return nil
				}
				continue
			}
			return terminal
		}
		if err := s.completeGenerationDeadLetterReplay(observation.Context, collected); err != nil {
			s.endCollectorObserve(observation, err)
			return err
		}
		s.endCollectorObserve(observation, nil)
		committedSinceDrain = true
		emptyDrainObserved = false
	}
}

func (s Service) nextWithObservation(ctx context.Context) (
	CollectedGeneration,
	bool,
	CollectorObservation,
	error,
) {
	if observed, ok := s.Source.(ObservedSource); ok {
		return observed.NextObserved(ctx, s.startCollectorObserve)
	}
	collected, ok, err := s.Source.Next(ctx)
	return collected, ok, CollectorObservation{}, err
}

func (s Service) startCollectorObserve(ctx context.Context) CollectorObservation {
	observeStartedAt := time.Now()
	if s.Tracer != nil {
		observedCtx, span := s.Tracer.Start(ctx, telemetry.SpanCollectorObserve)
		return CollectorObservation{
			Context:   observedCtx,
			Span:      span,
			StartedAt: observeStartedAt,
		}
	}
	return CollectorObservation{
		Context:   ctx,
		StartedAt: observeStartedAt,
	}
}

func (s Service) annotateCollectorObserve(observation CollectorObservation, collected CollectedGeneration) {
	if observation.Span == nil {
		return
	}
	observation.Span.SetAttributes(
		telemetry.AttrScopeID(collected.Scope.ScopeID),
		telemetry.AttrSourceSystem(collected.Scope.SourceSystem),
		telemetry.AttrCollectorKind(string(collected.Scope.CollectorKind)),
	)
}

func (s Service) endCollectorObserve(observation CollectorObservation, err error) {
	if observation.Span == nil {
		return
	}
	if err != nil {
		observation.Span.RecordError(err)
		observation.Span.SetStatus(codes.Error, err.Error())
	}
	observation.Span.End()
}

// recordGenerationDeadLetterOnly quarantines a failed generation for durable
// replay and returns only the dead-letter store error. It returns nil when no
// dead-letter sink is configured or the record was written. The caller decides
// whether the originating commit error is fatal or retryable; a non-nil return
// here always signals fatal dead-letter infrastructure breakage.
func (s Service) recordGenerationDeadLetterOnly(
	ctx context.Context,
	collected CollectedGeneration,
	failureClass string,
	cause error,
) error {
	if s.DeadLetters == nil {
		return nil
	}

	record := GenerationDeadLetter{
		Scope:            collected.Scope,
		Generation:       collected.Generation,
		FailureClass:     failureClass,
		FailureMessage:   cause.Error(),
		PayloadReference: generationDeadLetterPayloadReference(collected.Scope, collected.Generation),
		DeadLetteredAt:   time.Now().UTC(),
	}
	if err := s.DeadLetters.RecordGenerationDeadLetter(ctx, record); err != nil {
		return fmt.Errorf("record generation dead-letter: %w", err)
	}
	return nil
}

// logRetryableCommit emits an operator-facing record that a transient commit
// failure was quarantined for durable replay and that the collector kept
// running instead of tearing down. It lets an operator distinguish a retried
// generation from a fatal teardown at 3 AM.
func (s Service) logRetryableCommit(ctx context.Context, collected CollectedGeneration, cause error) {
	if s.Logger == nil {
		return
	}
	scopeAttrs := telemetry.ScopeAttrs(
		collected.Scope.ScopeID,
		collected.Generation.GenerationID,
		collected.Scope.SourceSystem,
	)
	logAttrs := make([]any, 0, len(scopeAttrs)+4)
	for _, a := range scopeAttrs {
		logAttrs = append(logAttrs, a)
	}
	logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseEmission))
	logAttrs = append(logAttrs, telemetry.FailureClassAttr("commit_retryable"))
	logAttrs = append(logAttrs, slog.Bool("retryable", true))
	logAttrs = append(logAttrs, log.Err(cause))
	s.Logger.WarnContext(ctx, "collector commit retryable; quarantined for replay, continuing", logAttrs...)
}

func (s Service) completeGenerationDeadLetterReplay(ctx context.Context, collected CollectedGeneration) error {
	completer, ok := s.DeadLetters.(GenerationDeadLetterReplayCompleter)
	if !ok {
		return nil
	}
	completionCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		generationDeadLetterReplayCompletionTimeout,
	)
	defer cancel()
	err := completer.CompleteGenerationDeadLetterReplay(completionCtx, GenerationDeadLetterReplayCompletion{
		Scope:       collected.Scope,
		Generation:  collected.Generation,
		CompletedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("complete generation replay: %w", err)
	}
	return nil
}

func (s Service) commitWithTelemetry(ctx context.Context, collected CollectedGeneration, startedAt time.Time) error {
	var err error
	if collected.FactStreamErr != nil {
		streamCommitter, ok := s.Committer.(StreamErrorCommitter)
		if !ok {
			err = errors.New("collector committer must support fact stream errors")
			if streamErr := cleanupCollectedFactStream(collected); streamErr != nil {
				err = errors.Join(err, streamErr)
			}
		} else {
			err = streamCommitter.CommitScopeGenerationWithStreamError(
				ctx,
				collected.Scope,
				collected.Generation,
				collected.Facts,
				collected.FactStreamErr,
			)
		}
	} else {
		err = s.Committer.CommitScopeGeneration(
			ctx,
			collected.Scope,
			collected.Generation,
			collected.Facts,
		)
	}

	// After the commit drain, FactCount() returns the exact streamed count
	// (via the atomic populated by the streaming goroutine) or the
	// pre-computed estimate for non-streaming collectors.
	factCount := int64(collected.FactCount())

	duration := time.Since(startedAt).Seconds()

	if s.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrScopeKind(string(collected.Scope.ScopeKind)),
			telemetry.AttrSourceSystem(collected.Scope.SourceSystem),
			telemetry.AttrCollectorKind(string(collected.Scope.CollectorKind)),
		)
		s.Instruments.CollectorObserveDuration.Record(ctx, duration, attrs)
		s.Instruments.FactsEmitted.Add(ctx, factCount, attrs)
		s.Instruments.GenerationFactCount.Record(ctx, float64(factCount), attrs)
		if err == nil {
			s.Instruments.FactsCommitted.Add(ctx, factCount, attrs)
		}
	}

	if s.Logger != nil {
		scopeAttrs := telemetry.ScopeAttrs(
			collected.Scope.ScopeID,
			collected.Generation.GenerationID,
			collected.Scope.SourceSystem,
		)
		logAttrs := make([]any, 0, len(scopeAttrs)+2)
		for _, a := range scopeAttrs {
			logAttrs = append(logAttrs, a)
		}
		logAttrs = append(logAttrs, slog.Int("fact_count", collected.FactCount()))
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))

		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseEmission))
		if err != nil {
			logAttrs = append(logAttrs, log.Err(err))
			logAttrs = append(logAttrs, telemetry.FailureClassAttr("commit_failure"))
			s.Logger.ErrorContext(ctx, "collector commit failed", logAttrs...)
		} else {
			s.Logger.InfoContext(ctx, "collector commit succeeded", logAttrs...)
		}
	}

	if err != nil {
		return fmt.Errorf("commit scope generation: %w", err)
	}
	return nil
}

func drainFactStream(factStream <-chan facts.Envelope) {
	if factStream == nil {
		return
	}
	for range factStream {
	}
}

func waitForNextPoll(ctx context.Context, pollInterval time.Duration) error {
	timer := time.NewTimer(pollInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
