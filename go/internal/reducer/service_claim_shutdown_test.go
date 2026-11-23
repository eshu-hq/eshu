// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// shutdownClaimSource blocks inside Claim until the run context ends and then
// returns the wrapped context error, the way a database claim query behaves
// when the shutdown signal lands while it is in flight.
type shutdownClaimSource struct {
	inClaim chan struct{}
}

func (s *shutdownClaimSource) Claim(ctx context.Context) (reducercontract.Intent, bool, error) {
	select {
	case s.inClaim <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return reducercontract.Intent{}, false, fmt.Errorf("claim reducer work: %w", ctx.Err())
}

type noopShutdownExecutor struct{}

func (noopShutdownExecutor) Execute(context.Context, reducercontract.Intent) (reducercontract.Result, error) {
	return reducercontract.Result{}, nil
}

type noopShutdownSink struct{}

func (noopShutdownSink) Ack(context.Context, reducercontract.Intent, reducercontract.Result) error {
	return nil
}

func (noopShutdownSink) Fail(context.Context, reducercontract.Intent, error) error { return nil }

// TestServiceRunTreatsShutdownDuringClaimAsCleanStop pins that a shutdown
// that arrives while a claim is in flight ends the run cleanly. The shutdown
// is the operator's own SIGTERM, not a claim failure: the batch loop already
// returns nil when its claim fails with a cancelled context, and the
// sequential and per-item loops must do the same, or the reducer logs "reducer
// failed" and exits 1 on an ordinary stop (#11256).
func TestServiceRunTreatsShutdownDuringClaimAsCleanStop(t *testing.T) {
	t.Parallel()
	for _, workers := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Parallel()
			source := &shutdownClaimSource{inClaim: make(chan struct{}, 1)}
			service := Service{
				PollInterval: time.Millisecond, Workers: workers,
				WorkSource: source, WorkSink: noopShutdownSink{}, Executor: noopShutdownExecutor{},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- service.Run(ctx) }()
			select {
			case <-source.inClaim:
			case <-time.After(10 * time.Second):
				t.Fatal("no claim started")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Service.Run after shutdown during a claim = %v, want nil", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Service.Run did not return after shutdown")
			}
		})
	}
}

type failingClaimSource struct{ err error }

func (s failingClaimSource) Claim(context.Context) (reducercontract.Intent, bool, error) {
	return reducercontract.Intent{}, false, s.err
}

// TestServiceRunStillReturnsAClaimFailure pins the other side: a claim error
// that is not a shutdown (the run context is live) stays fatal, in both the
// sequential and the per-item loops.
func TestServiceRunStillReturnsAClaimFailure(t *testing.T) {
	t.Parallel()
	for _, workers := range []int{0, 3} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Parallel()
			boom := errors.New("connection refused")
			service := Service{
				PollInterval: time.Millisecond, Workers: workers,
				WorkSource: failingClaimSource{err: boom}, WorkSink: noopShutdownSink{}, Executor: noopShutdownExecutor{},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := service.Run(ctx); !errors.Is(err, boom) {
				t.Fatalf("Service.Run = %v, want the claim failure %v", err, boom)
			}
		})
	}
}

// shutdownAckSink fails every Ack with the wrapped context error once the run
// context is done, the way an ack statement or its post-commit wake fails when
// the shutdown signal lands mid-ack (the live proof's "wake waiting items after
// ack: ... context canceled"). With a live context it returns liveErr.
type shutdownAckSink struct {
	started chan struct{}
	liveErr error
}

func (s *shutdownAckSink) Ack(ctx context.Context, _ reducercontract.Intent, _ reducercontract.Result) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	if s.liveErr != nil {
		return s.liveErr
	}
	<-ctx.Done()
	return fmt.Errorf("wake waiting items after ack: %w", ctx.Err())
}

func (*shutdownAckSink) Fail(context.Context, reducercontract.Intent, error) error { return nil }

type oneIntentSource struct{ once atomic.Bool }

func (s *oneIntentSource) Claim(ctx context.Context) (reducercontract.Intent, bool, error) {
	if s.once.CompareAndSwap(false, true) {
		return reducercontract.Intent{IntentID: "intent-1", ScopeID: "scope-1", GenerationID: "gen-1", Domain: DomainWorkloadIdentity}, true, nil
	}
	<-ctx.Done()
	return reducercontract.Intent{}, false, nil
}

// TestServiceRunTreatsShutdownDuringAckAsCleanStop pins that an ack that fails
// only because shutdown cancelled its context ends the run cleanly (the claim
// stays leased and is reclaimed, or the ack committed and the audit bounds a
// lost wake), matching how a shutdown that interrupts the ack retry backoff is
// already treated. An ack failure with a live context stays fatal (#11256).
func TestServiceRunTreatsShutdownDuringAckAsCleanStop(t *testing.T) {
	t.Parallel()
	for _, workers := range []int{0, 3} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Parallel()
			sink := &shutdownAckSink{started: make(chan struct{}, 1)}
			service := Service{
				PollInterval: time.Millisecond, Workers: workers,
				WorkSource: &oneIntentSource{}, WorkSink: sink, Executor: succeedingShutdownExecutor{},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- service.Run(ctx) }()
			select {
			case <-sink.started:
			case <-time.After(10 * time.Second):
				t.Fatal("no ack started")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Service.Run after shutdown during an ack = %v, want nil", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Service.Run did not return after shutdown")
			}
		})
	}
}

func TestServiceRunStillReturnsAnAckFailure(t *testing.T) {
	t.Parallel()
	for _, workers := range []int{0, 3} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Parallel()
			boom := errors.New("ack statement failed")
			service := Service{
				PollInterval: time.Millisecond, Workers: workers, WorkSource: &oneIntentSource{},
				WorkSink: &shutdownAckSink{started: make(chan struct{}, 1), liveErr: boom}, Executor: succeedingShutdownExecutor{},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := service.Run(ctx); !errors.Is(err, boom) {
				t.Fatalf("Service.Run = %v, want the ack failure %v", err, boom)
			}
		})
	}
}

type succeedingShutdownExecutor struct{}

func (succeedingShutdownExecutor) Execute(_ context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	return reducercontract.Result{IntentID: intent.IntentID, Domain: intent.Domain, Status: reducercontract.ResultStatusSucceeded}, nil
}
