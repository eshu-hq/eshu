// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanPagerDutyObserve wraps one claimed PagerDuty observation from
	// workflow claim through incident source fact envelope production.
	SpanPagerDutyObserve = "pagerduty.observe"
	// SpanPagerDutyFetch wraps one bounded PagerDuty incident evidence fetch.
	SpanPagerDutyFetch = "pagerduty.fetch"
)
