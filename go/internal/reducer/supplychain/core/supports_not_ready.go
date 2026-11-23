// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "fmt"

// SupplyChainImpactSupportsNotReadyFailureClass classifies a reducer-side read
// of the per-support table refused because the supports backfill has not
// completed at this binary's contract version, or the old-writer fence still
// holds dirty scopes (#10880 PR-B2, arbiter-b2c decision 2). It is enrolled in
// nonCountingReducerRetryFailureClasses
// (go/internal/storage/postgres/reducer_queue_readiness_sql.go): the backfill
// takes hours on a large install, and a counting class would dead-letter the
// intent after about three minutes. It is deliberately not parked under a wait
// kind (a not-a-wait row in WaitingDeferralMap): the park-time satisfied check
// would reopen at once, so the retry timer is the only wake.
const SupplyChainImpactSupportsNotReadyFailureClass = "supply_chain_impact_supports_not_ready"

// SupplyChainImpactSupportsNotReadyError is the one refusal every reducer-side
// reader of the supports table returns while the table is not ready. The reader
// refuses instead of answering from a partial table: an empty or short answer
// would silently drop findings from a decision, a retraction or a reopen. The
// refusal is retryable and an expected wait, so the queue defers the intent
// without spending an attempt and the service records it as deferred, not
// failed.
//
// A readiness probe that itself fails is not this error: it keeps its own
// classification and never takes the non-counting class.
type SupplyChainImpactSupportsNotReadyError struct {
	// Reader names the reader that refused (the reader label of
	// eshu_dp_supply_chain_impact_supports_reads_total).
	Reader string
}

// Error implements error.
func (e *SupplyChainImpactSupportsNotReadyError) Error() string {
	return fmt.Sprintf("supply chain impact supports are not ready for reader %q: the backfill has not completed at this contract version or the old-writer fence is not empty", e.Reader)
}

// Retryable marks the refusal retryable: it clears when the backfill completes.
func (*SupplyChainImpactSupportsNotReadyError) Retryable() bool { return true }

// ExpectedWait marks the refusal as a designed wait, recorded as deferred.
func (*SupplyChainImpactSupportsNotReadyError) ExpectedWait() bool { return true }

// FailureClass names the non-counting readiness class for the queue.
func (*SupplyChainImpactSupportsNotReadyError) FailureClass() string {
	return SupplyChainImpactSupportsNotReadyFailureClass
}
