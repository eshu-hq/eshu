package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Maintainer for the #3466 collector-readiness evidence summary read model. See
// docs/internal/design/collector-readiness-evidence-summary-materialization-design.md.
//
// Correctness-first design choice (mirrors the #3389 winners maintainer): the
// maintainer reconciles collector_evidence_summary with the current active fact
// set by running the atomic, idempotent full resweep
// (RebuildAllCollectorEvidence: upsert-all + delete-stale in one statement) on
// startup and on a fixed cadence. This deliberately avoids per-scope dirty
// tracking because the atomic reconcile cannot miss a change class —
// generation-activation flips, tombstones (set via re-upsert, not a dedicated
// UPDATE), hard deletes, and FK-cascade generation prunes are all captured by
// recomputing from the current active set, removing the "missed dirty signal"
// correctness risk an incremental design would carry. Incremental per-scope
// recompute remains a future performance optimization.
//
// Conflict domain: the whole collector_evidence_summary table during a resweep. A
// single-owner partition lease (partitionCount = 1) keeps exactly one reducer
// instance resweeping at a time, so concurrent resweeps never contend on the
// table. The idempotent rebuild is the backstop if the lease is lost mid-run: the
// next owner's resweep reconciles to the same state. The resweep stays off the
// hot fact-write path, so it adds no per-row write cost and no counter-row
// contention against the live ingestion/reducer write load.
//
// Cadence vs freshness: the summary's MAX(observed_at)/MAX(ingested_at) feed the
// collector promotion stale verdict (status.evidenceIsStale). The default 60s
// cadence is ~1440x smaller than status.DefaultCollectorPromotionStaleAfter
// (24h), so a one-cadence materialization lag can never flip a stale verdict.

const (
	// CollectorEvidenceSummaryDomain names the maintainer's single-owner lease
	// partition. It is not a shared-projection edge domain; it only scopes the
	// lease so one instance resweeps at a time.
	CollectorEvidenceSummaryDomain = "collector_evidence_summary"

	defaultCollectorEvidenceSummaryLeaseOwnerName = "collector-evidence-summary-maintainer"
	defaultCollectorEvidenceSummaryInterval       = 60 * time.Second
	defaultCollectorEvidenceSummaryLeaseTTL       = 120 * time.Second
	maxCollectorEvidenceSummaryBackoff            = 5 * time.Minute
)

// CollectorEvidenceSummaryRebuilder reconciles collector_evidence_summary to the
// current active fact set. Implemented by
// postgres.CollectorEvidenceSummaryStore.RebuildAllCollectorEvidence.
type CollectorEvidenceSummaryRebuilder interface {
	RebuildAllCollectorEvidence(ctx context.Context, materializedAt any) error
}

// CollectorEvidenceSummaryLeaseManager is the single-owner lease seam, satisfied
// by the existing shared-intent partition lease store.
type CollectorEvidenceSummaryLeaseManager interface {
	ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string, leaseTTL time.Duration) (bool, error)
	ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string) error
}

// CollectorEvidenceFreshnessLookup reports the newest resweep watermark
// (max materialized_at) in collector_evidence_summary, or ok=false when the
// summary has never been materialized. Implemented by
// postgres.CollectorEvidenceSummaryStore.LastCollectorEvidenceMaterializedAt.
//
// It is the durable last-materialized guard: because the lease is released after
// each resweep, every reducer replica could otherwise reclaim it and run the full
// O(active facts) scan on its own cadence. Checking this watermark under the lease
// lets a replica skip a resweep when the summary is already fresher than the
// cadence, so cluster-wide resweeps cap at ~one per cadence regardless of replica
// count while keeping fast crash failover (any replica may resweep when stale).
type CollectorEvidenceFreshnessLookup interface {
	LastCollectorEvidenceMaterializedAt(ctx context.Context) (time.Time, bool, error)
}

// CollectorEvidenceSummaryMaintainer runs the periodic atomic resweep that keeps
// the collector-readiness evidence summary current.
type CollectorEvidenceSummaryMaintainer struct {
	Rebuilder    CollectorEvidenceSummaryRebuilder
	LeaseManager CollectorEvidenceSummaryLeaseManager
	// Freshness is the durable last-materialized guard. When set, a held lease
	// resweep is skipped if the summary is newer than MinResweepInterval, so
	// multiple replicas reclaiming the released lease do not each run the full
	// O(active facts) scan every cadence. Nil disables the guard (always resweep).
	Freshness CollectorEvidenceFreshnessLookup
	// Now defaults to time.Now (UTC stamped at use). Injected in tests.
	Now func() time.Time
	// Interval is the resweep cadence; defaults to 60s.
	Interval time.Duration
	// MinResweepInterval is the freshness floor for the guard; a resweep is
	// skipped when the summary watermark is younger than this. Defaults to
	// Interval, so cluster-wide resweeps cap at ~one per cadence.
	MinResweepInterval time.Duration
	// LeaseOwner identifies this instance; defaults to a stable owner name.
	LeaseOwner string
	// LeaseTTL bounds a lost-owner takeover; defaults to 120s.
	LeaseTTL time.Duration
	Logger   *slog.Logger
}

func (m CollectorEvidenceSummaryMaintainer) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m CollectorEvidenceSummaryMaintainer) interval() time.Duration {
	if m.Interval > 0 {
		return m.Interval
	}
	return defaultCollectorEvidenceSummaryInterval
}

func (m CollectorEvidenceSummaryMaintainer) minResweepInterval() time.Duration {
	if m.MinResweepInterval > 0 {
		return m.MinResweepInterval
	}
	return m.interval()
}

func (m CollectorEvidenceSummaryMaintainer) leaseOwner() string {
	if m.LeaseOwner != "" {
		return m.LeaseOwner
	}
	return defaultCollectorEvidenceSummaryLeaseOwnerName
}

func (m CollectorEvidenceSummaryMaintainer) leaseTTL() time.Duration {
	if m.LeaseTTL > 0 {
		return m.LeaseTTL
	}
	return defaultCollectorEvidenceSummaryLeaseTTL
}

func (m CollectorEvidenceSummaryMaintainer) validate() error {
	if m.Rebuilder == nil {
		return fmt.Errorf("collector evidence summary maintainer requires a rebuilder")
	}
	if m.LeaseManager == nil {
		return fmt.Errorf("collector evidence summary maintainer requires a lease manager")
	}
	return nil
}

// RunOnce claims the single-owner lease and, if acquired, runs one atomic
// resweep. It returns rebuilt=true only when this instance held the lease and the
// resweep committed. The lease is always released before returning so a
// crashed/slow instance does not block takeover beyond the TTL.
func (m CollectorEvidenceSummaryMaintainer) RunOnce(ctx context.Context) (rebuilt bool, err error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	claimed, err := m.LeaseManager.ClaimPartitionLease(
		ctx, CollectorEvidenceSummaryDomain, 0, 1, m.leaseOwner(), m.leaseTTL(),
	)
	if err != nil {
		return false, fmt.Errorf("claim collector evidence summary lease: %w", err)
	}
	if !claimed {
		return false, nil
	}
	defer func() {
		if relErr := m.LeaseManager.ReleasePartitionLease(
			ctx, CollectorEvidenceSummaryDomain, 0, 1, m.leaseOwner(),
		); relErr != nil && err == nil {
			err = fmt.Errorf("release collector evidence summary lease: %w", relErr)
		}
	}()

	// Durable last-materialized guard: skip the expensive full resweep when the
	// summary is already fresher than the cadence. This bounds cluster-wide
	// resweeps to ~one per cadence even though the lease is released between runs
	// and multiple replicas reclaim it on their own timers.
	if m.Freshness != nil {
		last, ok, freshErr := m.Freshness.LastCollectorEvidenceMaterializedAt(ctx)
		if freshErr != nil {
			return false, fmt.Errorf("read collector evidence summary watermark: %w", freshErr)
		}
		if ok && m.now().Sub(last) < m.minResweepInterval() {
			return false, nil
		}
	}

	start := time.Now()
	if rbErr := m.Rebuilder.RebuildAllCollectorEvidence(ctx, m.now()); rbErr != nil {
		return false, fmt.Errorf("resweep collector evidence summary: %w", rbErr)
	}
	// Resweep duration is the operator's 3 AM signal for how long the full
	// reconcile takes against live fact volume; paired with the per-row
	// materialized_at watermark it shows both cost and freshness.
	if m.Logger != nil {
		m.Logger.Debug(
			"collector evidence summary resweep committed",
			"domain", CollectorEvidenceSummaryDomain,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
	return true, nil
}

// Run resweeps once immediately (startup backfill/reconcile) and then on the
// configured cadence until ctx is cancelled. Errors are logged and retried with
// exponential backoff capped at maxCollectorEvidenceSummaryBackoff; the loop never
// exits on a transient resweep error so the read model keeps converging.
func (m CollectorEvidenceSummaryMaintainer) Run(ctx context.Context) error {
	if err := m.validate(); err != nil {
		return err
	}
	backoff := m.interval()
	for {
		if ctx.Err() != nil {
			return nil
		}
		rebuilt, err := m.RunOnce(ctx)
		switch {
		case err != nil:
			if m.Logger != nil {
				m.Logger.Error("collector evidence summary resweep failed", "error", err)
			}
			backoff *= 2
			if backoff > maxCollectorEvidenceSummaryBackoff {
				backoff = maxCollectorEvidenceSummaryBackoff
			}
		default:
			// RunOnce logs the commit with its duration; nothing to add here.
			_ = rebuilt
			backoff = m.interval()
		}
		if err := m.wait(ctx, backoff); err != nil {
			return nil
		}
	}
}

// wait sleeps for d or returns early when ctx is cancelled.
func (m CollectorEvidenceSummaryMaintainer) wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
