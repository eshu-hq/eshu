// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// defaultIdentityCacheMaxBytes is the evidence-based default cap for the
// identity-fact cache. Measured on a 500k-identity-fact shim (2.5M total rows,
// 2000 scopes): 226.5 MB Postgres on-disk, estimated ~342 MB in-process Go
// structs (string fields + JSON payload + fixed overhead). 500 MiB provides
// 2.2× headroom over the measured worst case, keeping a realistic corpus
// cached while a pathological set passes through observably via
// eshu_dp_identity_cache_passthrough_total.
const defaultIdentityCacheMaxBytes = 500 * 1024 * 1024 // 500 MiB

// identityEpoch is the set-identity probe for the identity-fact cache.
// Two epochs are equal when count, max observed_at, and active_fingerprint
// all match. The fingerprint captures the active-generation mapping from
// ingestion_scopes so a supersession (active_generation_id flip) is detected
// even when total fact count and max observed_at are unchanged.
//
// The fingerprint is a collision-resistant SHA-256 digest of the ordered active-
// generation mapping (every scope's "scope_id:active_generation_id" pair,
// ORDER BY scope_id, joined with '|'), not a summed 32-bit hash. Any change
// to the active mapping — including two different mappings that would
// collide under a 32-bit hashtext sum, or offsetting deltas that would
// cancel out in a sum — changes the digest deterministically, so the epoch
// always changes when the active mapping changes.
type identityEpoch struct {
	count             int
	maxObservedAt     time.Time
	activeFingerprint string
}

// identityFlight is one in-flight identity-fact load. The leader that created
// it owns the load; every other caller that parks on it waits on done.
//
// startEpoch is the leader's pre-load probe. A parked caller whose own probe
// equals startEpoch is served the flight's rows; a caller whose probe differs
// saw a newer active set than the flight started from, so it waits for the
// flight and loads for itself rather than risk a set that predates its trigger.
//
// rows, err, leaderCanceled, and torn are written by the leader before it closes
// done and read by waiters only after done closes; the channel close is the
// happens-before edge. waiters is a cumulative count of callers that parked on
// the flight, guarded by IdentityEpochCache.mu with startEpoch. It is a
// test-only seam: production code only increments it, and the flight tests read
// it (awaitFlightWaiters) to know when N callers have parked before they
// release the load. Operators read the waiter counts from the
// flight_waiter_total counter, not from this field.
type identityFlight struct {
	done       chan struct{}
	startEpoch identityEpoch
	waiters    int

	rows           []facts.Envelope
	err            error
	leaderCanceled bool
	// torn marks a flight whose set could not be validated against a stable
	// epoch (the epoch kept moving through every attempt, or the post-load
	// probe failed). Nobody uses its rows: waiters re-probe and the leader's
	// item is retried.
	torn bool
}

// Reason values for eshu_dp_identity_cache_passthrough_total: why a finished
// load was not cached. cap_exceeded and size_unknown still serve the consistent
// set to its flight uncached. epoch_moved and probe_error mean the set could not
// be validated: it is discarded, never served, and the leader's item fails with
// identityLoadUnstableError.
const (
	identityDiscardEpochMoved  = "epoch_moved"
	identityDiscardCapExceeded = "cap_exceeded"
	identityDiscardSizeUnknown = "size_unknown"
	identityDiscardProbeError  = "probe_error"
)

// Outcome values for eshu_dp_identity_cache_flight_waiter_total: what happened
// to a caller that arrived while a load was in flight.
const (
	identityWaiterShared         = "shared"
	identityWaiterSharedError    = "shared_error"
	identityWaiterStaleEpoch     = "stale_epoch"
	identityWaiterLeaderCanceled = "leader_canceled"
	identityWaiterTornSet        = "torn_set"
)

// IdentityEpochCache caches the full set of active container-image identity
// facts, validated by an O(1) epoch probe (count + max observed_at) backed
// by a partial B-tree index. On probe match the cached slice is served with
// a defensive copy; on miss a singleflight reload drains the paginated load
// exactly once.
//
// Concurrency: mu guards epoch, facts, and loading. mu is never held across
// the DB load or the epoch probe — both release the lock before I/O.
type IdentityEpochCache struct {
	mu      sync.Mutex
	epoch   identityEpoch
	facts   []facts.Envelope
	loading *identityFlight // non-nil while a singleflight reload is in flight

	maxBytes int64
	inst     *telemetry.Instruments

	// heartbeatInterval is the reducer per-item heartbeat interval. A waiter
	// spends at most this long in total waiting on flights; zero disables the
	// wall-clock half of the waiter bound. See waitForFlight.
	heartbeatInterval time.Duration
	// newTimer and now are the clock a test injects to drive the wall-clock
	// bound without sleeping. Nil means the real clock.
	newTimer func(d time.Duration) (<-chan time.Time, func() bool)
	now      func() time.Time
	// afterUnservedFlight, when set by a test, runs in the caller's goroutine
	// each time a flight it waited on did not serve it, before the caller
	// loops. It is a deterministic seam for the patience tests only.
	afterUnservedFlight func(waited int)
	// callObserver, when set by a test or measurement harness, receives the
	// wall time of every get call, whether it waited on a flight, and whether it
	// led a load. It never runs in production.
	callObserver func(took time.Duration, waited, led bool)
}

// NewIdentityEpochCache constructs the identity epoch cache. maxBytes caps
// the cached set; 0 disables the cap (uses defaultIdentityCacheMaxBytes).
// Returns (nil, nil) if maxBytes is negative (cache disabled — callers use the
// uncached path). inst must be non-nil when cache is enabled. Options tune the
// bound on how long one caller may spend inside the cache.
func NewIdentityEpochCache(
	inst *telemetry.Instruments,
	maxBytes int64,
	opts ...IdentityEpochCacheOption,
) (*IdentityEpochCache, error) {
	if maxBytes < 0 {
		return nil, nil
	}
	if maxBytes == 0 {
		maxBytes = defaultIdentityCacheMaxBytes
	}
	if inst == nil {
		return nil, nil
	}
	cache := &IdentityEpochCache{
		maxBytes: maxBytes,
		inst:     inst,
	}
	for _, opt := range opts {
		opt(cache)
	}
	return cache, nil
}

// get serves the identity fact set, transparently applying the epoch cache
// and the shared flight.
//
// Every caller first probes the epoch (no lock held). Then, under mu:
//   - a cache whose epoch equals the probe serves a defensive copy;
//   - an in-flight load whose start epoch equals the probe is joined: the
//     caller receives that flight's rows whether or not the flight ends up
//     cached, so N callers behind one load cost one load (#7805);
//   - an in-flight load that started from a different epoch is waited out and
//     the caller then retries, so a caller never gets a set older than the
//     active set it observed;
//   - otherwise the caller becomes the leader of a new flight, unless it has
//     used up its wait budget (3 unserved flights or one heartbeat interval of
//     total waiting): such a caller never leads, it fails retryably and the
//     next caller leads with a fresh budget. A caller that still has budget may
//     lead, with at most 2 load attempts.
func (c *IdentityEpochCache) get(ctx context.Context, store *FactStore) ([]facts.Envelope, error) {
	unserved := 0
	var waitedFor time.Duration
	waited, led := false, false
	if c.callObserver != nil {
		callStart := time.Now()
		defer func() { c.callObserver(time.Since(callStart), waited, led) }()
	}
	for {
		// Probe the epoch WITHOUT the lock held, so concurrent callers' probes
		// overlap instead of serializing behind mu.
		probeStart := time.Now()
		probe, err := store.probeIdentityEpoch(ctx)
		c.inst.IdentityCacheProbeDuration.Record(ctx, time.Since(probeStart).Seconds())
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		if flight := c.loading; flight != nil {
			flight.waiters++
			waited = true
			joinable := flight.startEpoch == probe
			c.mu.Unlock()
			if err := c.waitForFlight(ctx, flight, &waitedFor); err != nil {
				if errors.Is(err, errIdentityWaitExpired) {
					return c.giveUp(ctx, store, identityWaiterGaveUpWall, identityGaveUpWallClock)
				}
				return nil, err
			}
			if rows, served, flightErr := c.settleWaiter(ctx, flight, joinable); served {
				return rows, flightErr
			}
			unserved++
			if c.afterUnservedFlight != nil {
				c.afterUnservedFlight(unserved)
			}
			if unserved >= maxIdentityWaiterFlights {
				return c.giveUp(ctx, store, identityWaiterGaveUpFlights, identityGaveUpFlights)
			}
			continue
		}

		if c.facts != nil && c.epoch == probe {
			result := defensiveCopyEnvelopes(c.facts)
			c.inst.IdentityCacheHitTotal.Add(ctx, 1)
			c.mu.Unlock()
			return result, nil
		}

		if c.waitBudgetUsedUp(waitedFor) {
			// The caller spent its whole wait budget on other callers' flights.
			// A caller that has used up its budget never leads: its item fails
			// retryably and the next caller, with a fresh budget, leads (#7805).
			c.mu.Unlock()
			return nil, c.gaveUp(ctx, identityWaiterGaveUpWall, identityGaveUpWallClock)
		}

		c.inst.IdentityCacheMissTotal.Add(ctx, 1)
		flight := &identityFlight{done: make(chan struct{}), startEpoch: probe}
		c.loading = flight
		c.mu.Unlock()
		led = true
		return c.lead(ctx, store, flight)
	}
}

// settleWaiter resolves a caller that parked on a finished flight. It reports
// served=true with the flight's rows or error when the caller is done, and
// served=false when the caller must retry from the top: its probe predates a
// newer active set than the flight started from, or the leader gave up on its
// own context and its error belongs to the leader alone, or the flight's set
// could not be validated (torn) and was discarded.
func (c *IdentityEpochCache) settleWaiter(
	ctx context.Context,
	flight *identityFlight,
	joinable bool,
) ([]facts.Envelope, bool, error) {
	if !joinable {
		c.inst.IdentityCacheFlightWaiterTotal.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrOutcome(identityWaiterStaleEpoch)))
		return nil, false, nil
	}
	if flight.leaderCanceled {
		c.inst.IdentityCacheFlightWaiterTotal.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrOutcome(identityWaiterLeaderCanceled)))
		return nil, false, nil
	}
	if flight.torn {
		c.inst.IdentityCacheFlightWaiterTotal.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrOutcome(identityWaiterTornSet)))
		return nil, false, nil
	}
	if flight.err != nil {
		c.inst.IdentityCacheFlightWaiterTotal.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrOutcome(identityWaiterSharedError)))
		return nil, true, flight.err
	}
	c.inst.IdentityCacheFlightWaiterTotal.Add(ctx, 1,
		metric.WithAttributes(telemetry.AttrOutcome(identityWaiterShared)))
	return defensiveCopyEnvelopes(flight.rows), true, nil
}

// maxIdentityLoadAttempts bounds how many times one flight loads the identity
// set. The paged load is many READ COMMITTED statements, so a generation flip
// that lands between pages can tear it (old-generation rows after the cursor
// drop out, new-generation rows before the cursor are never read). The
// post-load probe detects such a flip; the flight then loads once more from
// the moved epoch. A flight whose epoch is still moving after this many loads
// stops retrying: the waiters re-probe and the leader's item fails with a
// retryable identityLoadUnstableError, so a possibly torn set is never used to
// decide any item (#7805).
const maxIdentityLoadAttempts = 2

// lead runs the load for a flight this caller created, publishes the outcome to
// every waiter, and caches the rows only when the active set did not move while
// the load ran. When it moved, the load is retried inside the same flight (see
// maxIdentityLoadAttempts). The flight is always closed and cleared, even on
// error or panic.
func (c *IdentityEpochCache) lead(
	ctx context.Context,
	store *FactStore,
	flight *identityFlight,
) ([]facts.Envelope, error) {
	finished := false
	defer func() {
		// A panic in the load must not strand the waiters on a flight that
		// never closes; fail them with an error and let the panic continue.
		if !finished {
			flight.err = errors.New("identity fact load aborted before it finished")
			c.finish(flight, nil)
		}
	}()

	for attempt := 1; ; attempt++ {
		c.inst.IdentityCacheReloadTotal.Add(ctx, 1)
		reloadStart := time.Now()
		loaded, err := store.loadIdentityFactsUncached(ctx)
		c.inst.IdentityCacheReloadDuration.Record(ctx, time.Since(reloadStart).Seconds())
		if err != nil {
			flight.err = err
			flight.leaderCanceled = ctx.Err() != nil &&
				(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
			finished = true
			c.finish(flight, nil)
			return nil, err
		}

		verdict := c.judgeLoad(ctx, store, flight, loaded)
		if verdict.retry && attempt < maxIdentityLoadAttempts {
			c.inst.IdentityCacheLoadRetryTotal.Add(ctx, 1)
			c.mu.Lock()
			flight.startEpoch = verdict.postProbe
			c.mu.Unlock()
			continue
		}

		if !verdict.cacheable {
			c.inst.IdentityCachePassthroughTotal.Add(ctx, 1,
				metric.WithAttributes(telemetry.AttrReason(verdict.reason)))
		}
		if verdict.retry || verdict.reason == identityDiscardProbeError {
			// The set could not be validated against a stable epoch. Nobody
			// decides on it: the waiters re-probe, and the leader's own item
			// gets a retryable error so the queue re-runs it from a fresh
			// probe (F17).
			flight.torn = true
			finished = true
			c.finish(flight, nil)
			return nil, newIdentityLoadUnstableError(verdict.reason)
		}
		flight.rows = loaded
		finished = true
		c.finish(flight, func() {
			if verdict.cacheable {
				// Cache the epoch the load started from (raw fact_records
				// state), not the loaded set's self-epoch, so later probes match.
				c.epoch = flight.startEpoch
				c.facts = loaded
			}
		})
		return defensiveCopyEnvelopes(loaded), nil
	}
}

// loadVerdict is the outcome of validating one finished load against the
// active set. retry means the epoch moved during the load, so the set may be
// torn. cacheable means the set may be retained. reason names the closed
// passthrough reason when it is not cacheable.
type loadVerdict struct {
	cacheable bool
	retry     bool
	reason    string
	postProbe identityEpoch
}

// judgeLoad validates a finished load. A set is cacheable only when the
// post-load probe equals the flight's start epoch (nothing moved while the load
// ran) and the set fits the byte cap. A probe error leaves the set unvalidated:
// it is treated as possibly torn but not retried, because the database that
// failed the probe would likely fail the retry too. A sizing error counts as
// "does not fit": a set that cannot be sized cannot be proven to fit.
func (c *IdentityEpochCache) judgeLoad(
	ctx context.Context,
	store *FactStore,
	flight *identityFlight,
	loaded []facts.Envelope,
) loadVerdict {
	postProbeStart := time.Now()
	postProbe, postProbeErr := store.probeIdentityEpoch(ctx)
	c.inst.IdentityCacheProbeDuration.Record(ctx, time.Since(postProbeStart).Seconds())
	if postProbeErr != nil {
		return loadVerdict{reason: identityDiscardProbeError}
	}
	if postProbe != flight.startEpoch {
		return loadVerdict{retry: true, reason: identityDiscardEpochMoved, postProbe: postProbe}
	}
	estBytes, sizeErr := estimateEnvelopesByteSize(loaded)
	if sizeErr != nil {
		return loadVerdict{reason: identityDiscardSizeUnknown, postProbe: postProbe}
	}
	if c.maxBytes > 0 && estBytes > c.maxBytes {
		return loadVerdict{reason: identityDiscardCapExceeded, postProbe: postProbe}
	}
	return loadVerdict{cacheable: true, postProbe: postProbe}
}

// finish clears the in-flight marker and wakes every waiter. publish, when
// non-nil, runs under mu before the flight clears so a caller that probes next
// sees the populated cache rather than an empty gap.
func (c *IdentityEpochCache) finish(flight *identityFlight, publish func()) {
	c.mu.Lock()
	if publish != nil {
		publish()
	}
	c.loading = nil
	c.mu.Unlock()
	close(flight.done)
}
