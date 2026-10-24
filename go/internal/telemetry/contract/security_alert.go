// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

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

	// AttrSecurityAlertFanoutRepositories is the span attribute key recording
	// how many allowlisted repositories inside the target organization the
	// per-repository fan-out considered after a truncated org read (#8805).
	// It is a count, never a repository name.
	AttrSecurityAlertFanoutRepositories = "eshu.security_alert.org_fanout.repositories"
	// AttrSecurityAlertFanoutRequested is the span attribute key recording how
	// many bounded per-repository reads the fan-out issued (#8805).
	AttrSecurityAlertFanoutRequested = "eshu.security_alert.org_fanout.requested"
	// AttrSecurityAlertFanoutIncomplete is the span attribute key recording how
	// many fan-out repositories still have incomplete coverage: truncated,
	// failed, rate limited, or skipped at the repository cap (#8805).
	AttrSecurityAlertFanoutIncomplete = "eshu.security_alert.org_fanout.incomplete"
	// AttrSecurityAlertFanoutRateLimited is the span attribute key recording
	// whether the provider rate limited the fan-out and stopped it (#8805).
	AttrSecurityAlertFanoutRateLimited = "eshu.security_alert.org_fanout.rate_limited"
	// AttrSecurityAlertFanoutStopped is the span attribute key recording
	// whether a read failure other than not_found stopped the fan-out early,
	// a rate limit included (#8805).
	AttrSecurityAlertFanoutStopped = "eshu.security_alert.org_fanout.stopped"
)

// Org-scope security-alert fan-out outcomes (#8805), recorded on the existing
// outcome dimension of eshu_dp_security_alert_org_fanout_repositories_total.
// The value set is closed: one value per allowlisted repository per truncated
// org read.
const (
	// SecurityAlertFanoutOutcomeComplete means the repository's own bounded
	// read returned every open alert inside max_pages.
	SecurityAlertFanoutOutcomeComplete = "complete"
	// SecurityAlertFanoutOutcomeTruncated means the repository's own bounded
	// read still hit max_pages, so its facts stay incomplete.
	SecurityAlertFanoutOutcomeTruncated = "truncated"
	// SecurityAlertFanoutOutcomeFailed means the repository's own read failed
	// with a non-rate-limit provider failure. The fan-out continues only after
	// not_found; any other class stops it.
	SecurityAlertFanoutOutcomeFailed = "failed"
	// SecurityAlertFanoutOutcomeRateLimited means the provider rate limited
	// this or an earlier repository read, so the fan-out stopped and the
	// repository was not read.
	SecurityAlertFanoutOutcomeRateLimited = "rate_limited"
	// SecurityAlertFanoutOutcomeSkippedCap means the repository sat past the
	// per-claim fan-out repository cap and was not read.
	SecurityAlertFanoutOutcomeSkippedCap = "skipped_cap"
	// SecurityAlertFanoutOutcomeStopped means an earlier repository read
	// failed with a class other than not_found or rate_limited (auth denied,
	// retryable, terminal), so the fan-out stopped and this repository was not
	// read.
	SecurityAlertFanoutOutcomeStopped = "stopped"
	// SecurityAlertFanoutOutcomeAlertsDisabled means the repository's own read
	// got a 403 whose body says Dependabot alerts are disabled for it. The
	// repository stays incomplete and the fan-out continues, because the answer
	// is specific to that repository and not to the credential.
	SecurityAlertFanoutOutcomeAlertsDisabled = "alerts_disabled"
)
