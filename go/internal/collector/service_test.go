package collector

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestServiceRunCommitsCollectedGenerationViaDurableBoundary(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := &stubSource{
		collected: []CollectedGeneration{
			FactsFromSlice(scopeValue, generationValue, envelopes),
		},
	}
	committer := &stubCommitter{
		commit: func(
			_ context.Context,
			gotScope scope.IngestionScope,
			gotGeneration scope.ScopeGeneration,
			gotFactStream <-chan facts.Envelope,
		) error {
			cancel()

			if !reflect.DeepEqual(gotScope, scopeValue) {
				t.Fatalf(
					"CommitScopeGeneration() scope = %#v, want %#v",
					gotScope,
					scopeValue,
				)
			}
			if gotGeneration != generationValue {
				t.Fatalf(
					"CommitScopeGeneration() generation = %#v, want %#v",
					gotGeneration,
					generationValue,
				)
			}

			var gotFacts []facts.Envelope
			for f := range gotFactStream {
				gotFacts = append(gotFacts, f)
			}
			if len(gotFacts) != len(envelopes) {
				t.Fatalf(
					"CommitScopeGeneration() fact count = %d, want %d",
					len(gotFacts),
					len(envelopes),
				)
			}

			return nil
		},
	}

	service := Service{
		Source:       source,
		Committer:    committer,
		PollInterval: time.Millisecond,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := committer.calls, 1; got != want {
		t.Fatalf("CommitScopeGeneration() call count = %d, want %d", got, want)
	}
}

func TestServiceRunPropagatesDurableCommitErrors(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	wantErr := errors.New("commit failed")

	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{
				FactsFromSlice(scopeValue, generationValue, envelopes),
			},
		},
		Committer: &stubCommitter{
			commit: func(
				_ context.Context,
				_ scope.IngestionScope,
				_ scope.ScopeGeneration,
				factStream <-chan facts.Envelope,
			) error {
				// Drain the channel before returning error
				for range factStream {
				}
				return wantErr
			},
		},
		PollInterval: time.Millisecond,
	}

	err := service.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestServiceRunPreservesFactStreamErrorWhenCommitterIsNotStreamAware(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	streamErr := errors.New("fact replay failed")
	collected := FactsFromSlice(scopeValue, generationValue, envelopes)
	collected.FactStreamErr = func() error {
		return streamErr
	}

	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{collected},
		},
		Committer:    &stubCommitter{},
		PollInterval: time.Millisecond,
	}

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want stream-aware committer error")
	}
	if !errors.Is(err, streamErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, streamErr)
	}
	if !strings.Contains(err.Error(), "collector committer must support fact stream errors") {
		t.Fatalf("Run() error = %q, want committer support context", err)
	}
}

func TestServiceRunCallsAfterBatchDrainedOnceAfterCommittedBatch(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hookCalls := 0
	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{
				FactsFromSlice(scopeValue, generationValue, envelopes),
			},
			empty: func() {
				cancel()
			},
		},
		Committer:    &stubCommitter{},
		PollInterval: time.Millisecond,
		AfterBatchDrained: func(context.Context) error {
			hookCalls++
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := hookCalls, 1; got != want {
		t.Fatalf("AfterBatchDrained() calls = %d, want %d", got, want)
	}
}

func testCollectedGeneration() (
	scope.IngestionScope,
	scope.ScopeGeneration,
	[]facts.Envelope,
) {
	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 12, 1, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	envelopes := []facts.Envelope{{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:repo-123",
		ObservedAt:    generationValue.ObservedAt,
		Payload:       map[string]any{"graph_id": "repo-123"},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKey:      "fact-key",
		},
	}}

	return scopeValue, generationValue, envelopes
}

type stubSource struct {
	collected []CollectedGeneration
	index     int
	empty     func()
}

func (s *stubSource) Next(ctx context.Context) (CollectedGeneration, bool, error) {
	if s.index >= len(s.collected) {
		if s.empty != nil {
			s.empty()
			return CollectedGeneration{}, false, nil
		}
		<-ctx.Done()
		return CollectedGeneration{}, false, ctx.Err()
	}

	item := s.collected[s.index]
	s.index++
	return item, true, nil
}

type stubCommitter struct {
	calls  int
	commit func(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error
}

func (s *stubCommitter) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	s.calls++
	if s.commit != nil {
		return s.commit(ctx, scopeValue, generationValue, factStream)
	}
	// Drain the channel to prevent goroutine leaks
	for range factStream {
	}
	return nil
}

func TestServiceRunWithTelemetry(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup noop telemetry providers
	tracer := noop.NewTracerProvider().Tracer("test")
	meter := metricnoop.NewMeterProvider().Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logger := slog.Default()

	source := &stubSource{
		collected: []CollectedGeneration{
			FactsFromSlice(scopeValue, generationValue, envelopes),
		},
	}
	committer := &stubCommitter{
		commit: func(
			_ context.Context,
			gotScope scope.IngestionScope,
			gotGeneration scope.ScopeGeneration,
			gotFactStream <-chan facts.Envelope,
		) error {
			cancel()

			if !reflect.DeepEqual(gotScope, scopeValue) {
				t.Fatalf(
					"CommitScopeGeneration() scope = %#v, want %#v",
					gotScope,
					scopeValue,
				)
			}
			if gotGeneration != generationValue {
				t.Fatalf(
					"CommitScopeGeneration() generation = %#v, want %#v",
					gotGeneration,
					generationValue,
				)
			}

			var gotFacts []facts.Envelope
			for f := range gotFactStream {
				gotFacts = append(gotFacts, f)
			}
			if len(gotFacts) != len(envelopes) {
				t.Fatalf(
					"CommitScopeGeneration() fact count = %d, want %d",
					len(gotFacts),
					len(envelopes),
				)
			}

			return nil
		},
	}

	service := Service{
		Source:       source,
		Committer:    committer,
		PollInterval: time.Millisecond,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := committer.calls, 1; got != want {
		t.Fatalf("CommitScopeGeneration() call count = %d, want %d", got, want)
	}
}

func TestServiceMetricsUseCollectedScopeCollectorKind(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	scopeValue.SourceSystem = "confluence"
	scopeValue.ScopeKind = scope.KindDocumentationSource
	scopeValue.CollectorKind = scope.CollectorDocumentation

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	service := Service{
		Committer:    &stubCommitter{},
		Instruments:  instruments,
		PollInterval: time.Millisecond,
	}
	collected := FactsFromSlice(scopeValue, generationValue, envelopes)
	if err := service.commitWithTelemetry(context.Background(), collected); err != nil {
		t.Fatalf("commitWithTelemetry() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got := collectorCounterValue(t, rm, "eshu_dp_facts_emitted_total", map[string]string{
		"scope_id":       scopeValue.ScopeID,
		"source_system":  "confluence",
		"collector_kind": string(scope.CollectorDocumentation),
	}); got != int64(len(envelopes)) {
		t.Fatalf("eshu_dp_facts_emitted_total = %d, want %d", got, len(envelopes))
	}
}

func TestServiceRunNilTelemetry(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := &stubSource{
		collected: []CollectedGeneration{
			FactsFromSlice(scopeValue, generationValue, envelopes),
		},
	}
	committer := &stubCommitter{
		commit: func(
			_ context.Context,
			gotScope scope.IngestionScope,
			gotGeneration scope.ScopeGeneration,
			gotFactStream <-chan facts.Envelope,
		) error {
			cancel()

			if !reflect.DeepEqual(gotScope, scopeValue) {
				t.Fatalf(
					"CommitScopeGeneration() scope = %#v, want %#v",
					gotScope,
					scopeValue,
				)
			}
			if gotGeneration != generationValue {
				t.Fatalf(
					"CommitScopeGeneration() generation = %#v, want %#v",
					gotGeneration,
					generationValue,
				)
			}

			var gotFacts []facts.Envelope
			for f := range gotFactStream {
				gotFacts = append(gotFacts, f)
			}
			if len(gotFacts) != len(envelopes) {
				t.Fatalf(
					"CommitScopeGeneration() fact count = %d, want %d",
					len(gotFacts),
					len(envelopes),
				)
			}

			return nil
		},
	}

	// Service with nil telemetry fields (existing behavior)
	service := Service{
		Source:       source,
		Committer:    committer,
		PollInterval: time.Millisecond,
		Tracer:       nil,
		Instruments:  nil,
		Logger:       nil,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := committer.calls, 1; got != want {
		t.Fatalf("CommitScopeGeneration() call count = %d, want %d", got, want)
	}
}

func collectorCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}

			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf(
					"metric %s data = %T, want metricdata.Sum[int64]",
					metricName,
					metricRecord.Data,
				)
			}

			for _, dp := range sum.DataPoints {
				if collectorHasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func collectorHasAttrs(actual []attribute.KeyValue, want map[string]string) bool {
	matched := 0
	for _, attr := range actual {
		wantValue, ok := want[string(attr.Key)]
		if !ok {
			continue
		}
		if wantValue != attr.Value.AsString() {
			return false
		}
		matched++
	}
	return matched == len(want)
}
