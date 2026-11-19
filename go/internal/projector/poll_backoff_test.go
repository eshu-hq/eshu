// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// scriptedWorkSource replays a scripted claim sequence so idle-poll tests can
// interleave empty answers, real work, and claim conflicts deterministically.
type scriptedWorkSource struct {
	mu     sync.Mutex
	script []scriptedClaim
	calls  int
}

type scriptedClaim struct {
	work ScopeGenerationWork
	ok   bool
	err  error
}

func (s *scriptedWorkSource) Claim(context.Context) (ScopeGenerationWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.script) == 0 {
		return ScopeGenerationWork{}, false, nil
	}
	next := s.script[0]
	s.script = s.script[1:]
	return next.work, next.ok, next.err
}

// waitRecorder records every wait interval and ends the run after maxWaits by
// returning context.Canceled.
type waitRecorder struct {
	mu        sync.Mutex
	intervals []time.Duration
	maxWaits  int
}

func (w *waitRecorder) wait(_ context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.intervals = append(w.intervals, d)
	if len(w.intervals) >= w.maxWaits {
		return context.Canceled
	}
	return nil
}

func (w *waitRecorder) recorded() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.intervals...)
}

func idleTestWork() ScopeGenerationWork {
	return ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
	}
}

func idleTestService(source ProjectorWorkSource, recorder *waitRecorder) Service {
	return Service{
		PollInterval:       time.Second,
		WorkSource:         source,
		FactStore:          &stubFactStore{},
		Runner:             &stubProjectionRunner{result: runtime.Result{ScopeID: "scope-123", GenerationID: "generation-456"}},
		WorkSink:           &stubProjectorWorkSink{},
		DeltaBaselineFence: &fakeDeltaBaselineFence{},
		WriteMarker:        &fakeWriteMarker{},
		Wait:               recorder.wait,
	}
}

func assertWaits(t *testing.T, got []time.Duration, want []time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("wait intervals = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wait intervals = %v, want %v", got, want)
		}
	}
}

func TestProjectorPollBackoffSequence(t *testing.T) {
	t.Parallel()
	base := time.Second
	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second,
		5 * time.Second, 5 * time.Second, 5 * time.Second,
	}
	for i, w := range want {
		if got := projectorPollBackoff(base, i+1); got != w {
			t.Fatalf("projectorPollBackoff(1s, %d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestProjectorPollBackoffNeverShortensLargeBase(t *testing.T) {
	t.Parallel()
	base := 10 * time.Second
	for _, n := range []int{1, 2, 3, 4, 5, 10} {
		if got := projectorPollBackoff(base, n); got != base {
			t.Fatalf("projectorPollBackoff(10s, %d) = %v, want %v", n, got, base)
		}
	}
}

func TestProjectorPollBackoffDefaultsNonPositiveBase(t *testing.T) {
	t.Parallel()
	if got := projectorPollBackoff(0, 1); got != defaultPollInterval {
		t.Fatalf("projectorPollBackoff(0, 1) = %v, want %v", got, defaultPollInterval)
	}
	if got := projectorPollBackoff(-time.Second, 3); got != 4*defaultPollInterval {
		t.Fatalf("projectorPollBackoff(-1s, 3) = %v, want %v", got, 4*defaultPollInterval)
	}
}

func TestServiceRunSequentialBacksOffWhileIdle(t *testing.T) {
	t.Parallel()
	source := &scriptedWorkSource{}
	recorder := &waitRecorder{maxWaits: 5}
	service := idleTestService(source, recorder)

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	assertWaits(t, recorder.recorded(), []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second,
	})
}

func TestServiceRunSequentialResetsBackoffAfterWork(t *testing.T) {
	t.Parallel()
	source := &scriptedWorkSource{script: []scriptedClaim{
		{},
		{},
		{work: idleTestWork(), ok: true},
	}}
	recorder := &waitRecorder{maxWaits: 5}
	service := idleTestService(source, recorder)

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	// Two empties back off to 1s, 2s; the claimed work resets the counter so
	// the next empties wait 1s, 2s, 4s again.
	assertWaits(t, recorder.recorded(), []time.Duration{
		time.Second, 2 * time.Second,
		time.Second, 2 * time.Second, 4 * time.Second,
	})
}

func TestServiceRunSequentialConflictWaitsBaseWithoutBackoff(t *testing.T) {
	t.Parallel()
	conflict := fmt.Errorf("claim projector work: %w: deadlock detected (SQLSTATE 40P01)",
		failure.ErrWorkClaimConflict)
	source := &scriptedWorkSource{script: []scriptedClaim{
		{err: conflict}, {err: conflict}, {err: conflict},
	}}
	recorder := &waitRecorder{maxWaits: 4}
	service := idleTestService(source, recorder)

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	// Contention is not emptiness: every conflict waits exactly one base
	// interval and never advances the idle backoff. The fourth wait comes
	// from the empty poll after the scripted conflicts drain, and it is
	// still the base interval because conflicts left the counter at zero.
	assertWaits(t, recorder.recorded(), []time.Duration{
		time.Second, time.Second, time.Second, time.Second,
	})
}

func TestServiceRunConcurrentBacksOffWithinBounds(t *testing.T) {
	t.Parallel()
	source := &scriptedWorkSource{}
	recorder := &waitRecorder{maxWaits: 12}
	service := idleTestService(source, recorder)
	service.Workers = 3

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	recorded := recorder.recorded()
	// Workers already past their wait when the last recorded wait ends the
	// run still record their own final wait, so the total may exceed
	// maxWaits; every recorded interval must still be a valid backoff step.
	if len(recorded) < 12 {
		t.Fatalf("recorded %d waits, want at least 12", len(recorded))
	}
	allowed := map[time.Duration]bool{
		time.Second: true, 2 * time.Second: true,
		4 * time.Second: true, 5 * time.Second: true,
	}
	baseCount := 0
	for _, d := range recorded {
		if !allowed[d] {
			t.Fatalf("wait interval %v outside idle backoff sequence [1s 2s 4s 5s]", d)
		}
		if d == time.Second {
			baseCount++
		}
	}
	// Every worker's first idle wait is exactly the base interval, so at
	// least one base wait per worker must be present.
	if baseCount < 3 {
		t.Fatalf("only %d base-interval waits, want at least 3 (one per worker)", baseCount)
	}
	// Progression pin: with at least 12 waits across 3 workers, some worker
	// recorded at least 4 consecutive waits, so at least one interval must
	// exceed the base. Without this, a worker stuck at the base interval
	// (no backoff on the concurrent path) still passes.
	progressed := false
	for _, d := range recorded {
		if d > time.Second {
			progressed = true
			break
		}
	}
	if !progressed {
		t.Fatalf("no wait interval exceeded the base 1s across %d recorded waits; concurrent backoff did not progress", len(recorded))
	}
}
