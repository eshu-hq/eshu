// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeConfigStateDriftRedriveClaimer implements configStateDriftRedriveClaimer
// without a real Postgres, so the catch-up loop/tick's own control flow can
// be proven as a fast unit test (issue #5593 P1-1).
type fakeConfigStateDriftRedriveClaimer struct {
	calls  int
	claims []postgres.ConfigStateDriftRedriveClaim
	err    error
}

func (f *fakeConfigStateDriftRedriveClaimer) ClaimDue(context.Context, int, int, time.Time) ([]postgres.ConfigStateDriftRedriveClaim, error) {
	f.calls++
	return f.claims, f.err
}

// fakeConfigStateDriftRedriveReplayer implements configStateDriftRedriveReplayer,
// recording every ReplayDomain call so tests can assert the catch-up loop
// redrives exactly the claimed (scope, generation) pairs against the
// config_state_drift domain -- never any other domain.
type fakeConfigStateDriftRedriveReplayer struct {
	calls []struct {
		scopeID      string
		generationID string
		domain       reducer.Domain
	}
	err     error
	perCall map[string]error
}

func (f *fakeConfigStateDriftRedriveReplayer) ReplayDomain(_ context.Context, scopeID, generationID string, domain reducer.Domain) (bool, error) {
	f.calls = append(f.calls, struct {
		scopeID      string
		generationID string
		domain       reducer.Domain
	}{scopeID, generationID, domain})
	if f.perCall != nil {
		if err, ok := f.perCall[scopeID+"/"+generationID]; ok {
			return err == nil, err
		}
	}
	return f.err == nil, f.err
}

func TestRunConfigStateDriftRedriveCatchUpTickReplaysEachClaimedRow(t *testing.T) {
	t.Parallel()

	claimer := &fakeConfigStateDriftRedriveClaimer{
		claims: []postgres.ConfigStateDriftRedriveClaim{
			{ScopeID: "state_snapshot:s3:hash-1", GenerationID: "gen-1", AttemptCount: 1},
			{ScopeID: "state_snapshot:s3:hash-2", GenerationID: "gen-2", AttemptCount: 3},
		},
	}
	replayer := &fakeConfigStateDriftRedriveReplayer{}

	runConfigStateDriftRedriveCatchUpTick(context.Background(), claimer, replayer, slog.Default())

	if claimer.calls != 1 {
		t.Fatalf("ClaimDue call count = %d, want 1", claimer.calls)
	}
	if len(replayer.calls) != 2 {
		t.Fatalf("ReplayDomain call count = %d, want 2", len(replayer.calls))
	}
	for i, want := range []struct {
		scopeID      string
		generationID string
	}{
		{"state_snapshot:s3:hash-1", "gen-1"},
		{"state_snapshot:s3:hash-2", "gen-2"},
	} {
		got := replayer.calls[i]
		if got.scopeID != want.scopeID || got.generationID != want.generationID {
			t.Fatalf("ReplayDomain call %d = %+v, want scope/generation %+v", i, got, want)
		}
		if got.domain != reducer.DomainConfigStateDrift {
			t.Fatalf("ReplayDomain call %d domain = %q, want %q", i, got.domain, reducer.DomainConfigStateDrift)
		}
	}
}

func TestRunConfigStateDriftRedriveCatchUpTickSwallowsClaimError(t *testing.T) {
	t.Parallel()

	claimer := &fakeConfigStateDriftRedriveClaimer{err: errors.New("injected claim failure")}
	replayer := &fakeConfigStateDriftRedriveReplayer{}

	// Must not panic and must not require a non-nil logger.
	runConfigStateDriftRedriveCatchUpTick(context.Background(), claimer, replayer, slog.Default())
	if claimer.calls != 1 {
		t.Fatalf("ClaimDue call count = %d, want 1 despite the error", claimer.calls)
	}
	if len(replayer.calls) != 0 {
		t.Fatalf("ReplayDomain call count = %d, want 0 (claim failed, nothing to replay)", len(replayer.calls))
	}

	runConfigStateDriftRedriveCatchUpTick(context.Background(), claimer, replayer, nil)
	if claimer.calls != 2 {
		t.Fatalf("ClaimDue call count = %d, want 2 with a nil logger", claimer.calls)
	}
}

// TestRunConfigStateDriftRedriveCatchUpTickContinuesPastOneReplayFailure
// proves one claimed row's ReplayDomain error does not stop the tick from
// attempting the REST of the claimed batch.
func TestRunConfigStateDriftRedriveCatchUpTickContinuesPastOneReplayFailure(t *testing.T) {
	t.Parallel()

	claimer := &fakeConfigStateDriftRedriveClaimer{
		claims: []postgres.ConfigStateDriftRedriveClaim{
			{ScopeID: "state_snapshot:s3:hash-1", GenerationID: "gen-1", AttemptCount: 1},
			{ScopeID: "state_snapshot:s3:hash-2", GenerationID: "gen-2", AttemptCount: 1},
		},
	}
	replayer := &fakeConfigStateDriftRedriveReplayer{
		perCall: map[string]error{
			"state_snapshot:s3:hash-1/gen-1": errors.New("replay failed for hash-1"),
		},
	}

	runConfigStateDriftRedriveCatchUpTick(context.Background(), claimer, replayer, slog.Default())

	if len(replayer.calls) != 2 {
		t.Fatalf("ReplayDomain call count = %d, want 2 (one failure must not skip the rest of the batch)", len(replayer.calls))
	}
}

// TestRunConfigStateDriftRedriveCatchUpLoopReturnsPromptlyOnContextCancellation
// proves the loop's ctx.Done() case wins immediately over the ticker
// interval -- an already-canceled context must make the loop return well
// within this test's bounded timeout, having never called ClaimDue.
func TestRunConfigStateDriftRedriveCatchUpLoopReturnsPromptlyOnContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	claimer := &fakeConfigStateDriftRedriveClaimer{}
	replayer := &fakeConfigStateDriftRedriveReplayer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runConfigStateDriftRedriveCatchUpLoop(ctx, claimer, replayer, slog.Default())
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected runConfigStateDriftRedriveCatchUpLoop to return promptly on an already-canceled context")
	}
	if claimer.calls != 0 {
		t.Fatalf("expected ClaimDue to never be called when ctx is canceled before the first tick, got %d calls", claimer.calls)
	}
}

// fakeConfigStateDriftServiceRunnerFunc adapts a plain func to serviceRunner
// for tests.
type fakeConfigStateDriftServiceRunnerFunc func(context.Context) error

func (f fakeConfigStateDriftServiceRunnerFunc) Run(ctx context.Context) error { return f(ctx) }

// TestRunServiceAndJoinConfigStateDriftRedriveReturnsPromptlyOnFatalServiceError
// mirrors cmd/projector's issue #5476 P0 regression test: a fix that joins a
// background redrive goroutine via a deferred WaitGroup.Wait() registered
// after a deferred cancel() relies on LIFO ordering that only holds on the
// signal-shutdown path. This proves the ingester's own join helper still
// returns promptly when service.Run returns a non-nil error WITHOUT itself
// canceling ctx (the real compositeRunner's fatal-error shape).
func TestRunServiceAndJoinConfigStateDriftRedriveReturnsPromptlyOnFatalServiceError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	fatalErr := errors.New("fatal composite runner failure")
	fakeService := fakeConfigStateDriftServiceRunnerFunc(func(context.Context) error { return fatalErr })

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runServiceAndJoinConfigStateDriftRedrive(ctx, fakeService, cancel, &wg)
	}()

	select {
	case err := <-resultCh:
		if !errors.Is(err, fatalErr) {
			t.Fatalf("expected the fatal error to be returned verbatim, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runServiceAndJoinConfigStateDriftRedrive hung: the fatal-error path must cancel the redrive context before joining")
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected ctx to be canceled by runServiceAndJoinConfigStateDriftRedrive's unconditional cancel() call")
	}
}

func TestRunServiceAndJoinConfigStateDriftRedriveReturnsPromptlyOnNormalShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	fakeService := fakeConfigStateDriftServiceRunnerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	go cancel()

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runServiceAndJoinConfigStateDriftRedrive(ctx, fakeService, cancel, &wg)
	}()

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("expected a nil error on the normal shutdown path, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected runServiceAndJoinConfigStateDriftRedrive to return promptly on the normal shutdown path")
	}
}
