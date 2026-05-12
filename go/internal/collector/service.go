package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Source yields one collected scope generation at a time for durable commit.
type Source interface {
	Next(context.Context) (CollectedGeneration, bool, error)
}

// CollectedGeneration is one repo-scoped source generation gathered by the
// collector boundary. Facts are streamed through a channel so memory stays
// proportional to the batch size, not the total number of facts per repo.
type CollectedGeneration struct {
	Scope      scope.IngestionScope
	Generation scope.ScopeGeneration
	Facts      <-chan facts.Envelope
	FactCount  int // estimated total for telemetry (may be approximate)
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
	return CollectedGeneration{Scope: s, Generation: g, Facts: ch, FactCount: len(envs)}
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
	PollInterval time.Duration
	// AfterBatchDrained runs once after at least one committed generation and
	// the current source batch is exhausted.
	AfterBatchDrained func(context.Context) error
	Tracer            trace.Tracer           // optional — nil means no tracing
	Instruments       *telemetry.Instruments // optional — nil means no metrics
	Logger            *slog.Logger           // optional — nil means no structured logging
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
	for {
		if ctx.Err() != nil {
			return nil
		}
		observeCtx, observeSpan, observedAt := s.startCollectorObserve(ctx)
		collected, ok, err := s.Source.Next(observeCtx)
		if err != nil {
			s.endCollectorObserve(observeSpan, err)
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return nil
			}
			return fmt.Errorf("collect scope generation: %w", err)
		}
		if !ok {
			s.endCollectorObserve(observeSpan, nil)
			if committedSinceDrain && s.AfterBatchDrained != nil {
				if err := s.AfterBatchDrained(ctx); err != nil {
					return fmt.Errorf("after collector batch drained: %w", err)
				}
				committedSinceDrain = false
			}
			if err := waitForNextPoll(ctx, s.PollInterval); err != nil {
				return nil
			}
			continue
		}

		s.annotateCollectorObserve(observeSpan, collected)
		if err := s.commitWithTelemetry(observeCtx, collected, observedAt); err != nil {
			s.endCollectorObserve(observeSpan, err)
			return err
		}
		s.endCollectorObserve(observeSpan, nil)
		committedSinceDrain = true
	}
}

func (s Service) startCollectorObserve(ctx context.Context) (context.Context, trace.Span, time.Time) {
	start := time.Now()
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanCollectorObserve)
		return ctx, span, start
	}
	return ctx, nil, start
}

func (s Service) annotateCollectorObserve(span trace.Span, collected CollectedGeneration) {
	if span == nil {
		return
	}
	span.SetAttributes(
		telemetry.AttrScopeID(collected.Scope.ScopeID),
		telemetry.AttrSourceSystem(collected.Scope.SourceSystem),
		telemetry.AttrCollectorKind(string(collected.Scope.CollectorKind)),
	)
}

func (s Service) endCollectorObserve(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func (s Service) commitWithTelemetry(ctx context.Context, collected CollectedGeneration, startedAt time.Time) error {
	factCount := int64(collected.FactCount)

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

	duration := time.Since(startedAt).Seconds()

	if s.Instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrScopeID(collected.Scope.ScopeID),
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
		logAttrs = append(logAttrs, slog.Int("fact_count", collected.FactCount))
		logAttrs = append(logAttrs, slog.Float64("duration_seconds", duration))

		logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseEmission))
		if err != nil {
			logAttrs = append(logAttrs, slog.String("error", err.Error()))
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
