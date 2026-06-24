// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanSecurityAlertObserve wraps one claimed hosted provider security-alert
	// observation from workflow claim through source fact envelope production.
	SpanSecurityAlertObserve = "security_alert.observe"
	// SpanSecurityAlertFetch wraps one bounded hosted provider alert fetch.
	SpanSecurityAlertFetch = "security_alert.fetch"

	// AttrSecurityAlertTargetScope is the span attribute key recording whether a
	// security-alert observation polled a single repository ("repository") or an
	// organization-wide endpoint ("org") that fans out into per-repository facts.
	// It is a bounded span attribute, not a metric label, so cardinality stays
	// safe while operators can still tell org fan-out work from per-repo polls.
	AttrSecurityAlertTargetScope = "eshu.security_alert.target_scope"
)
