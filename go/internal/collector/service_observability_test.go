package collector

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestServiceRunCollectorObserveSpanWrapsSourceNextAndCommit(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	source := &spanCheckingSource{
		collected: FactsFromSlice(scopeValue, generationValue, envelopes),
	}

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	defer func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	committer := &stubCommitter{
		commit: func(
			ctx context.Context,
			_ scope.IngestionScope,
			_ scope.ScopeGeneration,
			factStream <-chan facts.Envelope,
		) error {
			if !oteltrace.SpanContextFromContext(ctx).IsValid() {
				t.Fatal("CommitScopeGeneration() context has no active collector.observe span")
			}
			for range factStream {
			}
			cancel()
			return nil
		},
	}

	service := Service{
		Source:       source,
		Committer:    committer,
		PollInterval: time.Millisecond,
		Tracer:       tracerProvider.Tracer(telemetry.DefaultSignalName),
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if !source.sawSpan {
		t.Fatal("Source.Next() context has no active collector.observe span")
	}

	var collectorObserveSpans int
	for _, span := range spanRecorder.Ended() {
		if span.Name() == telemetry.SpanCollectorObserve {
			collectorObserveSpans++
		}
	}
	if collectorObserveSpans != 1 {
		t.Fatalf("collector.observe spans = %d, want 1", collectorObserveSpans)
	}
}

func TestServiceRunDoesNotEmitCollectorObserveSpanForIdlePoll(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	defer func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := Service{
		Source: &stubSource{
			empty: cancel,
		},
		Committer:    &stubCommitter{},
		PollInterval: time.Millisecond,
		Tracer:       tracerProvider.Tracer(telemetry.DefaultSignalName),
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	for _, span := range spanRecorder.Ended() {
		if span.Name() == telemetry.SpanCollectorObserve {
			t.Fatal("collector.observe span emitted for idle poll")
		}
	}
}

func TestServiceRunDoesNotMarkGracefulSourceCancellationAsSpanError(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	defer func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := Service{
		Source:       &cancelingObservedSource{cancel: cancel},
		Committer:    &stubCommitter{},
		PollInterval: time.Millisecond,
		Tracer:       tracerProvider.Tracer(telemetry.DefaultSignalName),
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var collectorObserveSpans int
	for _, span := range spanRecorder.Ended() {
		if span.Name() != telemetry.SpanCollectorObserve {
			continue
		}
		collectorObserveSpans++
		if span.Status().Code == codes.Error {
			t.Fatal("collector.observe span marked graceful cancellation as error")
		}
	}
	if collectorObserveSpans != 1 {
		t.Fatalf("collector.observe spans = %d, want 1", collectorObserveSpans)
	}
}

type spanCheckingSource struct {
	collected CollectedGeneration
	called    bool
	sawSpan   bool
}

func (s *spanCheckingSource) Next(context.Context) (CollectedGeneration, bool, error) {
	panic("spanCheckingSource must be called through NextObserved")
}

func (s *spanCheckingSource) NextObserved(
	ctx context.Context,
	startObserve StartObserveFunc,
) (CollectedGeneration, bool, CollectorObservation, error) {
	if !s.called {
		s.called = true
		observation := startObserve(ctx)
		s.sawSpan = oteltrace.SpanContextFromContext(observation.Context).IsValid()
		return s.collected, true, observation, nil
	}
	<-ctx.Done()
	return CollectedGeneration{}, false, CollectorObservation{}, ctx.Err()
}

type cancelingObservedSource struct {
	cancel context.CancelFunc
}

func (s *cancelingObservedSource) Next(context.Context) (CollectedGeneration, bool, error) {
	panic("cancelingObservedSource must be called through NextObserved")
}

func (s *cancelingObservedSource) NextObserved(
	ctx context.Context,
	startObserve StartObserveFunc,
) (CollectedGeneration, bool, CollectorObservation, error) {
	observation := startObserve(ctx)
	s.cancel()
	return CollectedGeneration{}, false, observation, context.Canceled
}
