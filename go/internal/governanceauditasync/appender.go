// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceauditasync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"go.opentelemetry.io/otel/metric"
)

const (
	// DefaultBufferCapacity is the F-9 (#5170) addendum's proven buffer size
	// (~300KB resident at this capacity): large enough to absorb realistic
	// burst traffic between worker drains without applying backpressure to
	// the request path.
	DefaultBufferCapacity = 1024

	// defaultBatchMax bounds how many events one flush call persists in a
	// single sink.Append, so a burst does not produce one unbounded INSERT.
	defaultBatchMax = 128

	// defaultFlushTimeout bounds each sink.Append call the worker makes. It
	// always uses context.Background() plus this timeout, never the
	// long-gone request context that queued the event.
	defaultFlushTimeout = 2 * time.Second

	// defaultShutdownTimeout bounds how long Close() waits for the worker to
	// drain and perform its final flush before returning, regardless of sink
	// behavior.
	defaultShutdownTimeout = 5 * time.Second
)

// ErrShutdownFlushIncomplete is returned by Close when the worker did not
// finish draining and flushing within the configured shutdown timeout. The
// worker goroutine may still be running in the background; this is a
// diagnostic signal for the caller, not a guarantee that no more work will
// ever complete.
var ErrShutdownFlushIncomplete = errors.New("governanceauditasync: shutdown flush did not complete before timeout")

// Appender is the durable sink AsyncAppender flushes batches to. The
// production sink is storage/postgres.GovernanceAuditStore. This interface is
// structurally identical to query.GovernanceAuditAppender so this package
// does not need to import query.
type Appender interface {
	Append(ctx context.Context, events []governanceaudit.Event) error
}

// Metrics holds the three drop-observability counters AsyncAppender records
// to. Any field may be nil (a safe no-op), which lets tests omit counters
// they do not assert on and lets callers construct an AsyncAppender before a
// meter provider is installed.
type Metrics struct {
	// Emitted counts events accepted into the buffer.
	Emitted metric.Int64Counter
	// Dropped counts events rejected because the buffer was full or the
	// appender was closed. Non-zero means governance data loss is
	// happening — this is the 3am operator signal.
	Dropped metric.Int64Counter
	// PersistFailures counts events accepted into the buffer but that a
	// sink.Append call failed to persist. Non-zero means the durable sink
	// itself is rejecting or unreachable for these events.
	PersistFailures metric.Int64Counter
}

// Option configures an AsyncAppender at construction time.
type Option func(*config)

type config struct {
	capacity        int
	batchMax        int
	flushTimeout    time.Duration
	shutdownTimeout time.Duration
}

// WithBufferCapacity overrides the default buffered-channel capacity
// (DefaultBufferCapacity).
func WithBufferCapacity(n int) Option {
	return func(c *config) { c.capacity = n }
}

// WithBatchMax overrides the default maximum events flushed per sink.Append
// call.
func WithBatchMax(n int) Option {
	return func(c *config) { c.batchMax = n }
}

// WithFlushTimeout overrides the default per-flush sink.Append timeout.
func WithFlushTimeout(d time.Duration) Option {
	return func(c *config) { c.flushTimeout = d }
}

// WithShutdownTimeout overrides the default bound on how long Close() waits
// for the worker to finish before returning.
func WithShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.shutdownTimeout = d }
}

// AsyncAppender is a best-effort, non-blocking Appender that buffers events
// in a bounded channel and flushes them to a durable sink from a single
// background worker. See the package doc and README for the full design
// rationale and semantics. The zero value is not usable; construct with
// NewAsyncAppender.
type AsyncAppender struct {
	sink    Appender
	metrics Metrics
	cfg     config

	buf chan governanceaudit.Event

	closeOnce sync.Once
	// closed signals the worker and enqueue path that no more events should
	// be accepted. It is closed exactly once by Close(); buf itself is never
	// closed, so a producer racing Close() can never panic on a send to a
	// closed channel.
	closed chan struct{}
	// done is closed by the worker goroutine once it has drained the buffer
	// and performed its final flush after closed fires.
	done chan struct{}
}

// NewAsyncAppender constructs an AsyncAppender and starts its background
// worker immediately. sink must not be nil.
func NewAsyncAppender(sink Appender, metrics Metrics, opts ...Option) *AsyncAppender {
	cfg := config{
		capacity:        DefaultBufferCapacity,
		batchMax:        defaultBatchMax,
		flushTimeout:    defaultFlushTimeout,
		shutdownTimeout: defaultShutdownTimeout,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	a := &AsyncAppender{
		sink:    sink,
		metrics: metrics,
		cfg:     cfg,
		buf:     make(chan governanceaudit.Event, cfg.capacity),
		closed:  make(chan struct{}),
		done:    make(chan struct{}),
	}
	go a.run()
	return a
}

// Append enqueues events for asynchronous persistence. It never blocks: each
// event costs one struct copy and one non-blocking channel send. A full
// buffer, or an appender that has been Close()d, drops the event and
// increments Metrics.Dropped instead of applying backpressure. The returned
// error is always nil; callers that need to observe loss must read
// Metrics.Dropped / Metrics.PersistFailures, not this return value.
func (a *AsyncAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	for _, event := range events {
		a.enqueue(event)
	}
	return nil
}

func (a *AsyncAppender) enqueue(event governanceaudit.Event) {
	// Guarded intake: check the closed signal first so an enqueue arriving
	// after Close() has begun drops cleanly. buf is never closed, so even
	// the inherent TOCTOU race between this check and the send below cannot
	// panic — it can only, in the narrow race window, deliver one extra
	// event to a worker that has not yet exited, which is an acceptable
	// best-effort outcome.
	select {
	case <-a.closed:
		a.countDropped()
		return
	default:
	}
	select {
	case a.buf <- event:
		a.countEmitted()
	default:
		a.countDropped()
	}
}

// run is the single background worker. It blocks for the first event of each
// batch, non-blocking-drains up to cfg.batchMax-1 more, flushes, and repeats.
// Once closed fires it drains whatever remains in the buffer, flushes it, and
// exits.
func (a *AsyncAppender) run() {
	defer close(a.done)
	for {
		select {
		case event := <-a.buf:
			batch := a.drainInto([]governanceaudit.Event{event})
			a.flush(batch)
		case <-a.closed:
			a.drainRemaining()
			return
		}
	}
}

// drainInto non-blockingly appends up to cfg.batchMax-len(batch) more events
// already sitting in the buffer, so one flush call covers a burst instead of
// one event per sink.Append.
func (a *AsyncAppender) drainInto(batch []governanceaudit.Event) []governanceaudit.Event {
	for len(batch) < a.cfg.batchMax {
		select {
		case event := <-a.buf:
			batch = append(batch, event)
		default:
			return batch
		}
	}
	return batch
}

// drainRemaining flushes whatever is left in the buffer at shutdown, in
// batches bounded by cfg.batchMax, then returns once the buffer reads empty.
func (a *AsyncAppender) drainRemaining() {
	var batch []governanceaudit.Event
	for {
		select {
		case event := <-a.buf:
			batch = append(batch, event)
			if len(batch) >= a.cfg.batchMax {
				a.flush(batch)
				batch = nil
			}
		default:
			a.flush(batch)
			return
		}
	}
}

// flush persists one batch to the sink using a fresh bounded-timeout context
// (never the request context of the original caller, which may already be
// gone). A persist failure drops the whole batch after recording it.
func (a *AsyncAppender) flush(batch []governanceaudit.Event) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.flushTimeout)
	defer cancel()
	if err := a.sink.Append(ctx, batch); err != nil {
		a.countPersistFailures(len(batch))
	}
}

// Close stops accepting new events, drains the buffer, performs a final
// bounded flush, and waits for the worker to exit. It is idempotent — safe
// to call more than once. It returns ErrShutdownFlushIncomplete if the
// worker has not finished within the configured shutdown timeout (default
// 5s); the worker may still be running in the background in that case, so a
// stuck sink can never hang process shutdown.
func (a *AsyncAppender) Close() error {
	a.closeOnce.Do(func() {
		close(a.closed)
	})
	select {
	case <-a.done:
		return nil
	case <-time.After(a.cfg.shutdownTimeout):
		return ErrShutdownFlushIncomplete
	}
}

func (a *AsyncAppender) countEmitted() {
	if a.metrics.Emitted != nil {
		a.metrics.Emitted.Add(context.Background(), 1)
	}
}

func (a *AsyncAppender) countDropped() {
	if a.metrics.Dropped != nil {
		a.metrics.Dropped.Add(context.Background(), 1)
	}
}

func (a *AsyncAppender) countPersistFailures(n int) {
	if n <= 0 || a.metrics.PersistFailures == nil {
		return
	}
	a.metrics.PersistFailures.Add(context.Background(), int64(n))
}
