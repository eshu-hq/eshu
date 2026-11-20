// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// DefaultAWSDriftEvidenceCacheMaxBytes caps the shared AWS drift evidence
// set in production: 64 MiB of AddressedRow estimates (see
// EstimateAWSDriftEvidenceBytes) across all stored input versions. It is a
// starting default, not a measured optimum: the budget only bounds memory,
// and over-cap loads pass through observably rather than failing, so an
// undersized value degrades to sharing less, never to wrong results.
// Override with ESHU_AWS_DRIFT_EVIDENCE_CACHE_MAX_BYTES.
const DefaultAWSDriftEvidenceCacheMaxBytes = 64 * 1024 * 1024

// AWSDriftEvidenceCache shares one AWS drift evidence load per input
// version across concurrent workers (#10878 AC5). The key is
// scope|generation|fingerprint: callers on the same version park on the
// leader's flight (exactly-once per input version); a caller whose version
// moved loads for itself rather than risk a stale set (the
// IdentityEpochCache startEpoch rule: the version in the key IS the epoch,
// so a moved version can never be served a stale flight).
//
// Stored flights share one total byte budget (maxBytes): when a new flight
// would exceed it the oldest stored flight evicts, so generations
// accumulate boundedly instead of forever. A stored flight older than the
// TTL (when set) serves nobody. Payloads over the whole budget pass
// through observably: they are served but never stored, and each
// pass-through counts, so a pathological generation cannot evict the
// fleet's peers.
type AWSDriftEvidenceCache struct {
	maxBytes int64
	ttl      time.Duration
	now      func() time.Time

	mu          sync.Mutex
	flights     map[string]*awsDriftFlight
	storedBytes int64
	passthrough atomic.Int64
}

// awsDriftFlight is one in-flight or stored evidence load. rows and err are
// written by the leader before done closes; the close is the happens-before
// edge for parked waiters. storedAt is zero while the load is in flight and
// set when the flight is stored; it orders oldest-first eviction.
type awsDriftFlight struct {
	done     chan struct{}
	rows     []cloudruntime.AddressedRow
	size     int64
	err      error
	storedAt time.Time
}

// AWSDriftEvidenceCacheOption tunes a cache. The zero option set keeps the
// constructor behavior: budget eviction only, no expiry.
type AWSDriftEvidenceCacheOption func(*AWSDriftEvidenceCache)

// WithAWSDriftEvidenceCacheTTL expires stored flights older than ttl. A
// non-positive ttl disables expiry. now supplies the clock; nil means the
// process clock. Tests inject a deterministic now.
func WithAWSDriftEvidenceCacheTTL(ttl time.Duration, now func() time.Time) AWSDriftEvidenceCacheOption {
	return func(c *AWSDriftEvidenceCache) {
		c.ttl = ttl
		if now != nil {
			c.now = now
		}
	}
}

// NewAWSDriftEvidenceCache returns a cache holding at most maxBytes of
// shared evidence rows. A non-positive maxBytes disables storage: every
// load passes through and counts.
func NewAWSDriftEvidenceCache(maxBytes int64, opts ...AWSDriftEvidenceCacheOption) *AWSDriftEvidenceCache {
	c := &AWSDriftEvidenceCache{flights: make(map[string]*awsDriftFlight), maxBytes: maxBytes, now: time.Now}
	for _, opt := range opts {
		opt(c)
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// LoadShared returns the evidence rows for key, running load once per key
// no matter how many callers arrive together. shared is true when the
// caller parked on a peer's flight or a stored result; false when this
// call loaded (or passed through over the cap).
func (c *AWSDriftEvidenceCache) LoadShared(
	ctx context.Context,
	scopeID string,
	generationID string,
	inputVersion string,
	load func(context.Context) ([]cloudruntime.AddressedRow, int64, error),
) (rows []cloudruntime.AddressedRow, shared bool, err error) {
	key := scopeID + "|" + generationID + "|" + inputVersion
	c.mu.Lock()
	if f, ok := c.flights[key]; ok {
		if !f.storedAt.IsZero() && c.expiredLocked(f) {
			delete(c.flights, key)
			c.storedBytes -= f.size
		} else {
			c.mu.Unlock()
			select {
			case <-f.done:
				return f.rows, true, f.err
			case <-ctx.Done():
				return nil, false, ctx.Err()
			}
		}
	}
	c.sweepExpiredLocked()
	flight := &awsDriftFlight{done: make(chan struct{})}
	c.flights[key] = flight
	c.mu.Unlock()

	flight.rows, flight.size, flight.err = load(ctx)
	if flight.err == nil && c.maxBytes > 0 && flight.size <= c.maxBytes {
		c.mu.Lock()
		c.sweepExpiredLocked()
		for c.storedBytes+flight.size > c.maxBytes {
			if !c.evictOldestLocked() {
				break
			}
		}
		if c.storedBytes+flight.size <= c.maxBytes {
			flight.storedAt = c.now()
			c.storedBytes += flight.size
			c.mu.Unlock()
			close(flight.done)
			return flight.rows, false, nil
		}
		c.mu.Unlock()
	}
	// Failure or over-cap payload: drop the flight so the next caller
	// retries (failure) or reloads observably (passthrough), and never
	// serves a stale or oversized set to a parked peer.
	c.mu.Lock()
	delete(c.flights, key)
	c.mu.Unlock()
	if flight.err == nil {
		c.passthrough.Add(1)
	}
	close(flight.done)
	return flight.rows, false, flight.err
}

// expiredLocked reports whether a stored flight is older than the TTL.
// The caller holds mu; a zero TTL never expires.
func (c *AWSDriftEvidenceCache) expiredLocked(f *awsDriftFlight) bool {
	return c.ttl > 0 && c.now().Sub(f.storedAt) > c.ttl
}

// sweepExpiredLocked drops every stored flight older than the TTL. The
// caller holds mu. In-flight loads (zero storedAt) are never swept: expiry
// applies at store time, so a leader's waiters always get its rows.
func (c *AWSDriftEvidenceCache) sweepExpiredLocked() {
	if c.ttl <= 0 {
		return
	}
	for key, f := range c.flights {
		if f.storedAt.IsZero() {
			continue
		}
		if c.expiredLocked(f) {
			delete(c.flights, key)
			c.storedBytes -= f.size
		}
	}
}

// evictOldestLocked drops the oldest stored flight and reports whether one
// was dropped. The caller holds mu.
func (c *AWSDriftEvidenceCache) evictOldestLocked() bool {
	var oldestKey string
	var oldest *awsDriftFlight
	for key, f := range c.flights {
		if f.storedAt.IsZero() {
			continue
		}
		if oldest == nil || f.storedAt.Before(oldest.storedAt) {
			oldestKey, oldest = key, f
		}
	}
	if oldest == nil {
		return false
	}
	delete(c.flights, oldestKey)
	c.storedBytes -= oldest.size
	return true
}

// PassthroughCount returns the lifetime count of over-cap evidence loads
// served without storing.
func (c *AWSDriftEvidenceCache) PassthroughCount() int64 {
	return c.passthrough.Load()
}
