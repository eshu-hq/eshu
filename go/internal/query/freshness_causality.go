// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type (
	FreshnessCause     = querycontract.FreshnessCause
	FreshnessNextCheck = querycontract.FreshnessNextCheck
)

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

var freshnessCauses = map[FreshnessCause]struct{}{
	FreshnessCausePendingRepoGeneration:      {},
	FreshnessCauseReducerBacklog:             {},
	FreshnessCauseDeadLetteredDomain:         {},
	FreshnessCauseMissingCollectorCompletion: {},
	FreshnessCauseContentCoverageUnavailable: {},
	FreshnessCauseUnsupportedProfile:         {},
	FreshnessCauseRetentionExpired:           {},
	FreshnessCausePendingSearchVector:        {},
}

func ValidFreshnessCause(cause FreshnessCause) bool {
	return querycontract.ValidFreshnessCause(cause)
}

func FreshnessCauseNextCheck(cause FreshnessCause) (FreshnessNextCheck, bool) {
	return querycontract.FreshnessCauseNextCheck(cause)
}

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
