// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Maintenance-reopen circuit breaker (reopen storm bound).
//
// The deferred-maintenance reopen is conservative by design: a partition the
// pass cannot prove redundant is reopened, so a skipped replay retries next
// pass while nothing wedges. That design inverts when the redundancy SIGNAL
// collapses fleet-wide — a catalog-fingerprint flip under a withheld fan-in
// publication turns every pass into a near-total miss (eshu QA, Nov 27:
// 64→3→50→442→670 reopen waves on pass cadence with 0 memo writes in 36h,
// so misses never healed and each wave reopened the whole floor pool while
// the reducer could not drain it). The breaker restores the asymmetry: a
// same-pass gate outcome that would reopen more than half of a non-trivial
// candidate pool parks the pass instead of reopening it. Parked rows stay
// 'succeeded' and are re-listed by the next pass, so no owed replay is
// dropped; a storm that would wedge the queue becomes a bounded one-pass
// delay plus one alertable line.
//
// Scope: the breaker applies ONLY to the periodic maintenance pass
// (reopenMaintenanceWorkItemsInTransaction), i.e. when the caller supplies a
// non-nil same-pass skip-set — including the degraded pass's authoritative
// empty set. A nil skip-set is an explicit standalone one-shot
// (bootstrap-index's Reopen* phases) with no periodic loop behind it, so the
// breaker never applies there, however large the pool.
const (
	// maintenanceReopenBreakerMinCandidates floors the breaker: below this
	// many candidates a total miss still reopens, so tiny corpora and
	// one-shot passes never stall on it.
	maintenanceReopenBreakerMinCandidates = 20
	// maintenanceReopenBreakerMaxMissFraction is the largest fraction of a
	// pass's candidate pool that may reopen before the pass parks. Above it
	// the miss almost certainly names a collapsed signal (fingerprint flip,
	// withheld publication), not per-partition news.
	maintenanceReopenBreakerMaxMissFraction = 0.5
)

// evaluateReopenCircuitBreaker decides whether a same-pass reopen outcome
// parks. It is pure so the boundary is pinnable without a database.
func evaluateReopenCircuitBreaker(candidates, toReopen int) (park bool, missFraction float64) {
	if candidates < maintenanceReopenBreakerMinCandidates || candidates <= 0 {
		return false, 0
	}
	if toReopen < 0 {
		toReopen = 0
	}
	if toReopen > candidates {
		toReopen = candidates
	}
	missFraction = float64(toReopen) / float64(candidates)
	return missFraction > maintenanceReopenBreakerMaxMissFraction, missFraction
}

// reportReopenCircuitBreakerOpen emits the breaker's operator signals for
// one parked domain: the alertable
// maintenance_reopen_circuit_breaker_open log line and the
// eshu_dp_maintenance_reopen_parked_by_breaker_total counter. The reopen
// block span carries parked_by_breaker through the caller's ReopenBlockAttrs.
func reportReopenCircuitBreakerOpen(
	ctx context.Context,
	domain string,
	candidates, parked int,
	missFraction float64,
	instruments *telemetry.Instruments,
) {
	log.Printf(
		"maintenance_reopen_circuit_breaker_open domain=%s candidates=%d parked=%d miss_fraction=%.4f threshold=%.2f pool_min=%d",
		domain, candidates, parked, missFraction,
		maintenanceReopenBreakerMaxMissFraction, maintenanceReopenBreakerMinCandidates,
	)
	if instruments != nil {
		instruments.MaintenanceReopenParkedByBreaker.Add(ctx, int64(parked),
			metric.WithAttributes(attribute.String("domain", domain)))
	}
}
