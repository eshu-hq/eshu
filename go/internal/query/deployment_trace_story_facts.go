// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file held the deployment-trace story/fact builders split out of
// deployment_trace_support_helpers.go (#5720). The builders moved to
// internal/query/impacttrace with lane B2 of #6060, which is their only
// caller; firstPositiveFloat keeps a wrapper here for root callers.

// firstPositiveFloat returns the first positive candidate, or zero. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func firstPositiveFloat(candidates ...float64) float64 {
	return querycontract.FirstPositiveFloat(candidates...)
}
