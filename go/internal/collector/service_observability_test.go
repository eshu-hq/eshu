package collector

import (
	"context"
	"testing"
	"time"

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

type spanCheckingSource struct {
	collected CollectedGeneration
	called    bool
	sawSpan   bool
}

func (s *spanCheckingSource) Next(ctx context.Context) (CollectedGeneration, bool, error) {
	if !s.called {
		s.called = true
		s.sawSpan = oteltrace.SpanContextFromContext(ctx).IsValid()
		return s.collected, true, nil
	}
	<-ctx.Done()
	return CollectedGeneration{}, false, ctx.Err()
}
