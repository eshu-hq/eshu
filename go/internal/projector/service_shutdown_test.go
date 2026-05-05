package projector

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestServiceRunDoesNotFailWorkWhenShutdownCancelsLoad(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sink := &stubProjectorWorkSink{}
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubProjectorWorkSource{workItems: []ScopeGenerationWork{shutdownCanceledWork()}},
		FactStore:    &stubFactStore{returnContextErr: true},
		Runner:       &stubProjectionRunner{},
		WorkSink:     sink,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
	if got, want := sink.ackCalls, 0; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
}

func TestServiceRunDoesNotFailWorkWhenShutdownCancelsProjection(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sink := &stubProjectorWorkSink{}
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubProjectorWorkSource{workItems: []ScopeGenerationWork{shutdownCanceledWork()}},
		FactStore:    &stubFactStore{},
		Runner:       &stubProjectionRunner{waitForContextCancellation: true},
		WorkSink:     sink,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := sink.failCalls, 0; got != want {
		t.Fatalf("fail calls = %d, want %d", got, want)
	}
	if got, want := sink.ackCalls, 0; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
}

func TestServiceRunLogsShutdownCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var logs bytes.Buffer
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubProjectorWorkSource{workItems: []ScopeGenerationWork{shutdownCanceledWork()}},
		FactStore:    &stubFactStore{returnContextErr: true},
		Runner:       &stubProjectionRunner{},
		WorkSink:     &stubProjectorWorkSink{},
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	logOutput := logs.String()
	for _, want := range []string{
		`"msg":"projector work canceled during shutdown"`,
		`"failure_class":"shutdown_canceled"`,
		`"status":"shutdown_canceled"`,
		`"queue":"projector"`,
		`"scope_id":"scope-123"`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs missing %s in %s", want, logOutput)
		}
	}
}

func TestServiceRunAcksSuccessfulProjectionAfterShutdownCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	sink := &stubProjectorWorkSink{}
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource: &stubProjectorWorkSource{
			workItems:                    []ScopeGenerationWork{shutdownCanceledWork()},
			returnContextErrWhenCanceled: true,
		},
		FactStore: &stubFactStore{},
		Runner: &stubProjectionRunner{
			result: Result{
				ScopeID:      "scope-123",
				GenerationID: "generation-456",
			},
			beforeReturn: func(context.Context) {
				cancel()
			},
		},
		WorkSink: sink,
		Wait:     func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if sink.ackCtxErr != nil {
		t.Fatalf("ack context error = %v, want nil", sink.ackCtxErr)
	}
}

func TestServiceRunAcksSuccessfulProjectionWhenShutdownCancelsDuringAck(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	sink := &stubProjectorWorkSink{
		beforeAck: func(context.Context) {
			cancel()
		},
	}
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource: &stubProjectorWorkSource{
			workItems:                    []ScopeGenerationWork{shutdownCanceledWork()},
			returnContextErrWhenCanceled: true,
		},
		FactStore: &stubFactStore{},
		Runner: &stubProjectionRunner{
			result: Result{
				ScopeID:      "scope-123",
				GenerationID: "generation-456",
			},
		},
		WorkSink: sink,
		Wait:     func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := sink.ackCalls, 1; got != want {
		t.Fatalf("ack calls = %d, want %d", got, want)
	}
	if sink.ackCtxErr != nil {
		t.Fatalf("ack context error = %v, want nil", sink.ackCtxErr)
	}
}

func shutdownCanceledWork() ScopeGenerationWork {
	return ScopeGenerationWork{
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
}
