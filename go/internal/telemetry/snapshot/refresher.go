// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	"go.opentelemetry.io/otel/metric"
)

const (
	// DefaultInterval is the delay between the end of one refresh and the start
	// of the next when Config.Interval is unset.
	DefaultInterval = 5 * time.Minute
	// DefaultTimeout bounds one refresh read when Config.Timeout is unset.
	DefaultTimeout = 30 * time.Second

	// OutcomeSuccess labels a refresh that published a new snapshot.
	OutcomeSuccess = "success"
	// OutcomeError labels a refresh whose read returned an error before its
	// deadline.
	OutcomeError = "error"
	// OutcomeTimeout labels a refresh whose read did not finish inside
	// Config.Timeout.
	OutcomeTimeout = "timeout"
)

// Fetch reads one complete snapshot from the backing store. It must honor ctx
// cancellation: the refresher cancels ctx when Config.Timeout elapses or the
// process shuts down. The returned map is copied before it is published, so a
// Fetch may reuse its own buffers.
type Fetch func(ctx context.Context) (map[string]int64, error)

// Config tunes a Refresher.
type Config struct {
	// Interval is the delay between the end of one refresh and the start of
	// the next. Values <= 0 select DefaultInterval.
	Interval time.Duration
	// Timeout bounds each refresh read. Values <= 0 select DefaultTimeout.
	Timeout time.Duration
	// Meter registers the refresh metrics. A nil Meter records no metrics.
	Meter metric.Meter
	// Logger receives WARN records for failed and timed-out refreshes. A nil
	// Logger discards them.
	Logger *slog.Logger
	// Now supplies the clock; nil selects time.Now.
	Now func() time.Time
}

// reading is one immutable published snapshot.
type reading struct {
	counts map[string]int64
	at     time.Time
}

// Source is one gauge's cached snapshot. Counts never performs I/O.
type Source struct {
	owner  *Refresher
	name   string
	fetch  Fetch
	latest atomic.Pointer[reading]
}

// Counts returns the most recently published snapshot without blocking or
// touching the backing store. It returns a nil map and a nil error before the
// first successful refresh, so an observable-gauge callback that ranges over
// the result observes nothing until real data exists. It also returns nil once
// the snapshot is older than the Refresher's max age (3x the interval), so a
// gauge whose refreshes keep failing goes stale like a failed callback would
// instead of reporting a frozen value forever; the age gauge keeps reporting.
// Callers must not mutate the returned map.
func (s *Source) Counts(context.Context) (map[string]int64, error) {
	current := s.latest.Load()
	if current == nil {
		return nil, nil
	}
	if s.owner.now().Sub(current.at) > s.owner.maxAge {
		return nil, nil
	}
	return current.counts, nil
}

// Refresher refreshes registered Sources off the metrics scrape path. Each
// Source has exactly one refresh in flight at a time, run by its own
// goroutine, so a stuck backend read delays only that Source and never a
// scrape.
type Refresher struct {
	interval time.Duration
	timeout  time.Duration
	maxAge   time.Duration
	logger   *slog.Logger
	now      func() time.Time
	metrics  *refreshMetrics

	mu      sync.Mutex
	sources []*Source
	started bool
	wg      sync.WaitGroup
}

// New builds a Refresher. Register Sources, then call Start once.
func New(cfg Config) (*Refresher, error) {
	r := &Refresher{
		interval: cfg.Interval,
		timeout:  cfg.Timeout,
		logger:   cfg.Logger,
		now:      cfg.Now,
	}
	if r.interval <= 0 {
		r.interval = DefaultInterval
	}
	if r.timeout <= 0 {
		r.timeout = DefaultTimeout
	}
	if r.now == nil {
		r.now = time.Now
	}
	// A snapshot expires after three missed intervals. The floor keeps a
	// healthy but slow refresh (up to timeout after the interval) from
	// expiring its own snapshot when the timeout exceeds twice the interval.
	r.maxAge = 3 * r.interval
	if floor := r.interval + r.timeout; r.maxAge < floor {
		r.maxAge = floor
	}
	if cfg.Meter != nil {
		metrics, err := newRefreshMetrics(cfg.Meter, r)
		if err != nil {
			return nil, err
		}
		r.metrics = metrics
	}
	return r, nil
}

// Register adds a named Source before Start. The name becomes the bounded
// gauge label on the refresh metrics.
func (r *Refresher) Register(name string, fetch Fetch) (*Source, error) {
	if name == "" {
		return nil, errors.New("snapshot source name is required")
	}
	if fetch == nil {
		return nil, fmt.Errorf("snapshot source %q needs a fetch function", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil, fmt.Errorf("snapshot source %q registered after Start", name)
	}
	for _, existing := range r.sources {
		if existing.name == name {
			return nil, fmt.Errorf("snapshot source %q already registered", name)
		}
	}
	source := &Source{owner: r, name: name, fetch: fetch}
	r.sources = append(r.sources, source)
	return source, nil
}

// Start launches one refresh loop per Source, each running an immediate first
// refresh. The loops stop when ctx is done; Wait blocks until they have. Start
// is idempotent. Pass the process shutdown context, not context.Background, so
// the loops end before the backend they read from is closed.
func (r *Refresher) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	sources := append([]*Source(nil), r.sources...)
	r.mu.Unlock()

	for _, source := range sources {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.run(ctx, source)
		}()
	}
}

// Wait blocks until every refresh loop started by Start has returned.
func (r *Refresher) Wait() {
	r.wg.Wait()
}

func (r *Refresher) run(ctx context.Context, source *Source) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		r.refresh(ctx, source)
		timer.Reset(r.interval)
	}
}

// refresh performs one bounded read and publishes or records the result. A
// failed refresh keeps the previous snapshot; the age gauge and the outcome
// counter tell an operator how stale it has become.
func (r *Refresher) refresh(ctx context.Context, source *Source) {
	start := r.now()
	readCtx, cancel := context.WithTimeout(ctx, r.timeout)
	counts, err := source.fetch(readCtx)
	timedOut := errors.Is(readCtx.Err(), context.DeadlineExceeded)
	cancel()
	elapsed := r.now().Sub(start)

	if err == nil {
		source.latest.Store(&reading{counts: maps.Clone(counts), at: r.now()})
		r.record(ctx, source, OutcomeSuccess, elapsed)
		return
	}
	if ctx.Err() != nil {
		// Process shutdown cancelled the read; that is not a refresh failure.
		return
	}
	outcome := OutcomeError
	if timedOut {
		outcome = OutcomeTimeout
	}
	r.record(ctx, source, outcome, elapsed)
	r.warn(ctx, source, outcome, elapsed, err)
}

func (r *Refresher) record(ctx context.Context, source *Source, outcome string, elapsed time.Duration) {
	if r.metrics != nil {
		r.metrics.record(ctx, source.name, outcome, elapsed)
	}
}

func (r *Refresher) warn(ctx context.Context, source *Source, outcome string, elapsed time.Duration, err error) {
	if r.logger == nil {
		return
	}
	attrs := []any{
		slog.String(telemetry.MetricDimensionGauge, source.name),
		slog.String(telemetry.MetricDimensionOutcome, outcome),
		slog.Float64("elapsed_seconds", elapsed.Seconds()),
		slog.Float64("timeout_seconds", r.timeout.Seconds()),
		log.Err(err),
		telemetry.FailureClassAttr("gauge_snapshot_refresh_" + outcome),
	}
	if previous := source.latest.Load(); previous != nil {
		attrs = append(attrs, slog.Float64("snapshot_age_seconds", r.now().Sub(previous.at).Seconds()))
	}
	r.logger.WarnContext(ctx, "gauge snapshot refresh failed; keeping the last good snapshot until it expires", attrs...)
}
