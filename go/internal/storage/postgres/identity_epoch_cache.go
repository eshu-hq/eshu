// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
// Two epochs are equal when both the count and the max observed_at match.
type identityEpoch struct {
	count         int
	maxObservedAt time.Time
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

	// Cache telemetry instruments.
	hitCounter         metric.Int64Counter
	missCounter        metric.Int64Counter
	reloadCounter      metric.Int64Counter
	passthroughCounter metric.Int64Counter
	reloadDuration     metric.Float64Histogram
	probeDuration      metric.Float64Histogram
}

// NewIdentityEpochCache constructs the identity epoch cache. maxBytes caps
// the cached set; 0 disables the cap (uses defaultIdentityCacheMaxBytes).
// Returns nil if maxBytes is negative (cache disabled — callers use the
// uncached path). meter must be non-nil when cache is enabled.
func NewIdentityEpochCache(meter metric.Meter, maxBytes int64) *IdentityEpochCache {
	if maxBytes < 0 {
		return nil
	}
	if maxBytes == 0 {
		maxBytes = defaultIdentityCacheMaxBytes
	}
	return newIdentityEpochCache(meter, maxBytes)
}

// newIdentityEpochCache constructs a ready-to-use identity epoch cache.
// maxBytes caps the cached set; 0 disables the cap (unlimited).
// meter must be non-nil.
func newIdentityEpochCache(meter metric.Meter, maxBytes int64) *IdentityEpochCache {
	c := &IdentityEpochCache{
		maxBytes: maxBytes,
	}
	if c.maxBytes <= 0 {
		c.maxBytes = defaultIdentityCacheMaxBytes
	}

	// Register cache telemetry instruments.
	var err error
	c.hitCounter, err = meter.Int64Counter(
		"eshu_dp_identity_cache_hit_total",
		metric.WithDescription("Total identity-fact cache hits"),
	)
	if err != nil {
		panic("register identity cache hit counter: " + err.Error())
	}
	c.missCounter, err = meter.Int64Counter(
		"eshu_dp_identity_cache_miss_total",
		metric.WithDescription("Total identity-fact cache misses (epoch changed → reload)"),
	)
	if err != nil {
		panic("register identity cache miss counter: " + err.Error())
	}
	c.reloadCounter, err = meter.Int64Counter(
		"eshu_dp_identity_cache_reload_total",
		metric.WithDescription("Total identity-fact cache reloads (singleflight leader)"),
	)
	if err != nil {
		panic("register identity cache reload counter: " + err.Error())
	}
	c.passthroughCounter, err = meter.Int64Counter(
		"eshu_dp_identity_cache_passthrough_total",
		metric.WithDescription("Total identity-fact cache passthroughs (cap exceeded or mid-load commit)"),
	)
	if err != nil {
		panic("register identity cache passthrough counter: " + err.Error())
	}
	c.reloadDuration, err = meter.Float64Histogram(
		"eshu_dp_identity_cache_reload_duration_seconds",
		metric.WithDescription("Duration of identity-fact cache reloads"),
	)
	if err != nil {
		panic("register identity cache reload duration histogram: " + err.Error())
	}
	c.probeDuration, err = meter.Float64Histogram(
		"eshu_dp_identity_cache_probe_duration_seconds",
		metric.WithDescription("Duration of identity-fact epoch probe queries"),
	)
	if err != nil {
		panic("register identity cache probe duration histogram: " + err.Error())
	}

	return c
}

// get serves the identity fact set, transparently applying the epoch cache
// and singleflight reload.
func (c *IdentityEpochCache) get(ctx context.Context, store *FactStore) ([]facts.Envelope, error) {
	// Fast path: check cache under lock.
	c.mu.Lock()

	// If a reload is already in flight, wait for it, then retry.
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

	// Probe the epoch.
	probeStart := time.Now()
	probe, err := store.probeIdentityEpoch(ctx)
	c.probeDuration.Record(ctx, time.Since(probeStart).Seconds())
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}

	// Check cache hit.
	if c.facts != nil && c.epoch == probe {
		// Cache hit: serve with defensive copy.
		result := defensiveCopyEnvelopes(c.facts)
		c.hitCounter.Add(ctx, 1)
		c.mu.Unlock()
		return result, nil
	}

	// Cache miss: start a singleflight reload.
	c.missCounter.Add(ctx, 1)
	c.loading = make(chan struct{})
	preLoadProbe := probe // save for post-load comparison
	c.mu.Unlock()

	// Singleflight leader: load from DB.
	c.reloadCounter.Add(ctx, 1)
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
	c.probeDuration.Record(ctx, time.Since(postProbeStart).Seconds())
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
		c.passthroughCounter.Add(ctx, 1)
		c.mu.Lock()
		close(c.loading)
		c.loading = nil
		c.mu.Unlock()
		return loaded, nil
	}

	// Cap check: if estimated bytes exceed maxBytes, passthrough uncached.
	estBytes := estimateEnvelopesByteSize(loaded)
	if c.maxBytes > 0 && estBytes > c.maxBytes {
		c.passthroughCounter.Add(ctx, 1)
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

	c.reloadDuration.Record(ctx, time.Since(reloadStart).Seconds())

	return defensiveCopyEnvelopes(loaded), nil
}

// estimateEnvelopesByteSize returns a conservative byte estimate for a set of
// fact envelopes, used for the cache cap check.
func estimateEnvelopesByteSize(loaded []facts.Envelope) int64 {
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
			b, _ := json.Marshal(env.Payload)
			total += int64(len(b))
		}
		// Fixed overhead: each time.Time, int64, bool ~ 40 bytes.
		total += 40
	}
	return total
}

// defensiveCopyEnvelopes returns a shallow copy of the slice and a deep copy
// of each envelope's Payload map, so callers cannot mutate the shared cache.
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
