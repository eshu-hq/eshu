// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Cause is the closed reason a truth response is not fresh.
type Cause = querycontract.FreshnessCause

// NextCheck is a bounded follow-up call for one freshness cause.
type NextCheck = querycontract.FreshnessNextCheck

// Cause aliases preserve querycontract's closed enumeration.
const (
	CausePendingRepoGeneration      = querycontract.FreshnessCausePendingRepoGeneration
	CauseReducerBacklog             = querycontract.FreshnessCauseReducerBacklog
	CauseDeadLetteredDomain         = querycontract.FreshnessCauseDeadLetteredDomain
	CauseMissingCollectorCompletion = querycontract.FreshnessCauseMissingCollectorCompletion
	CauseContentCoverageUnavailable = querycontract.FreshnessCauseContentCoverageUnavailable
	CauseUnsupportedProfile         = querycontract.FreshnessCauseUnsupportedProfile
	CauseRetentionExpired           = querycontract.FreshnessCauseRetentionExpired
	CausePendingSearchVector        = querycontract.FreshnessCausePendingSearchVector
)

// ValidCause reports whether cause belongs to the closed enumeration.
func ValidCause(cause Cause) bool {
	return querycontract.ValidFreshnessCause(cause)
}

// CauseNextCheck returns the bounded follow-up for a known cause.
func CauseNextCheck(cause Cause) (NextCheck, bool) {
	return querycontract.FreshnessCauseNextCheck(cause)
}

// WithCause attaches a proven cause to a non-fresh envelope.
func WithCause(truth *querycontract.TruthEnvelope, cause Cause) {
	querycontract.WithFreshnessCause(truth, cause)
}

// NextCheckAsRecommendedCall keeps prompt rendering in one place. Exported
// (#6642) because root package query's staying status_freshness_causality.go
// (StatusHandler.getFreshnessCausality) renders it through the
// freshnessNextCheckAsRecommendedCall forwarder in freshness_alias.go.
func NextCheckAsRecommendedCall(next NextCheck) map[string]any {
	return querycontract.FreshnessNextCheckAsRecommendedCall(next)
}
