// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package saturation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// defaultMaxAttempts mirrors postgres.ReducerQueue's own default retry ceiling
// (reducer_queue_helpers.go maxAttempts) so this Odù's dead-letter budget for a
// counting failure class matches the production queue's.
const defaultMaxAttempts = 3

// saturationStage and saturationDomain label the modeled dead-letter rows so a
// DeadLetterRecord produced here is shaped like a real reducer graph-write
// dead-letter (stage "reducer", a materialization domain) for the
// ifa.DeadLetterSetsEqual comparison.
const (
	saturationStage  = "reducer"
	saturationDomain = "gcp_resource_materialization"
)

// errBackendSaturated is the cause wrapped by the modeled backend's
// GraphWriteTimeoutError so the surfaced error is a real
// cypher.GraphWriteTimeoutError (Retryable() == true, FailureClass ==
// graph_write_timeout) rather than a bespoke sentinel the retry classifier
// would not recognize.
var errBackendSaturated = errors.New("graph backend over write capacity")

// Config configures one hermetic saturation Odù.
type Config struct {
	// WorkItems is the number N of recoverable graph writes to drive. Must be
	// positive.
	WorkItems int
	// BackendCapacity is the number C of concurrent graph writes the modeled
	// backend serves without a timeout. A write whose concurrent in-flight
	// ordinal exceeds C returns a real cypher.GraphWriteTimeoutError. Must be
	// positive.
	BackendCapacity int
	// PermitPool is the graph-write permit ceiling handed to the real
	// cypher.BackpressureGate. A value <= 0 disables the gate (the #3560
	// pre-fix control): every offered write reaches the backend at once and the
	// surplus over BackendCapacity times out. A value <= BackendCapacity bounds
	// in-flight so no write is ever oversubscribed.
	PermitPool int
	// MaxAttempts is the retry ceiling for a counting failure class before a
	// persistently timing-out write dead-letters. <= 0 uses defaultMaxAttempts.
	MaxAttempts int
	// TransientTimeouts injects this many one-shot timeouts on distinct items'
	// first attempt regardless of capacity, so the retry-then-succeed path is
	// exercised even when the gate prevents oversubscription. Must not exceed
	// WorkItems.
	TransientTimeouts int
}

func (c Config) validate() error {
	if c.WorkItems <= 0 {
		return fmt.Errorf("saturation: WorkItems must be positive, got %d", c.WorkItems)
	}
	if c.BackendCapacity <= 0 {
		return fmt.Errorf("saturation: BackendCapacity must be positive, got %d", c.BackendCapacity)
	}
	if c.TransientTimeouts < 0 || c.TransientTimeouts > c.WorkItems {
		return fmt.Errorf("saturation: TransientTimeouts must be in [0, WorkItems=%d], got %d", c.WorkItems, c.TransientTimeouts)
	}
	return nil
}

func (c Config) maxAttempts() int {
	if c.MaxAttempts > 0 {
		return c.MaxAttempts
	}
	return defaultMaxAttempts
}

// Report is the observed failure shape of one saturation Odù.
type Report struct {
	// BackpressureEngaged counts how many acquires had to wait for a permit
	// (the real gate's wait observer fired). > 0 proves backpressure engaged.
	BackpressureEngaged int64
	// Succeeded is the number of work items that ultimately committed.
	Succeeded int
	// Retries is the total number of retry re-enqueues across all rounds.
	Retries int
	// DeadLetters is the set of items dead-lettered as unrecoverable. Under a
	// gate that bounds in-flight to backend capacity it must be empty.
	DeadLetters []ifa.DeadLetterRecord
	// Residual is the count of non-terminal work items after the drain
	// completes — the B-12 residual, which must be 0.
	Residual int
	// PeakInFlight is the maximum concurrent backend writes observed. Under the
	// gate it must not exceed PermitPool.
	PeakInFlight int
}

// workItem is one recoverable graph write being driven through the saturation
// scenario.
type workItem struct {
	id                 string
	attempt            int
	transientRemaining int
}

// Run drives cfg.WorkItems recoverable graph writes past cfg.PermitPool permits
// against a capacity-bounded backend and returns the observed failure shape. It
// is hermetic: no Postgres, no graph backend, no network. It exercises the real
// cypher.BackpressureGate, the real cypher.GraphWriteTimeoutError, and the real
// reducer.IsRetryable retry classification, so the assertion that a bounded gate
// eliminates the #3560 dead-letter flood tracks the production seams.
func Run(ctx context.Context, cfg Config) (Report, error) {
	if err := cfg.validate(); err != nil {
		return Report{}, err
	}

	obs := &countingObserver{}
	gate := sourcecypher.NewBackpressureGate(cfg.PermitPool, obs)
	backend := &capacityBackend{capacity: cfg.BackendCapacity, gated: cfg.PermitPool > 0}

	pending := make([]*workItem, 0, cfg.WorkItems)
	for i := 0; i < cfg.WorkItems; i++ {
		item := &workItem{id: fmt.Sprintf("ifa-saturation-%04d", i)}
		if i < cfg.TransientTimeouts {
			item.transientRemaining = 1
		}
		pending = append(pending, item)
	}

	report := Report{}
	// A counting failure class dead-letters after at most maxAttempts rounds, so
	// the drain always terminates; the +2 guards a modeling error rather than a
	// real infinite loop.
	maxRounds := cfg.maxAttempts() + 2
	for round := 0; len(pending) > 0; round++ {
		if round > maxRounds {
			return Report{}, fmt.Errorf("saturation: drain did not converge after %d rounds; %d items still pending", round, len(pending))
		}
		outcomes, err := backend.runRound(ctx, gate, pending)
		if err != nil {
			return Report{}, err
		}
		pending = classify(cfg, pending, outcomes, &report)
	}

	report.BackpressureEngaged = obs.engaged.Load()
	report.PeakInFlight = backend.peakInFlight()
	report.Residual = len(pending)
	return report, nil
}

// classify applies the real retry-vs-dead-letter decision to one round's
// outcomes and returns the items still pending (to retry). A timed-out write is
// retried while its attempt count is under the ceiling and the cause is
// retryable; otherwise it dead-letters. A successful write is terminal.
func classify(cfg Config, pending []*workItem, outcomes []error, report *Report) []*workItem {
	next := pending[:0:0]
	for i, item := range pending {
		werr := outcomes[i]
		if werr == nil {
			report.Succeeded++
			continue
		}
		item.attempt++
		report.Retries++
		if retryable(werr, item.attempt, cfg.maxAttempts()) {
			next = append(next, item)
			continue
		}
		report.DeadLetters = append(report.DeadLetters, deadLetterRecord(werr, item))
	}
	return next
}

// retryable mirrors postgres.ReducerQueue.retryable for a counting failure
// class: the cause must self-classify as retryable AND the attempt count must be
// under the ceiling. graph_write_timeout is a counting class (it is not in
// nonCountingReducerRetryFailureClasses), so a write that keeps timing out past
// the ceiling correctly dead-letters — which is exactly why an unbounded gate
// floods and a bounded gate does not.
func retryable(cause error, attemptCount, maxAttempts int) bool {
	if !reducer.IsRetryable(cause) {
		return false
	}
	return attemptCount < maxAttempts
}

// deadLetterRecord builds the durable dead-letter row for an item that
// exhausted its retry budget, preserving the cause's self-classified failure
// class so the record is shaped like a real graph_write_timeout dead-letter.
func deadLetterRecord(cause error, item *workItem) ifa.DeadLetterRecord {
	failureClass := "reducer_failed"
	var classifier interface{ FailureClass() string }
	if errors.As(cause, &classifier) {
		failureClass = classifier.FailureClass()
	}
	return ifa.DeadLetterRecord{
		WorkItemID:   item.id,
		Stage:        saturationStage,
		Domain:       saturationDomain,
		FailureClass: failureClass,
	}
}

// countingObserver is a real cypher.BackpressureObserver that counts wait
// signals so a test can assert the gate engaged without wiring a full
// telemetry.Instruments meter.
type countingObserver struct {
	engaged atomic.Int64
}

func (o *countingObserver) ObserveBackpressureWait(_ context.Context, _ string, _ time.Duration) {
	o.engaged.Add(1)
}

// engagementGrace is how long a gated round waits, after every goroutine has
// reached the gate, for the over-pool surplus to block on Acquire before the
// pinned permit-holders release. It only guarantees the wait observer fires
// (backpressure engagement); the pass/fail assertions depend on the structural
// PermitPool-vs-capacity bound, not on this duration, so it is not a
// correctness race. It mirrors the channel-plus-deadline idiom the repo's own
// graph_write_permit_split_test.go uses to prove gate blocking.
const engagementGrace = 25 * time.Millisecond

// capacityBackend models a graph backend that serves up to `capacity`
// concurrent writes and returns a real cypher.GraphWriteTimeoutError for the
// surplus. Its over-subscription is deterministic: each write reads its unique
// concurrent ordinal from an atomic counter, and (when ungated) a per-round
// barrier forces every offered write to overlap so the ordinals span the full
// offered concurrency. When gated, the permit pool already bounds concurrency
// to <= capacity, so no ordinal exceeds capacity; the first cohort instead pins
// its permits on a hold channel until the surplus has blocked on Acquire, so
// the gate's wait observer deterministically fires.
type capacityBackend struct {
	capacity int
	gated    bool

	inFlight atomic.Int64
	peakMu   sync.Mutex
	peak     int
}

func (b *capacityBackend) peakInFlight() int {
	b.peakMu.Lock()
	defer b.peakMu.Unlock()
	return b.peak
}

func (b *capacityBackend) recordPeak(n int64) {
	b.peakMu.Lock()
	if int(n) > b.peak {
		b.peak = int(n)
	}
	b.peakMu.Unlock()
}

// runRound drives every pending item concurrently through the gate and the
// backend, returning each item's write outcome in pending order. It dispatches
// to the gated or ungated round shape, which differ only in how they make the
// concurrency deterministic (permit-hold vs overlap barrier).
func (b *capacityBackend) runRound(ctx context.Context, gate *sourcecypher.BackpressureGate, pending []*workItem) ([]error, error) {
	if b.gated {
		return b.runGatedRound(ctx, gate, pending)
	}
	return b.runUngatedRound(ctx, gate, pending)
}

// runUngatedRound reproduces the #3560 pre-fix world: with the gate disabled,
// every offered write reaches the backend at once. A barrier sized to the
// round's offered count forces all writes to overlap so the surplus over
// capacity deterministically times out, independent of scheduler timing and of
// -race overhead.
func (b *capacityBackend) runUngatedRound(ctx context.Context, gate *sourcecypher.BackpressureGate, pending []*workItem) ([]error, error) {
	outcomes := make([]error, len(pending))
	bar := newBarrier(len(pending))

	var wg sync.WaitGroup
	for i, item := range pending {
		wg.Add(1)
		go func(i int, item *workItem) {
			defer wg.Done()
			release, err := gate.Acquire(ctx, sourcecypher.GraphWriteTimeoutFailureClass)
			if err != nil {
				outcomes[i] = err
				return
			}
			defer release()
			n := b.inFlight.Add(1)
			b.recordPeak(n)
			bar.wait()
			outcomes[i] = b.settle(ctx, item, n)
			b.inFlight.Add(-1)
		}(i, item)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("saturation: round canceled: %w", err)
	}
	return outcomes, nil
}

// runGatedRound drives the pending items through the real permit gate. The
// first cohort of permit-holders pins its permits on holdCh so the over-pool
// surplus blocks on Acquire (its wait observer fires); after every goroutine has
// reached the gate and the surplus has had engagementGrace to block, holdCh
// closes and the holders release, letting the surplus drain in later cohorts.
// Because the gate never admits more than PermitPool concurrent writers and
// PermitPool <= capacity for a fixed configuration, no write is ever
// oversubscribed, so none times out from capacity.
func (b *capacityBackend) runGatedRound(ctx context.Context, gate *sourcecypher.BackpressureGate, pending []*workItem) ([]error, error) {
	outcomes := make([]error, len(pending))
	holdCh := make(chan struct{})
	var arrived sync.WaitGroup
	arrived.Add(len(pending))

	var wg sync.WaitGroup
	for i, item := range pending {
		wg.Add(1)
		go func(i int, item *workItem) {
			defer wg.Done()
			arrived.Done()
			release, err := gate.Acquire(ctx, sourcecypher.GraphWriteTimeoutFailureClass)
			if err != nil {
				outcomes[i] = err
				return
			}
			defer release()
			// Pin the permit until the surplus has blocked, then execute.
			select {
			case <-holdCh:
			case <-ctx.Done():
				outcomes[i] = fmt.Errorf("saturation: write canceled: %w", ctx.Err())
				return
			}
			n := b.inFlight.Add(1)
			b.recordPeak(n)
			outcomes[i] = b.settle(ctx, item, n)
			b.inFlight.Add(-1)
		}(i, item)
	}

	arrived.Wait()
	// Let the over-pool goroutines reach the blocking Acquire before the pinned
	// holders release, so the wait observer fires deterministically.
	select {
	case <-time.After(engagementGrace):
	case <-ctx.Done():
	}
	close(holdCh)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("saturation: round canceled: %w", err)
	}
	return outcomes, nil
}

// settle computes the outcome for one modeled write given its concurrent
// ordinal n; it does not touch inFlight (each caller brackets the write with its
// own inFlight increment/decrement). It returns a real
// cypher.GraphWriteTimeoutError when the write is a transient injected timeout
// on its first attempt, or when its ordinal exceeds capacity (oversubscription
// — the #3560 condition the gate prevents).
func (b *capacityBackend) settle(ctx context.Context, item *workItem, n int64) error {
	transient := false
	if item.transientRemaining > 0 && item.attempt == 0 {
		item.transientRemaining--
		transient = true
	}
	over := transient || n > int64(b.capacity)
	if over {
		return sourcecypher.GraphWriteTimeoutError{
			Operation: "graph write",
			Summary:   "graph backend over write capacity under saturation Odù",
			Cause:     errBackendSaturated,
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("saturation: write canceled: %w", err)
	}
	return nil
}

// barrier is a one-shot cyclic barrier: the first n callers to wait block until
// the n-th arrives, then all proceed. It makes a round's concurrency
// deterministic without a wall-clock settle, so the ungated flood reproduces
// identically under -race and on slow CI.
type barrier struct {
	n     int
	mu    sync.Mutex
	count int
	ready chan struct{}
}

func newBarrier(n int) *barrier {
	return &barrier{n: n, ready: make(chan struct{})}
}

func (b *barrier) wait() {
	b.mu.Lock()
	b.count++
	if b.count >= b.n {
		select {
		case <-b.ready:
		default:
			close(b.ready)
		}
	}
	b.mu.Unlock()
	<-b.ready
}
