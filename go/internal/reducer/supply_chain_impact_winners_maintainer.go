// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Maintainer for the #3389 supply-chain impact canonical winners read model. See
// docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md.
//
// Correctness-first design choice: the maintainer reconciles the winners table
// with the current active impact-fact set by running the atomic, idempotent full
// resweep (RebuildAllWinners: upsert-all + delete-stale in one statement) on
// startup and on a fixed cadence. This deliberately avoids per-canonical_key
// dirty tracking because the atomic reconcile cannot miss a change class —
// generation-activation flips, tombstones, and new sources are all captured by
// recomputing from the current active set, removing the "missed dirty signal"
// correctness risk the incremental design carried. Incremental per-key recompute
// remains a future performance optimization; the read reports freshness from the
// winners' materialized_at so a cadence lag is never served as fresh truth.
//
// Conflict domain: the whole winners table during a resweep. A single-owner
// partition lease (partitionCount = 1) keeps exactly one reducer instance
// resweeping at a time, so concurrent resweeps never contend on the table. The
// idempotent rebuild is the backstop if the lease is lost mid-run: the next
// owner's resweep reconciles to the same state.

const (
	// SupplyChainImpactWinnersDomain names the maintainer's single-owner lease
	// partition. It is not a shared-projection edge domain; it only scopes the
	// lease so one instance resweeps at a time.
	SupplyChainImpactWinnersDomain = "supply_chain_impact_winners"

	defaultSupplyChainImpactWinnersLeaseOwner = "supply-chain-impact-winners-maintainer"
	defaultSupplyChainImpactWinnersInterval   = 30 * time.Second
	defaultSupplyChainImpactWinnersLeaseTTL   = 60 * time.Second
	maxSupplyChainImpactWinnersBackoff        = 5 * time.Minute
)

// SupplyChainImpactWinnersRebuilder reconciles the winners table to the current
// active impact-fact set. Implemented by
// postgres.SupplyChainImpactWinnersStore.RebuildAllWinners.
type SupplyChainImpactWinnersRebuilder interface {
	RebuildAllWinners(ctx context.Context, materializedAt any) error
}

// SupplyChainImpactWinnersLeaseManager is the single-owner lease seam, satisfied
// by the existing shared-intent partition lease store.
type SupplyChainImpactWinnersLeaseManager interface {
	ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string, leaseTTL time.Duration) (bool, error)
	ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string) error
}

// SupplyChainImpactWinnersMaintainer runs the periodic atomic resweep.
type SupplyChainImpactWinnersMaintainer struct {
	Rebuilder    SupplyChainImpactWinnersRebuilder
	LeaseManager SupplyChainImpactWinnersLeaseManager
	// Now defaults to time.Now (UTC stamped at use). Injected in tests.
	Now func() time.Time
	// Interval is the resweep cadence; defaults to 30s.
	Interval time.Duration
	// LeaseOwner identifies this instance; defaults to a stable owner name.
	LeaseOwner string
	// LeaseTTL bounds a lost-owner takeover; defaults to 60s.
	LeaseTTL time.Duration
	Logger   *slog.Logger
}

func (m SupplyChainImpactWinnersMaintainer) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m SupplyChainImpactWinnersMaintainer) interval() time.Duration {
	if m.Interval > 0 {
		return m.Interval
	}
	return defaultSupplyChainImpactWinnersInterval
}

func (m SupplyChainImpactWinnersMaintainer) leaseOwner() string {
	if m.LeaseOwner != "" {
		return m.LeaseOwner
	}
	return defaultSupplyChainImpactWinnersLeaseOwner
}

func (m SupplyChainImpactWinnersMaintainer) leaseTTL() time.Duration {
	if m.LeaseTTL > 0 {
		return m.LeaseTTL
	}
	return defaultSupplyChainImpactWinnersLeaseTTL
}

func (m SupplyChainImpactWinnersMaintainer) validate() error {
	if m.Rebuilder == nil {
		return fmt.Errorf("supply chain impact winners maintainer requires a rebuilder")
	}
	if m.LeaseManager == nil {
		return fmt.Errorf("supply chain impact winners maintainer requires a lease manager")
	}
	return nil
}

// RunOnce claims the single-owner lease and, if acquired, runs one atomic
// resweep. It returns rebuilt=true only when this instance held the lease and
// the resweep committed. The lease is always released before returning so a
// crashed/slow instance does not block takeover beyond the TTL.
func (m SupplyChainImpactWinnersMaintainer) RunOnce(ctx context.Context) (rebuilt bool, err error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	claimed, err := m.LeaseManager.ClaimPartitionLease(
		ctx, SupplyChainImpactWinnersDomain, 0, 1, m.leaseOwner(), m.leaseTTL(),
	)
	if err != nil {
		return false, fmt.Errorf("claim winners lease: %w", err)
	}
	if !claimed {
		return false, nil
	}
	defer func() {
		if relErr := m.LeaseManager.ReleasePartitionLease(
			ctx, SupplyChainImpactWinnersDomain, 0, 1, m.leaseOwner(),
		); relErr != nil && err == nil {
			err = fmt.Errorf("release winners lease: %w", relErr)
		}
	}()

	if rbErr := m.Rebuilder.RebuildAllWinners(ctx, m.now()); rbErr != nil {
		return false, fmt.Errorf("resweep winners: %w", rbErr)
	}
	return true, nil
}

// Run resweeps once immediately (startup backfill/reconcile) and then on the
// configured cadence until ctx is cancelled. Errors are logged and retried with
// exponential backoff capped at maxSupplyChainImpactWinnersBackoff; the loop
// never exits on a transient resweep error so the read model keeps converging.
func (m SupplyChainImpactWinnersMaintainer) Run(ctx context.Context) error {
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
				m.Logger.Error("supply chain impact winners resweep failed", "error", err)
			}
			backoff *= 2
			if backoff > maxSupplyChainImpactWinnersBackoff {
				backoff = maxSupplyChainImpactWinnersBackoff
			}
		default:
			backoff = m.interval()
			if rebuilt && m.Logger != nil {
				m.Logger.Debug("supply chain impact winners resweep committed")
			}
		}
		if err := m.wait(ctx, backoff); err != nil {
			return nil
		}
	}
}

// wait sleeps for d or returns early when ctx is cancelled.
func (m SupplyChainImpactWinnersMaintainer) wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
