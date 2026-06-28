// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perfcontract

import "time"

// ClaimLatencyContract is the reducer queue claim-latency budget from
// reducer-claim-latency-gate.md. The check is relative to a same-shape baseline:
// p95 must stay within P95MaxMultiplier of the baseline, and the absolute p95
// must not increase by more than MaxAbsoluteIncrease. Both require a live
// Postgres benchmark at the documented depths, so the contract is operator-gated.
type ClaimLatencyContract struct {
	P95MaxMultiplier    float64
	MaxAbsoluteIncrease time.Duration
}

// ReducerClaimLatency returns the documented claim-latency budget.
func ReducerClaimLatency() ClaimLatencyContract {
	return ClaimLatencyContract{
		P95MaxMultiplier:    1.10,
		MaxAbsoluteIncrease: 60 * time.Second,
	}
}

// WithinBudget reports whether a measured p95 is within budget versus a
// baseline p95. It is the executable form of the documented rule, for the
// operator/remote run that has real measurements to feed it.
func (c ClaimLatencyContract) WithinBudget(baselineP95, measuredP95 time.Duration) bool {
	if measuredP95 > time.Duration(float64(baselineP95)*c.P95MaxMultiplier) {
		return false
	}
	return measuredP95-baselineP95 <= c.MaxAbsoluteIncrease
}

func reducerClaimLatencyThresholds() []Threshold {
	const doc = DocClaimLatency
	return []Threshold{
		{Name: "claim_p95_max_multiplier", Doc: doc, Phrase: "p95 claim latency must not exceed 1.10x", Token: "1.10x", Value: 1.10, Unit: "x", Enforcement: EnforcementOperatorGated},
		{Name: "claim_max_absolute_increase", Doc: doc, Phrase: "must not increase by more than 60 seconds", Token: "60 seconds", Value: 60, Unit: "seconds", Enforcement: EnforcementOperatorGated},
	}
}
