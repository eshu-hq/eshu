package projector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestServiceRunClaimsLoadsProjectsAndAcknowledges(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	source := &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}}
	factStore := &stubFactStore{
		facts: []facts.Envelope{{
			FactID:       "fact-1",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKind:     "source_node",
		}},
	}
	runner := &stubProjectionRunner{
		result: Result{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
		},
	}
	sink := &stubProjectorWorkSink{}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   source,
		FactStore:    factStore,
		Runner:       runner,
		WorkSink:     sink,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got, want := source.claimCalls, 2; got != want {
		t.Fatalf("claim calls = %d, want %d", got, want)
	}
	if got, want := factStore.loadCalls, 1; got != want {
		t.Fatalf("load calls = %d, want %d", got, want)
	}
	if got, want := runner.runCalls, 1; got != want {
		t.Fatalf("runner calls = %d, want %d", got, want)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
}

func TestServiceRunTreatsRetriedFirstGenerationAsCleanupRequired(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-retry",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-retry",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-retry",
			GenerationID: "generation-retry",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
		AttemptCount: 2,
	}
	runner := &stubProjectionRunner{
		result: Result{
			ScopeID:      "scope-retry",
			GenerationID: "generation-retry",
		},
	}
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore:    &stubFactStore{},
		Runner:       runner,
		WorkSink:     &stubProjectorWorkSink{},
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if !runner.lastScope.HasPriorGeneration() {
		t.Fatal("runner scope HasPriorGeneration() = false, want true for retried first-generation work")
	}
	if work.Scope.HasPriorGeneration() {
		t.Fatal("test setup invalid: original work scope should not report a prior generation")
	}
}

func TestServiceRunLogsFactLoadAndProjectionStages(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}
	var logs bytes.Buffer
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore: &stubFactStore{facts: []facts.Envelope{{
			FactID:       "fact-1",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKind:     "source_node",
		}}},
		Runner: &stubProjectionRunner{result: Result{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
		}},
		WorkSink: &stubProjectorWorkSink{},
		Wait:     func(context.Context, time.Duration) error { return context.Canceled },
		Logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	output := logs.String()
	for _, want := range []string{
		`"msg":"projector work stage completed"`,
		`"stage":"load_facts"`,
		`"stage":"project_generation"`,
		`"fact_count":1`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("projector stage logs missing %s:\n%s", want, output)
		}
	}
}

func TestServiceRunMarksFailureWhenProjectionFails(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	wantErr := errors.New("projection failed")
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore:    &stubFactStore{},
		Runner:       &stubProjectionRunner{runErr: wantErr},
		WorkSink:     &stubProjectorWorkSink{},
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	sink := service.WorkSink.(*stubProjectorWorkSink)

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got, want := sink.ackCalls, 0; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 1; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
	if !errors.Is(sink.failedWith, wantErr) {
		t.Fatalf("failed error = %v, want %v", sink.failedWith, wantErr)
	}
}

func TestServiceRunHeartbeatsLongRunningProjection(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	runner := &stubProjectionRunner{
		result: Result{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
		},
		blockFor: 35 * time.Millisecond,
	}
	heartbeater := &stubProjectorWorkHeartbeater{}
	sink := &stubProjectorWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore:         &stubFactStore{},
		Runner:            runner,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: 5 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got := heartbeater.calls; got < 2 {
		t.Fatalf("heartbeat calls = %d, want at least 2", got)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
}

func TestServiceRunStopsWhenHeartbeatLosesLease(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	wantErr := errors.New("lease lost")
	runner := &stubProjectionRunner{
		waitForContextCancellation: true,
	}
	heartbeater := &stubProjectorWorkHeartbeater{
		failAfter: 1,
		err:       wantErr,
	}
	sink := &stubProjectorWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore:         &stubFactStore{},
		Runner:            runner,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: 5 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want error containing %v", err, wantErr)
	}
	if got, want := sink.ackCalls, 0; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
}

func TestServiceRunAcksWithLiveContextAfterHeartbeatStops(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	runner := &stubProjectionRunner{
		result: Result{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
		},
		blockFor: 15 * time.Millisecond,
	}
	heartbeater := &stubProjectorWorkHeartbeater{}
	sink := &stubProjectorWorkSink{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore:         &stubFactStore{},
		Runner:            runner,
		WorkSink:          sink,
		Heartbeater:       heartbeater,
		HeartbeatInterval: 5 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if sink.ackCtxErr != nil {
		t.Fatalf("ack context error = %v, want nil", sink.ackCtxErr)
	}
}

func TestServiceHeartbeatStopIgnoresStopContextCancellation(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}
	heartbeater := &contextCanceledProjectorWorkHeartbeater{
		entered: make(chan struct{}),
	}

	service := Service{
		Heartbeater:       heartbeater,
		HeartbeatInterval: time.Millisecond,
	}
	_, stopHeartbeat := service.startHeartbeat(context.Background(), work, 0)

	select {
	case <-heartbeater.entered:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not start")
	}

	if err := stopHeartbeat(); err != nil {
		t.Fatalf("stopHeartbeat() error = %v, want nil", err)
	}
}

type stubProjectorWorkSource struct {
	mu                           sync.Mutex
	claimCalls                   int
	workItems                    []ScopeGenerationWork
	returnContextErrWhenCanceled bool
}

func (s *stubProjectorWorkSource) Claim(ctx context.Context) (ScopeGenerationWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls++
	if s.returnContextErrWhenCanceled && ctx.Err() != nil {
		return ScopeGenerationWork{}, false, ctx.Err()
	}
	if len(s.workItems) == 0 {
		return ScopeGenerationWork{}, false, nil
	}

	work := s.workItems[0]
	s.workItems = s.workItems[1:]
	return work, true, nil
}

type stubFactStore struct {
	mu               sync.Mutex
	loadCalls        int
	facts            []facts.Envelope
	returnContextErr bool
}

func (s *stubFactStore) LoadFacts(ctx context.Context, _ ScopeGenerationWork) ([]facts.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadCalls++
	if s.returnContextErr {
		return nil, fmt.Errorf("list facts: %w", ctx.Err())
	}
	cloned := make([]facts.Envelope, len(s.facts))
	copy(cloned, s.facts)
	return cloned, nil
}

type stubProjectionRunner struct {
	mu                         sync.Mutex
	runCalls                   int
	result                     Result
	runErr                     error
	beforeReturn               func(context.Context)
	failAfter                  int
	blockFor                   time.Duration
	waitForContextCancellation bool
	lastScope                  scope.IngestionScope
}

func (s *stubProjectionRunner) Project(ctx context.Context, scopeValue scope.IngestionScope, generation scope.ScopeGeneration, inputFacts []facts.Envelope) (Result, error) {
	s.mu.Lock()
	s.runCalls++
	s.lastScope = scopeValue
	runCalls := s.runCalls
	blockFor := s.blockFor
	waitForContextCancellation := s.waitForContextCancellation
	result := s.result
	runErr := s.runErr
	beforeReturn := s.beforeReturn
	failAfter := s.failAfter
	s.mu.Unlock()

	if failAfter > 0 && runCalls > failAfter {
		return Result{}, errors.New("executor failed after threshold")
	}
	if waitForContextCancellation {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}
	if blockFor > 0 {
		timer := time.NewTimer(blockFor)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-timer.C:
		}
	}
	if failAfter > 0 && runCalls > failAfter {
		return Result{}, errors.New("executor failed after threshold")
	}
	if beforeReturn != nil {
		beforeReturn(ctx)
	}
	return result, runErr
}

type stubProjectorWorkSink struct {
	mu         sync.Mutex
	ackCalls   int
	ackCtxErr  error
	failCalls  int
	failedWith error
	ackErr     error
	beforeAck  func(context.Context)
	failAfter  int
}

func (s *stubProjectorWorkSink) Ack(ctx context.Context, _ ScopeGenerationWork, _ Result) error {
	if s.beforeAck != nil {
		s.beforeAck(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls++
	s.ackCtxErr = ctx.Err()
	if s.failAfter > 0 && s.ackCalls > s.failAfter {
		return errors.New("ack failed after threshold")
	}
	return s.ackErr
}

func (s *stubProjectorWorkSink) Fail(_ context.Context, _ ScopeGenerationWork, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCalls++
	s.failedWith = err
	return nil
}

type stubProjectorWorkHeartbeater struct {
	mu        sync.Mutex
	calls     int
	failAfter int
	err       error
}

func (s *stubProjectorWorkHeartbeater) Heartbeat(context.Context, ScopeGenerationWork) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.failAfter > 0 && s.calls >= s.failAfter {
		return s.err
	}
	return nil
}

type contextCanceledProjectorWorkHeartbeater struct {
	entered chan struct{}
	once    sync.Once
}

func (s *contextCanceledProjectorWorkHeartbeater) Heartbeat(ctx context.Context, _ ScopeGenerationWork) error {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return ctx.Err()
}

func TestServiceRunWithTelemetry(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	tracer := noop.NewTracerProvider().Tracer("test")
	meter := metricnoop.NewMeterProvider().Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	logger := slog.Default()

	source := &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}}
	factStore := &stubFactStore{
		facts: []facts.Envelope{{
			FactID:       "fact-1",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKind:     "source_node",
		}},
	}
	runner := &stubProjectionRunner{
		result: Result{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
		},
	}
	sink := &stubProjectorWorkSink{}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   source,
		FactStore:    factStore,
		Runner:       runner,
		WorkSink:     sink,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got, want := source.claimCalls, 2; got != want {
		t.Fatalf("claim calls = %d, want %d", got, want)
	}
	if got, want := factStore.loadCalls, 1; got != want {
		t.Fatalf("load calls = %d, want %d", got, want)
	}
	if got, want := runner.runCalls, 1; got != want {
		t.Fatalf("runner calls = %d, want %d", got, want)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
}

func TestServiceRunConcurrentMultipleItems(t *testing.T) {
	t.Parallel()

	workItems := []ScopeGenerationWork{
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-1",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-1",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-1",
				GenerationID: "gen-1",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-2",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-2",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-2",
				GenerationID: "gen-2",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-3",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-3",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-3",
				GenerationID: "gen-3",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-4",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-4",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-4",
				GenerationID: "gen-4",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-5",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-5",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-5",
				GenerationID: "gen-5",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
	}

	source := &stubProjectorWorkSource{workItems: workItems}
	factStore := &stubFactStore{
		facts: []facts.Envelope{{
			FactID:       "fact-1",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKind:     "source_node",
		}},
	}
	runner := &stubProjectionRunner{
		result: Result{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
		},
	}
	sink := &stubProjectorWorkSink{}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   source,
		FactStore:    factStore,
		Runner:       runner,
		WorkSink:     sink,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
		Workers:      3,
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	source.mu.Lock()
	claimCalls := source.claimCalls
	source.mu.Unlock()

	factStore.mu.Lock()
	loadCalls := factStore.loadCalls
	factStore.mu.Unlock()

	runner.mu.Lock()
	runCalls := runner.runCalls
	runner.mu.Unlock()

	sink.mu.Lock()
	ackCalls := sink.ackCalls
	failCalls := sink.failCalls
	sink.mu.Unlock()

	if got, want := loadCalls, 5; got != want {
		t.Fatalf("load calls = %d, want %d", got, want)
	}
	if got, want := runCalls, 5; got != want {
		t.Fatalf("runner calls = %d, want %d", got, want)
	}
	if got, want := ackCalls, 5; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if got, want := failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
	if claimCalls < 8 {
		t.Fatalf("claim calls = %d, should be >= 8 (5 items + workers polling empty)", claimCalls)
	}
}

func TestServiceRunConcurrentErrorCancelsWorkers(t *testing.T) {
	t.Parallel()

	workItems := []ScopeGenerationWork{
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-1",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-1",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-1",
				GenerationID: "gen-1",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-2",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-2",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-2",
				GenerationID: "gen-2",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-3",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-3",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-3",
				GenerationID: "gen-3",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
		{
			Scope: scope.IngestionScope{
				ScopeID:       "scope-4",
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  "repo-4",
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      "scope-4",
				GenerationID: "gen-4",
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		},
	}

	source := &stubProjectorWorkSource{workItems: workItems}
	factStore := &stubFactStore{
		facts: []facts.Envelope{{
			FactID:       "fact-1",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKind:     "source_node",
		}},
	}
	runner := &stubProjectionRunner{
		result: Result{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
		},
	}
	// Make the sink fail after 2 successful acks (infrastructure failure)
	sink := &stubProjectorWorkSink{failAfter: 2}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   source,
		FactStore:    factStore,
		Runner:       runner,
		WorkSink:     sink,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
		Workers:      3,
	}

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil error")
	}

	sink.mu.Lock()
	ackCalls := sink.ackCalls
	sink.mu.Unlock()

	runner.mu.Lock()
	runCalls := runner.runCalls
	runner.mu.Unlock()

	// Should have processed at least 3 items (2 successful acks + 1 that triggered error)
	if ackCalls < 3 {
		t.Fatalf("ack calls = %d, want >= 3", ackCalls)
	}
	if runCalls < 3 {
		t.Fatalf("runner calls = %d, want >= 3", runCalls)
	}
	// Should not have processed all 4 items due to cancellation
	if runCalls >= 4 {
		t.Logf("warning: runner calls = %d, expected < 4 due to cancellation (may be timing-dependent)", runCalls)
	}
}

// stubFactCounter implements FactCounter for testing the large-gen semaphore.
type stubFactCounter struct {
	mu     sync.Mutex
	counts map[string]int // keyed by "scopeID/generationID"
	err    error
}

func (s *stubFactCounter) CountFacts(_ context.Context, scopeID, generationID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	return s.counts[scopeID+"/"+generationID], nil
}

// slowProjectionRunner blocks for a configurable duration per call to
// track concurrent execution for semaphore tests.
type slowProjectionRunner struct {
	mu          sync.Mutex
	maxInFlight int
	inFlight    int
	runCalls    int
	result      Result
	delay       time.Duration
}

func (s *slowProjectionRunner) Project(_ context.Context, _ scope.IngestionScope, _ scope.ScopeGeneration, _ []facts.Envelope) (Result, error) {
	s.mu.Lock()
	s.runCalls++
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.mu.Unlock()

	time.Sleep(s.delay)

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()

	return s.result, nil
}

func TestLargeGenSemaphoreLimitsConcurrency(t *testing.T) {
	t.Parallel()

	workItems := make([]ScopeGenerationWork, 3)
	for i := range workItems {
		sid := fmt.Sprintf("scope-%d", i)
		gid := fmt.Sprintf("gen-%d", i)
		workItems[i] = ScopeGenerationWork{
			Scope: scope.IngestionScope{
				ScopeID:       sid,
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  fmt.Sprintf("repo-%d", i),
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      sid,
				GenerationID: gid,
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		}
	}

	counter := &stubFactCounter{
		counts: map[string]int{
			"scope-0/gen-0": 20000,
			"scope-1/gen-1": 20000,
			"scope-2/gen-2": 20000,
		},
	}

	runner := &slowProjectionRunner{
		result: Result{ScopeID: "scope-0", GenerationID: "gen-0"},
		delay:  50 * time.Millisecond,
	}

	svc := Service{
		PollInterval:          10 * time.Millisecond,
		WorkSource:            &stubProjectorWorkSource{workItems: workItems},
		FactStore:             &stubFactStore{},
		Runner:                runner,
		WorkSink:              &stubProjectorWorkSink{},
		Wait:                  func(context.Context, time.Duration) error { return context.Canceled },
		Workers:               3,
		FactCounter:           counter,
		LargeGenThreshold:     1, // threshold=1 so all repos are "large"
		LargeGenMaxConcurrent: 1, // only 1 large gen at a time
	}
	svc.InitLargeGenSemaphore()

	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	runner.mu.Lock()
	maxInFlight := runner.maxInFlight
	runCalls := runner.runCalls
	runner.mu.Unlock()

	if runCalls != 3 {
		t.Fatalf("runner calls = %d, want 3", runCalls)
	}
	if maxInFlight > 1 {
		t.Fatalf("max concurrent projections = %d, want <= 1 (semaphore should limit)", maxInFlight)
	}
}

func TestSmallGenBypassesSemaphore(t *testing.T) {
	t.Parallel()

	workItems := make([]ScopeGenerationWork, 3)
	for i := range workItems {
		sid := fmt.Sprintf("scope-%d", i)
		gid := fmt.Sprintf("gen-%d", i)
		workItems[i] = ScopeGenerationWork{
			Scope: scope.IngestionScope{
				ScopeID:       sid,
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  fmt.Sprintf("repo-%d", i),
			},
			Generation: scope.ScopeGeneration{
				ScopeID:      sid,
				GenerationID: gid,
				ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
				IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
				Status:       scope.GenerationStatusPending,
				TriggerKind:  scope.TriggerKindSnapshot,
			},
		}
	}

	counter := &stubFactCounter{
		counts: map[string]int{
			"scope-0/gen-0": 50,
			"scope-1/gen-1": 50,
			"scope-2/gen-2": 50,
		},
	}

	runner := &slowProjectionRunner{
		result: Result{ScopeID: "scope-0", GenerationID: "gen-0"},
		delay:  50 * time.Millisecond,
	}

	svc := Service{
		PollInterval:          10 * time.Millisecond,
		WorkSource:            &stubProjectorWorkSource{workItems: workItems},
		FactStore:             &stubFactStore{},
		Runner:                runner,
		WorkSink:              &stubProjectorWorkSink{},
		Wait:                  func(context.Context, time.Duration) error { return context.Canceled },
		Workers:               3,
		FactCounter:           counter,
		LargeGenThreshold:     1000, // threshold=1000, facts=50 so all are "small"
		LargeGenMaxConcurrent: 1,
	}
	svc.InitLargeGenSemaphore()

	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	runner.mu.Lock()
	maxInFlight := runner.maxInFlight
	runCalls := runner.runCalls
	runner.mu.Unlock()

	if runCalls != 3 {
		t.Fatalf("runner calls = %d, want 3", runCalls)
	}
	// Small repos should run concurrently — at least 2 in-flight at once.
	if maxInFlight < 2 {
		t.Fatalf("max concurrent projections = %d, want >= 2 (small repos should bypass semaphore)", maxInFlight)
	}
}

func TestLargeGenSemaphoreSkipsOnCountError(t *testing.T) {
	t.Parallel()

	work := ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "scope-123",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo-123",
		},
		Generation: scope.ScopeGeneration{
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
			IngestedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
		},
	}

	counter := &stubFactCounter{err: errors.New("database error")}

	svc := Service{
		PollInterval:          10 * time.Millisecond,
		WorkSource:            &stubProjectorWorkSource{workItems: []ScopeGenerationWork{work}},
		FactStore:             &stubFactStore{facts: []facts.Envelope{{FactID: "f1", ScopeID: "scope-123", GenerationID: "generation-456", FactKind: "source_node"}}},
		Runner:                &stubProjectionRunner{result: Result{ScopeID: "scope-123", GenerationID: "generation-456"}},
		WorkSink:              &stubProjectorWorkSink{},
		Wait:                  func(context.Context, time.Duration) error { return context.Canceled },
		FactCounter:           counter,
		LargeGenThreshold:     1,
		LargeGenMaxConcurrent: 1,
		Logger:                slog.Default(),
	}
	svc.InitLargeGenSemaphore()

	if err := svc.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil (count error should not block)", err)
	}

	sink := svc.WorkSink.(*stubProjectorWorkSink)
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d (should proceed despite count error)", got, want)
	}
}
