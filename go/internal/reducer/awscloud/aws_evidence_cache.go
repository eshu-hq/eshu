// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// AWSDriftEvidenceCache shares one AWS drift evidence load per input
// version across concurrent workers (#10878 AC5). The key is
// scope|generation|fingerprint: callers on the same version park on the
// leader's flight (exactly-once per input version); a caller whose version
// moved loads for itself rather than risk a stale set (the
// IdentityEpochCache startEpoch rule). Payloads over maxBytes pass through
// observably: they are served but never stored, and each pass-through
// counts, so a pathological generation cannot evict the fleet's peers.
type AWSDriftEvidenceCache struct {
	maxBytes    int64
	mu          sync.Mutex
	flights     map[string]*awsDriftFlight
	passthrough atomic.Int64
}

// awsDriftFlight is one in-flight evidence load. rows and err are written
// by the leader before done closes; the close is the happens-before edge
// for parked waiters.
type awsDriftFlight struct {
	done chan struct{}
	rows []cloudruntime.AddressedRow
	size int64
	err  error
}

// NewAWSDriftEvidenceCache returns a cache holding at most maxBytes of
// shared evidence rows. A non-positive maxBytes disables storage: every
// load passes through and counts.
func NewAWSDriftEvidenceCache(maxBytes int64) *AWSDriftEvidenceCache {
	return &AWSDriftEvidenceCache{flights: make(map[string]*awsDriftFlight), maxBytes: maxBytes}
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
		c.mu.Unlock()
		select {
		case <-f.done:
			return f.rows, true, f.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	flight := &awsDriftFlight{done: make(chan struct{})}
	c.flights[key] = flight
	c.mu.Unlock()

	flight.rows, flight.size, flight.err = load(ctx)
	if flight.err == nil && flight.size <= c.maxBytes {
		close(flight.done)
		return flight.rows, false, nil
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

// PassthroughCount returns the lifetime count of over-cap evidence loads
// served without storing.
func (c *AWSDriftEvidenceCache) PassthroughCount() int64 {
	return c.passthrough.Load()
}
