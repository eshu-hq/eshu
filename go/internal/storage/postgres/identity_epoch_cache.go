// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

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
// The fingerprint is a collision-resistant md5 digest of the ordered active-
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
	loading chan struct{} // non-nil while a singleflight reload is in flight

	maxBytes int64
	inst     *telemetry.Instruments
}

// NewIdentityEpochCache constructs the identity epoch cache. maxBytes caps
// the cached set; 0 disables the cap (uses defaultIdentityCacheMaxBytes).
// Returns (nil, nil) if maxBytes is negative (cache disabled — callers use the
// uncached path). inst must be non-nil when cache is enabled.
func NewIdentityEpochCache(inst *telemetry.Instruments, maxBytes int64) (*IdentityEpochCache, error) {
	if maxBytes < 0 {
		return nil, nil
	}
	if maxBytes == 0 {
		maxBytes = defaultIdentityCacheMaxBytes
	}
	if inst == nil {
		return nil, nil
	}
	return &IdentityEpochCache{
		maxBytes: maxBytes,
		inst:     inst,
	}, nil
}

// get serves the identity fact set, transparently applying the epoch cache
// and singleflight reload.
func (c *IdentityEpochCache) get(ctx context.Context, store *FactStore) ([]facts.Envelope, error) {
	// Fast path: check whether a reload is already in flight, under lock.
	c.mu.Lock()
	if c.loading != nil {
		waitCh := c.loading
		c.mu.Unlock()
		select {
		case <-waitCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// After the flight lands, retry: the cache may now be populated.
		return c.get(ctx, store)
	}
	c.mu.Unlock() // Release before I/O: mu must never be held across the probe.

	// Probe the epoch WITHOUT the lock held, so concurrent callers' probes
	// overlap instead of serializing behind mu (and behind each other's
	// possibly-uncancellable lock wait).
	probeStart := time.Now()
	probe, err := store.probeIdentityEpoch(ctx)
	c.inst.IdentityCacheProbeDuration.Record(ctx, time.Since(probeStart).Seconds())
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Double-check: another goroutine may have started a reload, or
	// repopulated the cache, while we were probing without the lock.
	if c.loading != nil {
		waitCh := c.loading
		c.mu.Unlock()
		select {
		case <-waitCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return c.get(ctx, store)
	}

	// Check cache hit.
	if c.facts != nil && c.epoch == probe {
		// Cache hit: serve with defensive copy.
		result := defensiveCopyEnvelopes(c.facts)
		c.inst.IdentityCacheHitTotal.Add(ctx, 1)
		c.mu.Unlock()
		return result, nil
	}

	// Cache miss: start a singleflight reload.
	c.inst.IdentityCacheMissTotal.Add(ctx, 1)
	c.loading = make(chan struct{})
	preLoadProbe := probe // save for post-load comparison
	c.mu.Unlock()

	// Singleflight leader: load from DB.
	c.inst.IdentityCacheReloadTotal.Add(ctx, 1)
	reloadStart := time.Now()
	loaded, err := store.loadIdentityFactsUncached(ctx)
	if err != nil {
		c.mu.Lock()
		close(c.loading)
		c.loading = nil
		c.mu.Unlock()
		return nil, err
	}

	// Post-load probe: verify the raw fact_records set did not change during the load.
	postProbeStart := time.Now()
	postProbe, postProbeErr := store.probeIdentityEpoch(ctx)
	c.inst.IdentityCacheProbeDuration.Record(ctx, time.Since(postProbeStart).Seconds())
	if postProbeErr != nil {
		c.mu.Lock()
		close(c.loading)
		c.loading = nil
		c.mu.Unlock()
		// Serve uncached on probe error (best effort).
		return loaded, nil
	}

	// If post-load probe disagrees with pre-load probe, a commit landed mid-load.
	// Serve the loaded rows uncached; next call retries.
	if postProbe != preLoadProbe {
		c.inst.IdentityCachePassthroughTotal.Add(ctx, 1)
		c.mu.Lock()
		close(c.loading)
		c.loading = nil
		c.mu.Unlock()
		return loaded, nil
	}

	// Cap check: if estimated bytes exceed maxBytes, passthrough uncached.
	// A sizing error (json.Marshal failed on some envelope's Payload) is
	// treated the same as cap-exceeded: if the set can't be sized, it can't
	// be proven to fit, so it is not safe to cache.
	estBytes, sizeErr := estimateEnvelopesByteSize(loaded)
	if sizeErr != nil || (c.maxBytes > 0 && estBytes > c.maxBytes) {
		c.inst.IdentityCachePassthroughTotal.Add(ctx, 1)
		c.mu.Lock()
		close(c.loading)
		c.loading = nil
		c.mu.Unlock()
		return loaded, nil
	}

	// Store in cache and wake waiters.
	// Cache the epoch from the pre-load probe (raw fact_records state),
	// not the loaded set's self-epoch, so subsequent probes match.
	c.mu.Lock()
	c.epoch = preLoadProbe
	c.facts = loaded
	close(c.loading)
	c.loading = nil
	c.mu.Unlock()

	c.inst.IdentityCacheReloadDuration.Record(ctx, time.Since(reloadStart).Seconds())

	return defensiveCopyEnvelopes(loaded), nil
}

// estimateEnvelopesByteSize returns a conservative byte estimate for a set of
// fact envelopes, used for the cache cap check. It returns an error if any
// envelope's Payload cannot be sized via json.Marshal (e.g. a NaN/Inf float or
// another value json.Marshal rejects). Callers MUST treat a non-nil error as
// "unsizable, do not cache" rather than substituting a 0 or estimated size:
// silently under-counting an unsizable payload could let it slip under the
// cache's maxBytes cap.
func estimateEnvelopesByteSize(loaded []facts.Envelope) (int64, error) {
	var total int64
	for _, env := range loaded {
		total += int64(len(env.FactID))
		total += int64(len(env.ScopeID))
		total += int64(len(env.GenerationID))
		total += int64(len(env.FactKind))
		total += int64(len(env.StableFactKey))
		total += int64(len(env.SchemaVersion))
		total += int64(len(env.CollectorKind))
		total += int64(len(env.SourceConfidence))
		total += int64(len(env.SourceRef.SourceSystem))
		total += int64(len(env.SourceRef.FactKey))
		total += int64(len(env.SourceRef.SourceURI))
		total += int64(len(env.SourceRef.SourceRecordID))
		// Estimate payload as its JSON serialization size.
		if env.Payload != nil {
			b, err := json.Marshal(env.Payload)
			if err != nil {
				return 0, fmt.Errorf("estimate envelope %s payload size: %w", env.FactID, err)
			}
			total += int64(len(b))
		}
		// Fixed overhead: each time.Time, int64, bool ~ 40 bytes.
		total += 40
	}
	return total, nil
}

// defensiveCopyEnvelopes returns a fresh slice with a one-level copy of each
// envelope's Payload map, so callers cannot mutate the shared cache through
// top-level payload keys. Nested payload values (e.g. entity_metadata maps)
// are shared by reference; identity-load callers are read-only by audit, and
// new callers must not mutate nested payload values.
func defensiveCopyEnvelopes(src []facts.Envelope) []facts.Envelope {
	dst := make([]facts.Envelope, len(src))
	for i, env := range src {
		dst[i] = env
		if env.Payload != nil {
			dst[i].Payload = make(map[string]any, len(env.Payload))
			for k, v := range env.Payload {
				dst[i].Payload[k] = v
			}
		}
	}
	return dst
}
