// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// FreshnessCause is the closed reason a truth response is not fresh.
type FreshnessCause = querycontract.FreshnessCause

// FreshnessNextCheck is a bounded follow-up call for one freshness cause.
type FreshnessNextCheck = querycontract.FreshnessNextCheck

// Freshness cause aliases preserve the root package's closed enumeration.
const (
	FreshnessCausePendingRepoGeneration      = querycontract.FreshnessCausePendingRepoGeneration
	FreshnessCauseReducerBacklog             = querycontract.FreshnessCauseReducerBacklog
	FreshnessCauseDeadLetteredDomain         = querycontract.FreshnessCauseDeadLetteredDomain
	FreshnessCauseMissingCollectorCompletion = querycontract.FreshnessCauseMissingCollectorCompletion
	FreshnessCauseContentCoverageUnavailable = querycontract.FreshnessCauseContentCoverageUnavailable
	FreshnessCauseUnsupportedProfile         = querycontract.FreshnessCauseUnsupportedProfile
	FreshnessCauseRetentionExpired           = querycontract.FreshnessCauseRetentionExpired
	FreshnessCausePendingSearchVector        = querycontract.FreshnessCausePendingSearchVector
)

// ValidFreshnessCause reports whether cause belongs to the closed enumeration.
func ValidFreshnessCause(cause FreshnessCause) bool {
	return querycontract.ValidFreshnessCause(cause)
}

// FreshnessCauseNextCheck returns the bounded follow-up for a known cause.
func FreshnessCauseNextCheck(cause FreshnessCause) (FreshnessNextCheck, bool) {
	return querycontract.FreshnessCauseNextCheck(cause)
}

// WithFreshnessCause attaches a proven cause to a non-fresh envelope.
func WithFreshnessCause(truth *TruthEnvelope, cause FreshnessCause) {
	querycontract.WithFreshnessCause(truth, cause)
}

// freshnessNextCheckAsRecommendedCall keeps prompt rendering in the root query package.
func freshnessNextCheckAsRecommendedCall(next FreshnessNextCheck) map[string]any {
	call := map[string]any{}
	if tool := strings.TrimSpace(next.Tool); tool != "" {
		call["tool"] = tool
	}
	if route := strings.TrimSpace(next.Route); route != "" {
		call["route"] = route
	}
	if reason := strings.TrimSpace(next.Reason); reason != "" {
		call["reason"] = reason
	}
	if len(next.Params) > 0 {
		params := make(map[string]any, len(next.Params))
		for key, value := range next.Params {
			params[key] = value
		}
		call["params"] = params
	}
	return call
}
