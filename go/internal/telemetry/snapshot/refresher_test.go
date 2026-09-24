// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const waitBudget = 5 * time.Second

// syncBuffer lets a test read log output while refresh loops still write it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newMeter(t *testing.T) (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return reader, provider
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitBudget)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func refreshCount(t *testing.T, reader *sdkmetric.ManualReader, gauge, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_gauge_snapshot_refreshes_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("refreshes data = %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				g, _ := dp.Attributes.Value("gauge")
				o, _ := dp.Attributes.Value("outcome")
				if g.AsString() == gauge && o.AsString() == outcome {
					return dp.Value
				}
			}
		}
	}
	return 0
}

func ageGaugePresent(t *testing.T, reader *sdkmetric.ManualReader, gauge string) bool {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_gauge_snapshot_age_seconds" {
				continue
			}
			data, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("age data = %T, want Gauge[float64]", m.Data)
			}
			for _, dp := range data.DataPoints {
				if g, _ := dp.Attributes.Value("gauge"); g.AsString() == gauge {
					return true
				}
			}
		}
	}
	return false
}

// TestCountsBeforeFirstRefreshObservesNothing pins the contract that a gauge
// never reports a value before a read has actually succeeded.
func TestCountsBeforeFirstRefreshObservesNothing(t *testing.T) {
	t.Parallel()
	r, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	src, err := r.Register("g", func(context.Context) (map[string]int64, error) { return map[string]int64{"a": 1}, nil })
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	counts, err := src.Counts(context.Background())
	if err != nil || counts != nil {
		t.Fatalf("Counts() before refresh = %v, %v; want nil, nil", counts, err)
	}
}

// TestBlockedFetchNeverBlocksCounts is the #7062 core property: a backend read
// that never returns must not delay the cached read the scrape path uses.
func TestBlockedFetchNeverBlocksCounts(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	r, _ := New(Config{Timeout: time.Hour})
	src, _ := r.Register("g", func(ctx context.Context) (map[string]int64, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	t.Cleanup(func() { cancel(); r.Wait() })
	<-entered

	done := make(chan struct{})
	go func() {
		_, _ = src.Counts(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Counts() blocked while a refresh read was in flight")
	}
}

func TestRefreshPublishesSnapshotAndKeepsItOnFailure(t *testing.T) {
	t.Parallel()
	reader, provider := newMeter(t)
	var logs syncBuffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	var calls atomic.Int64
	r, err := New(Config{Interval: time.Millisecond, Meter: provider.Meter("t"), Logger: logger})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	src, _ := r.Register("edges", func(context.Context) (map[string]int64, error) {
		if calls.Add(1) == 1 {
			return map[string]int64{"terraform": 7}, nil
		}
		return nil, errors.New("bolt: connection reset")
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	t.Cleanup(func() { cancel(); r.Wait() })

	eventually(t, "an error outcome", func() bool { return refreshCount(t, reader, "edges", OutcomeError) >= 1 })
	counts, _ := src.Counts(context.Background())
	if counts["terraform"] != 7 {
		t.Fatalf("snapshot after failed refresh = %v, want the previous terraform=7", counts)
	}
	if got := refreshCount(t, reader, "edges", OutcomeSuccess); got != 1 {
		t.Fatalf("success outcomes = %d, want 1", got)
	}
	if !ageGaugePresent(t, reader, "edges") {
		t.Fatal("snapshot age gauge absent after a successful refresh")
	}
	// The outcome is counted just before the WARN is written, so wait for it.
	eventually(t, "the WARN log", func() bool { return strings.Contains(logs.String(), "level=WARN") })
	line := logs.String()
	for _, want := range []string{"level=WARN", "gauge=edges", "outcome=error", "connection reset", "snapshot_age_seconds"} {
		if !strings.Contains(line, want) {
			t.Fatalf("WARN log missing %q:\n%s", want, line)
		}
	}
}

func TestRefreshTimeoutRecordsTimeoutOutcome(t *testing.T) {
	t.Parallel()
	reader, provider := newMeter(t)
	var logs syncBuffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	r, _ := New(Config{Interval: time.Hour, Timeout: 20 * time.Millisecond, Meter: provider.Meter("t"), Logger: logger})
	src, _ := r.Register("files", func(ctx context.Context) (map[string]int64, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	t.Cleanup(func() { cancel(); r.Wait() })

	eventually(t, "a timeout outcome", func() bool { return refreshCount(t, reader, "files", OutcomeTimeout) == 1 })
	if got := refreshCount(t, reader, "files", OutcomeError); got != 0 {
		t.Fatalf("error outcomes = %d, want 0 (a deadline is a timeout)", got)
	}
	if counts, _ := src.Counts(context.Background()); counts != nil {
		t.Fatalf("snapshot after only a timeout = %v, want none", counts)
	}
	if ageGaugePresent(t, reader, "files") {
		t.Fatal("age gauge reported before any successful refresh")
	}
	eventually(t, "the WARN log", func() bool { return strings.Contains(logs.String(), "level=WARN") })
	if !strings.Contains(logs.String(), "outcome=timeout") || !strings.Contains(logs.String(), "gauge=files") {
		t.Fatalf("WARN log missing timeout outcome and gauge name:\n%s", logs.String())
	}
}

// TestShutdownStopsRefreshLoops proves the loops exit when the process context
// ends, including mid-read, and that a shutdown-cancelled read is not counted
// as a refresh failure.
func TestShutdownStopsRefreshLoops(t *testing.T) {
	t.Parallel()
	reader, provider := newMeter(t)
	entered := make(chan struct{})
	r, _ := New(Config{Timeout: time.Hour, Meter: provider.Meter("t")})
	_, _ = r.Register("g", func(ctx context.Context) (map[string]int64, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	<-entered
	cancel()

	stopped := make(chan struct{})
	go func() { r.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(waitBudget):
		t.Fatal("refresh loop did not stop after the process context ended")
	}
	for _, outcome := range []string{OutcomeError, OutcomeTimeout, OutcomeSuccess} {
		if got := refreshCount(t, reader, "g", outcome); got != 0 {
			t.Fatalf("%s outcomes after shutdown = %d, want 0", outcome, got)
		}
	}
}

// TestOneRefreshAtATimePerSource pins the concurrency contract: refreshes for
// one Source never overlap, however short the interval.
func TestOneRefreshAtATimePerSource(t *testing.T) {
	t.Parallel()
	var inFlight, maxInFlight, calls atomic.Int64
	r, _ := New(Config{Interval: time.Nanosecond})
	_, _ = r.Register("g", func(context.Context) (map[string]int64, error) {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		inFlight.Add(-1)
		calls.Add(1)
		return map[string]int64{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	r.Start(ctx) // idempotent: must not double the loops
	eventually(t, "several refreshes", func() bool { return calls.Load() >= 5 })
	cancel()
	r.Wait()
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("max concurrent refreshes = %d, want 1", got)
	}
}

func TestSourcesRefreshIndependently(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	r, _ := New(Config{Timeout: time.Hour})
	_, _ = r.Register("stuck", func(ctx context.Context) (map[string]int64, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	})
	healthy, _ := r.Register("healthy", func(context.Context) (map[string]int64, error) {
		return map[string]int64{"go": 3}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	t.Cleanup(func() { close(release); cancel(); r.Wait() })
	eventually(t, "the healthy source to publish while another is stuck", func() bool {
		counts, _ := healthy.Counts(context.Background())
		return counts["go"] == 3
	})
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()
	ok := func(context.Context) (map[string]int64, error) { return nil, nil }
	r, _ := New(Config{})
	if _, err := r.Register("", ok); err == nil {
		t.Fatal("Register(empty name) succeeded")
	}
	if _, err := r.Register("g", nil); err == nil {
		t.Fatal("Register(nil fetch) succeeded")
	}
	if _, err := r.Register("g", ok); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := r.Register("g", ok); err == nil {
		t.Fatal("Register(duplicate name) succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	if _, err := r.Register("late", ok); err == nil {
		t.Fatal("Register(after Start) succeeded")
	}
	cancel()
	r.Wait()
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Parallel()
	r, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if r.interval != DefaultInterval || r.timeout != DefaultTimeout {
		t.Fatalf("defaults = %v/%v, want %v/%v", r.interval, r.timeout, DefaultInterval, DefaultTimeout)
	}
}

// fakeClock is a settable clock for max-age tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestSnapshotExpiresAfterMaxAge pins #7062 review F3: once refreshes keep
// failing past MaxAge (3x the interval) the gauge stops observing the stale
// value, so its series goes stale like a callback error would, while the age
// gauge keeps reporting so the staleness stays visible.
func TestSnapshotExpiresAfterMaxAge(t *testing.T) {
	t.Parallel()
	reader, provider := newMeter(t)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	var fail atomic.Bool
	var calls atomic.Int64
	r, _ := New(Config{Interval: time.Millisecond, Timeout: time.Second, Meter: provider.Meter("t"), Now: clock.Now})
	src, _ := r.Register("edges", func(context.Context) (map[string]int64, error) {
		calls.Add(1)
		if fail.Load() {
			return nil, errors.New("graph unreachable")
		}
		return map[string]int64{"terraform": 7}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	t.Cleanup(func() { cancel(); r.Wait() })

	eventually(t, "first snapshot", func() bool {
		counts, _ := src.Counts(context.Background())
		return counts["terraform"] == 7
	})
	fail.Store(true)
	// Let at least one failing refresh land, then age the snapshot.
	seen := calls.Load()
	eventually(t, "a failing refresh", func() bool { return calls.Load() > seen+1 })

	clock.Advance(r.maxAge - time.Millisecond)
	if counts, _ := src.Counts(context.Background()); counts["terraform"] != 7 {
		t.Fatalf("snapshot just inside MaxAge = %v, want the served value", counts)
	}
	clock.Advance(2 * time.Millisecond)
	if counts, _ := src.Counts(context.Background()); counts != nil {
		t.Fatalf("snapshot past MaxAge = %v, want nothing observed", counts)
	}
	if !ageGaugePresent(t, reader, "edges") {
		t.Fatal("age gauge must keep reporting for an expired snapshot")
	}
}

func TestMaxAgeDerivedFromInterval(t *testing.T) {
	t.Parallel()
	r, _ := New(Config{Interval: time.Minute, Timeout: 10 * time.Second})
	if r.maxAge != 3*time.Minute {
		t.Fatalf("maxAge = %v, want 3x interval", r.maxAge)
	}
	// A timeout longer than the derived bound must not expire healthy slow reads.
	r, _ = New(Config{Interval: time.Second, Timeout: time.Minute})
	if r.maxAge < time.Second+time.Minute {
		t.Fatalf("maxAge = %v, want at least interval+timeout", r.maxAge)
	}
}
