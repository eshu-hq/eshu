// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
)

// validateShared runs the checks both enqueue and claim paths require, with
// error messages that name the caller's side. Both validateEnqueue and
// validateClaim delegate here so a db-nil or ClaimDomain failure carries the
// correct side marker in error strings and wrapped stack traces.
//
// The earlier shape composed validateClaim on top of validateEnqueue, which
// produced enqueue-marked errors on the claim path for shared-check failures
// (see Copilot review of PR #196). Routing through validateShared with an
// explicit side label fixes that without duplicating the check bodies.
func (q ReducerQueue) validateShared(side string) error {
	if q.db == nil {
		return fmt.Errorf("reducer queue database is required for %s", side)
	}
	if q.ClaimDomain != "" && len(q.ClaimDomains) > 0 {
		return fmt.Errorf("reducer queue claim domain and claim domains both set for %s", side)
	}
	for _, domain := range q.effectiveClaimDomains() {
		if err := domain.Validate(); err != nil {
			return fmt.Errorf("reducer queue claim domain invalid for %s: %w", side, err)
		}
	}
	return nil
}

// validateEnqueue checks the inputs Enqueue needs to insert a reducer
// fact_work_items row. The enqueue SQL writes NULL for lease_owner and
// claim_until (see enqueueReducerBatchPrefix and the VALUES tuple in
// enqueueReducerBatch), so LeaseOwner and LeaseDuration are not part of the
// enqueue contract. Splitting the check off from validateClaim removes the
// historical smell at drift_enqueue.go where producers had to fabricate
// placeholder lease values just to construct a struct used for enqueue only.
//
// Every error returned here carries the side marker "for enqueue" so wrapped
// errors and stack traces remain self-locating, including the shared checks
// delegated to validateShared.
func (q ReducerQueue) validateEnqueue() error {
	return q.validateShared("enqueue")
}

// validateClaim checks the inputs Claim, Heartbeat, Ack, and Fail need to
// fence reducer work by lease owner. LeaseOwner identifies the worker on the
// fact_work_items UPDATE statements (claim_until = $1, lease_owner = $3 in
// claimReducerWorkQuery; lease_owner = $3 in ackReducerWorkQuery and the
// heartbeat/fail variants). LeaseDuration sets claim_until on claim and
// renews it on heartbeat.
//
// validateClaim delegates the shared db != nil and ClaimDomain.Validate()
// checks to validateShared with the claim-side marker, so shared-check
// failures on the claim path are labeled "for claim/ack/heartbeat/fail"
// instead of inheriting the enqueue-side marker.
//
// Every error returned here carries the side marker
// "for claim/ack/heartbeat/fail" so wrapped errors remain self-locating.
func (q ReducerQueue) validateClaim() error {
	if err := q.validateShared("claim/ack/heartbeat/fail"); err != nil {
		return err
	}
	if q.LeaseOwner == "" {
		return errors.New("reducer queue lease owner is required for claim/ack/heartbeat/fail")
	}
	if q.LeaseDuration <= 0 {
		return errors.New("reducer queue lease duration must be positive for claim/ack/heartbeat/fail")
	}
	return nil
}
