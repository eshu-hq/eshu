// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// partitionBackoffGraceVisits is the K=2 full-cadence unproductive
	// visits a partition gets before its backoff delay engages (#7724
	// 2A). A flapping partition (productive every other cycle) never
	// backs off.
	partitionBackoffGraceVisits = 2
	// DefaultPartitionBackoffMax is the default T_max: the longest a
	// persistently unproductive partition waits between visits. It is
	// also the pickup bound: work arriving during backoff is picked up
	// within T_max plus one global poll interval (at most 5s), the
	// idle sleep Run takes between cycles.
	DefaultPartitionBackoffMax = 30 * time.Second
	// MaxPartitionBackoffMax hard-caps the configured T_max so an
	// operator cannot silence a partition past five minutes.
	MaxPartitionBackoffMax = 5 * time.Minute
	// partitionBackoffMapSize pre-sizes the tracker for the fixed
	// domain x partition grid (11 domains x 8 partitions at defaults).
	partitionBackoffMapSize = 88
)

// partitionBackoffKey identifies one backoff cell: a (domain, partition)
// pair's consecutive-unproductive state.
type partitionBackoffKey struct {
	domain      string
	partitionID int
}

// partitionBackoffEntry is one cell's consecutive-unproductive state.
type partitionBackoffEntry struct {
	unproductive   int
	delay          time.Duration
	nextEligibleAt time.Time
}

// partitionBackoffTracker carries the per-(domain, partition) backoff
// state for one Runner (#7724 2A). A partition whose visits complete no
// intents backs off exponentially to T_max; any productive visit resets it
// to full cadence. Lease-not-acquired and error visits hold the counter:
// they report nothing, so progress elsewhere never penalizes a partition
// and errors never hide behind silence.
//
// Every partition is visited at least once per T_max at the cycle level,
// and skip decisions use only the partition's own counters, so no row
// waits more than T_max plus its partition-window scan plus one global
// poll interval (at most 5s of idle sleep between Run cycles). The zero
// value is not
// usable; construct with newPartitionBackoffTracker. It is safe for
// concurrent use by the runner's sequential and concurrent cycle paths.
type partitionBackoffTracker struct {
	mu           sync.Mutex
	pollInterval time.Duration
	maxBackoff   time.Duration
	entries      map[partitionBackoffKey]*partitionBackoffEntry
}

// newPartitionBackoffTracker builds a tracker with the runner's poll
// interval (the doubling base) and T_max cap.
func newPartitionBackoffTracker(pollInterval, maxBackoff time.Duration) *partitionBackoffTracker {
	return &partitionBackoffTracker{
		pollInterval: pollInterval,
		maxBackoff:   maxBackoff,
		entries:      make(map[partitionBackoffKey]*partitionBackoffEntry, partitionBackoffMapSize),
	}
}

// shouldSkip reports whether the partition must be skipped at now: its
// backoff delay has not elapsed since its last unproductive visit.
func (t *partitionBackoffTracker) shouldSkip(domain string, partitionID int, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[partitionBackoffKey{domain: domain, partitionID: partitionID}]
	if !ok {
		return false
	}
	return now.Before(entry.nextEligibleAt)
}

// reportProductive resets the partition to full cadence. Productive means
// ProcessedIntents > 0: any completion kind (projected, stale,
// superseded, terminal) proves the partition is draining.
func (t *partitionBackoffTracker) reportProductive(domain string, partitionID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[partitionBackoffKey{domain: domain, partitionID: partitionID}] = &partitionBackoffEntry{}
}

// reportUnproductive records one lease-acquired visit that completed no
// intents and returns the partition's new backoff delay: zero during the
// K=2 grace visits, then min(poll*2^(n-K), T_max). now anchors the next
// eligible visit.
func (t *partitionBackoffTracker) reportUnproductive(domain string, partitionID int, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := partitionBackoffKey{domain: domain, partitionID: partitionID}
	entry, ok := t.entries[key]
	if !ok {
		entry = &partitionBackoffEntry{}
		t.entries[key] = entry
	}
	entry.unproductive++
	entry.delay = t.delayLocked(entry.unproductive)
	entry.nextEligibleAt = now.Add(entry.delay)
	return entry.delay
}

// delayFor returns the partition's current backoff delay and whether it is
// pinned at T_max. A never-visited partition reports zero delay.
func (t *partitionBackoffTracker) delayFor(domain string, partitionID int) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[partitionBackoffKey{domain: domain, partitionID: partitionID}]
	if !ok {
		return 0, false
	}
	return entry.delay, entry.delay >= t.maxBackoff && entry.delay > 0
}

// countAtMax returns the number of partitions pinned at T_max.
func (t *partitionBackoffTracker) countAtMax() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := 0
	for _, entry := range t.entries {
		if entry.delay >= t.maxBackoff && entry.delay > 0 {
			count++
		}
	}
	return count
}

// delayLocked computes the backoff delay for n consecutive unproductive
// visits: full cadence during grace, then exponential doubling capped at
// T_max. The doubling loop caps iteratively so deep counts cannot shift
// the interval into overflow.
func (t *partitionBackoffTracker) delayLocked(n int) time.Duration {
	if n <= partitionBackoffGraceVisits {
		return 0
	}
	delay := t.pollInterval
	for i := partitionBackoffGraceVisits; i < n; i++ {
		delay *= 2
		if delay >= t.maxBackoff || delay <= 0 {
			return t.maxBackoff
		}
	}
	return delay
}

// PartitionBackoffSnapshot is one partition's backoff state for the
// debug/status surface (Runner.BackoffState).
type PartitionBackoffSnapshot struct {
	// Domain and PartitionID identify the tracked cell.
	Domain      string
	PartitionID int
	// UnproductiveVisits is the consecutive lease-acquired visits that
	// completed no intents. Zero means full cadence.
	UnproductiveVisits int
	// Delay is the current backoff delay: zero at full cadence, up to
	// T_max for a persistently unproductive partition.
	Delay time.Duration
	// NextEligibleAt is when the partition becomes visitable again.
	NextEligibleAt time.Time
	// AtMax reports the delay is pinned at T_max.
	AtMax bool
}

// snapshot returns every touched partition's state, sorted by domain and
// partition id. Reset cells stay tracked with zero state, so the surface
// shows the full active grid once the runner has swept it.
func (t *partitionBackoffTracker) snapshot() []PartitionBackoffSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]PartitionBackoffSnapshot, 0, len(t.entries))
	for key, entry := range t.entries {
		out = append(out, PartitionBackoffSnapshot{
			Domain:             key.domain,
			PartitionID:        key.partitionID,
			UnproductiveVisits: entry.unproductive,
			Delay:              entry.delay,
			NextEligibleAt:     entry.nextEligibleAt,
			AtMax:              entry.delay >= t.maxBackoff && entry.delay > 0,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].PartitionID < out[j].PartitionID
	})
	return out
}

// runnerBackoffInitMu serializes lazy tracker creation. It is
// package-level rather than a Runner field so copying a Runner value
// never copies a mutex: callers construct Runner by value.
var runnerBackoffInitMu sync.Mutex

// tracker returns the runner's backoff tracker, creating it on first use.
// Creation is serialized on runnerBackoffInitMu so BackoffState stays safe
// to call while the runner starts.
func (r *Runner) tracker() *partitionBackoffTracker {
	runnerBackoffInitMu.Lock()
	defer runnerBackoffInitMu.Unlock()
	if r.backoff == nil {
		r.backoff = newPartitionBackoffTracker(r.Config.pollInterval(), r.Config.backoffMax())
	}
	return r.backoff
}

// clock returns the runner's current time: the test override when set,
// time.Now otherwise.
func (r *Runner) clock() time.Time {
	if r.nowFn != nil {
		return r.nowFn()
	}
	return time.Now()
}

// BackoffState returns every touched partition's backoff state, sorted by
// domain and partition id. It is the debug/status surface for the
// per-partition backoff (#7724 telemetry): an operator can see which cells
// are backed off, their consecutive-unproductive depth, and when each
// becomes visitable again. Safe to call while the runner runs.
func (r *Runner) BackoffState() []PartitionBackoffSnapshot {
	return r.tracker().snapshot()
}

// recordCycleBackoff samples the per-cycle backoff gauges and, for
// backing-off cycles, logs one line with partitions visited vs skipped
// by reason (#7724 telemetry). Productive cycles stay quiet; they
// already log per-domain completions.
func (r *Runner) recordCycleBackoff(ctx context.Context, backoff time.Duration, result PartitionProcessResult) {
	r.sampleCycleBackoffGauges(ctx, backoff)
	atMax := r.tracker().countAtMax()
	if r.Logger != nil && (result.PartitionsBackoffSkipped > 0 || result.PartitionsLeaseHeld > 0 || backoff > r.Config.pollInterval() || atMax > 0) {
		r.Logger.InfoContext(
			ctx,
			"shared projection cycle backoff summary",
			slog.Int("partitions_visited", result.PartitionsVisited),
			slog.Int("partitions_backoff_skipped", result.PartitionsBackoffSkipped),
			slog.Int("partitions_lease_held", result.PartitionsLeaseHeld),
			slog.Float64("backoff_interval_seconds", backoff.Seconds()),
			slog.Int("partitions_at_max_backoff", atMax),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}
}

// sampleCycleBackoffGauges records the current global backoff interval
// and the count of partitions pinned at T_max. It runs every cycle —
// including productive ones, which record 0 (immediate re-poll, no
// wait) — so the gauges never go stale behind a drain.
func (r *Runner) sampleCycleBackoffGauges(ctx context.Context, interval time.Duration) {
	if r.Instruments == nil {
		return
	}
	r.Instruments.SharedProjectionCycleBackoff.Record(ctx, interval.Seconds())
	r.Instruments.SharedProjectionPartitionsAtMaxBackoff.Record(ctx, int64(r.tracker().countAtMax()))
}

// visitPartition runs one partition through per-partition backoff and
// processing (#7724 2A). A backed-off partition is skipped without a
// lease claim; otherwise the partition is processed and its outcome is
// reported to the tracker: productive visits (any completion kind) reset
// the cell, lease-acquired visits that complete nothing advance it, and
// lease-not-acquired or error visits hold it. The result always carries
// exactly one of the visited/backoff-skipped/lease-held counters (error
// visits count as visited), so the merged cycle accounts every cell.
func (r *Runner) visitPartition(
	ctx context.Context,
	now time.Time,
	domain string,
	partitionID int,
	partitionCount int,
) (PartitionProcessResult, error) {
	tracker := r.tracker()
	if tracker.shouldSkip(domain, partitionID, now) {
		delay, _ := tracker.delayFor(domain, partitionID)
		r.recordPartitionVisit(ctx, domain, partitionID, telemetry.SharedProjectionVisitOutcomeBackoffSkipped, delay)
		return PartitionProcessResult{PartitionsBackoffSkipped: 1}, nil
	}
	result, err := r.processPartitionWithTelemetry(ctx, now, domain, partitionID, partitionCount)
	if err != nil {
		// Error visits hold the counter: backing off errors would hide
		// faults behind silence. They still count as visited.
		delay, _ := tracker.delayFor(domain, partitionID)
		r.recordPartitionVisit(ctx, domain, partitionID, telemetry.SharedProjectionVisitOutcomeError, delay)
		result.PartitionsVisited = 1
		return result, err
	}
	if !result.LeaseAcquired {
		// Lease-not-acquired visits hold the counter: another owner is
		// working (or will), so this cell must not back off.
		delay, _ := tracker.delayFor(domain, partitionID)
		r.recordPartitionVisit(ctx, domain, partitionID, telemetry.SharedProjectionVisitOutcomeLeaseHeld, delay)
		result.PartitionsLeaseHeld = 1
		return result, nil
	}
	if result.ProcessedIntents > 0 {
		tracker.reportProductive(domain, partitionID)
	} else {
		tracker.reportUnproductive(domain, partitionID, now)
	}
	delay, _ := tracker.delayFor(domain, partitionID)
	r.recordPartitionVisit(ctx, domain, partitionID, telemetry.SharedProjectionVisitOutcomeVisited, delay)
	result.PartitionsVisited = 1
	return result, nil
}
