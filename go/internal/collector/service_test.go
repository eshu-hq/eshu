package collector

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
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
