// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// stepClock is a clock the test sets by hand.
type stepClock struct{ at time.Time }

func (c *stepClock) now() time.Time { return c.at }

// TestProcessActivityWindowEdges pins the activity window at its edges: a claim keeps
// the pool active for strictly less than the cap, and a claim at or after the cap
// reports the process was idle. The zero value (no claim yet) is idle.
func TestProcessActivityWindowEdges(t *testing.T) {
	t.Parallel()

	clock := &stepClock{at: simStart}
	activity := newProcessActivity(clock.now)
	if activity.active() {
		t.Fatal("a process that never claimed reports active, want idle")
	}
	if !activity.noteClaim() {
		t.Fatal("the first claim reports the process was busy, want idle")
	}
	claimedAt := clock.at
	cases := []struct {
		name       string
		after      time.Duration
		wantActive bool
		wantIdle   bool
	}{
		{"at the claim", 0, true, false},
		{"just inside the cap", maxProjectorPollInterval - time.Millisecond, true, false},
		{"at the cap", maxProjectorPollInterval, false, true},
		{"just past the cap", maxProjectorPollInterval + time.Millisecond, false, true},
	}
	for _, c := range cases {
		clock.at = claimedAt.Add(c.after)
		if got := activity.active(); got != c.wantActive {
			t.Errorf("%s: active() = %v, want %v", c.name, got, c.wantActive)
		}
		// noteClaim moves the stamp, so probe it on a copy of the state.
		probe := newProcessActivity(clock.now)
		probe.base = activity.base
		probe.lastClaim.Store(activity.lastClaim.Load())
		if got := probe.noteClaim(); got != c.wantIdle {
			t.Errorf("%s: noteClaim() idle = %v, want %v", c.name, got, c.wantIdle)
		}
	}
}

// TestProcessActivityTreatsABackwardClockAsIdle pins the clock-step fix: a clock that
// reads earlier than the last claim must not keep the pool active until it catches up
// (that is base-interval polling, the cost #10869 removed) and must not hide the first
// claim after the step from the wake.
func TestProcessActivityTreatsABackwardClockAsIdle(t *testing.T) {
	t.Parallel()

	clock := &stepClock{at: simStart.Add(10 * time.Second)}
	activity := newProcessActivity(clock.now)
	activity.noteClaim()
	clock.at = simStart.Add(10*time.Second - time.Hour)
	if activity.active() {
		t.Fatal("active() = true after the clock stepped back an hour, want idle until a new claim")
	}
	if !activity.noteClaim() {
		t.Fatal("noteClaim() reported busy after the clock stepped back, want idle so the wake is not lost")
	}
}

func newMetricInstruments(t *testing.T) (*telemetry.Instruments, func() int64) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, func() int64 {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		var total int64
		for _, scope := range rm.ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name != "eshu_dp_projector_idle_wakes_total" {
					continue
				}
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
				}
				for _, point := range sum.DataPoints {
					total += point.Value
				}
			}
		}
		return total
	}
}

// TestRecordIdleWakeCountsOnePerCall pins the emission: one wake is one counter
// increment, and a service without instruments or a logger does not panic.
func TestRecordIdleWakeCountsOnePerCall(t *testing.T) {
	t.Parallel()

	instruments, wakes := newMetricInstruments(t)
	service := Service{Instruments: instruments}
	service.recordIdleWake(context.Background(), 2)
	service.recordIdleWake(context.Background(), 3)
	if got := wakes(); got != 2 {
		t.Fatalf("%s = %d after two wakes, want 2", "eshu_dp_projector_idle_wakes_total", got)
	}
	Service{}.recordIdleWake(context.Background(), 0)
}

// windowSource hands out one item per second inside its windows and nothing outside.
type windowSource struct {
	simSource
	windows [][2]time.Duration
}

func (s *windowSource) Claim(ctx context.Context) (ScopeGenerationWork, bool, error) {
	now := s.clock.elapsed()
	for _, w := range s.windows {
		if now >= w[0] && now < w[1] {
			return s.simSource.Claim(ctx)
		}
	}
	s.mu.Lock()
	s.calls = append(s.calls, now)
	s.mu.Unlock()
	return ScopeGenerationWork{}, false, nil
}

// TestWakeFiresOnlyOnTheFirstClaimAfterTheProcessWasIdle runs the real worker loop on
// a simulated clock: a claim every second for ten seconds, a quiet gap longer than the
// cap, then another ten seconds. The wake counter must read two (one per busy spell),
// not one per claim: a wake on every claim fails here.
func TestWakeFiresOnlyOnTheFirstClaimAfterTheProcessWasIdle(t *testing.T) {
	t.Parallel()

	clock := newSimClock()
	source := &windowSource{
		simSource: simSource{clock: clock, every: time.Second},
		windows:   [][2]time.Duration{{0, 10 * time.Second}, {30 * time.Second, 40 * time.Second}},
	}
	instruments, wakes := newMetricInstruments(t)
	service := simService(clock, source, 4)
	service.Instruments = instruments
	runSim(t, service, clock, 4, 45*time.Second)

	if claims := source.n.Load(); claims < 15 {
		t.Fatalf("only %d items were claimed, want a claim every second in both windows", claims)
	}
	if got := wakes(); got != 2 {
		t.Fatalf("wake counter = %d, want 2 (the first claim of each busy spell only)", got)
	}
}

// TestNoteClaimInAnActivePoolIsNotIdleWhenASiblingStampsFirst pins the race between
// two claiming workers: A reads its time, B reads a later time and stores it first, then
// A stores. A's claim happens in a pool B just proved active, so it must not report the
// process idle (that would broadcast a spurious wake and overwrite B's newer stamp).
// The injected clock plays B: on the call that A makes inside noteClaim it stores B's
// later stamp, then returns the earlier time A read.
func TestNoteClaimInAnActivePoolIsNotIdleWhenASiblingStampsFirst(t *testing.T) {
	t.Parallel()

	var activity *processActivity
	calls := 0
	clock := func() time.Time {
		calls++
		switch calls {
		case 1: // the base, read by newProcessActivity
			return simStart
		case 2: // worker A's read inside noteClaim: B stamps a later time first
			activity.lastClaim.Store(int64(10*time.Millisecond) + 1)
			return simStart.Add(5 * time.Millisecond)
		default: // any later read, after both have stamped
			return simStart.Add(20 * time.Millisecond)
		}
	}
	activity = newProcessActivity(clock)
	if activity.noteClaim() {
		t.Fatal("noteClaim() reported idle for a claim made 10 ms after a sibling's, want busy")
	}
	if got, want := activity.lastClaim.Load(), int64(20*time.Millisecond)+1; got != want {
		t.Fatalf("stored stamp = %d, want %d (the newest time, not the older read)", got, want)
	}
}

// TestIdleWakeSendsWhenTheClockReadsEarlierThanTheLastBroadcast pins that a clock that
// went backward does not suppress wakes until it catches up: a negative gap since the
// last send counts as elapsed.
func TestIdleWakeSendsWhenTheClockReadsEarlierThanTheLastBroadcast(t *testing.T) {
	t.Parallel()

	clock := &stepClock{at: simStart.Add(time.Hour)}
	wake := newIdleWake(time.Second, clock.now)
	if !wake.broadcast() {
		t.Fatal("first broadcast was suppressed")
	}
	clock.at = simStart // an hour earlier than the last send
	if !wake.broadcast() {
		t.Fatal("broadcast suppressed while the clock reads earlier than the last send, want it sent")
	}
	clock.at = simStart.Add(500 * time.Millisecond)
	if wake.broadcast() {
		t.Fatal("a broadcast 500 ms after the last send was not debounced")
	}
}
