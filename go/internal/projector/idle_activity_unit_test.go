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
