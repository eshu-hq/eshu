// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"time"
)

// InFlightClaim is one claimed or running findings item of another scope,
// named by its claim (#10972 D4b). ClaimedAt is the item's last_attempt_at:
// every claim of an item writes a strictly greater value
// (reducer_queue_claim_query.go, reducer_queue_batch_query.go), and a heartbeat
// or an acknowledgement only fences on it, so two reads that return the same
// (ScopeID, ClaimedAt) saw the same claim. A zero ClaimedAt is a claim with no
// recorded identity; it never matches, so such an item is always flagged.
type InFlightClaim struct {
	ScopeID   string
	ClaimedAt time.Time
}

// InFlightClaimReader is the claim-identity form of the in-flight read
// (#10972 D4b). A findings pass reads the in-flight set twice: once when its
// evidence load ends (set A) and again at fan-out (set B). It flags only the
// claims in both sets. A claim in B that is not in A was made after the
// source's load ended, so that pass loads its own evidence later and sees every
// fact the source saw; flagging it would only rerun it. Both reads leave out
// manifest markers and use the holder-read statement timeout.
type InFlightClaimReader interface {
	// FindInFlightNonMarkerClaims returns set B: the claims of the scopes,
	// other than ownScopeID, whose non-marker findings item at the scope's
	// active generation is claimed or running. It is the same row set as
	// FindInFlightNonMarkerScopes, with each item's claim identity, and fails
	// with ErrFanOutReadTimeout when it exceeds its timeout.
	FindInFlightNonMarkerClaims(ctx context.Context, ownScopeID string) ([]InFlightClaim, error)
	// SnapshotInFlightNonMarkerClaims returns set A: the claims of every
	// claimed or running non-marker findings item of another scope, at ANY
	// generation, so an item claimed before its generation became active is
	// still named. A larger A can only keep a flag, never drop one. Same
	// timeout contract.
	SnapshotInFlightNonMarkerClaims(ctx context.Context, ownScopeID string) ([]InFlightClaim, error)
}

// In-flight narrowing outcomes on eshu_dp_supply_chain_impact_fanout_total
// (#10972 D4b), one point per claim in set B, so the share a fan-out stopped
// flagging reads straight off the counter. A claim the row read also finds is
// reopened (and flagged) by that arm regardless.
const (
	// fanOutOutcomeInFlightNarrowed counts claims in set B that set A did not
	// hold: claimed after the source's load ended, so not flagged.
	fanOutOutcomeInFlightNarrowed = "in_flight_narrowed"
	// fanOutOutcomeInFlightKept counts claims in both sets: flagged.
	fanOutOutcomeInFlightKept = "in_flight_kept"
	// fanOutOutcomeInFlightEarlyReadFailed counts claims flagged because the
	// load-end read failed or timed out, so no claim could be proven new.
	fanOutOutcomeInFlightEarlyReadFailed = "in_flight_early_read_failed"
	// fanOutOutcomeInFlightEarlyReadSkipped counts claims flagged by a pass
	// that took no load-end read (a one-hop rerun).
	fanOutOutcomeInFlightEarlyReadSkipped = "in_flight_early_read_skipped"
)
