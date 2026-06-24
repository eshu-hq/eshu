// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanJiraObserve wraps one claimed Jira work-item evidence observation
	// from workflow claim through source fact envelope production.
	SpanJiraObserve = "jira.observe"
	// SpanJiraFetch wraps one bounded Jira Cloud REST fetch.
	SpanJiraFetch = "jira.fetch"
)

const (
	// SpanAttrJiraSearchPages records bounded Jira issue-search pages scanned
	// during one fetch span.
	SpanAttrJiraSearchPages = "jira.search_pages"
	// SpanAttrJiraChangelogPages records bounded Jira changelog pages scanned
	// during one fetch span.
	SpanAttrJiraChangelogPages = "jira.changelog_pages"
	// SpanAttrJiraRemoteLinkPages records bounded Jira remote-link pages
	// scanned during one fetch span.
	SpanAttrJiraRemoteLinkPages = "jira.remote_link_pages"
	// SpanAttrJiraIssuesEmitted records source issue records accepted for fact
	// emission during one fetch span.
	SpanAttrJiraIssuesEmitted = "jira.issues_emitted"
	// SpanAttrJiraChangelogEventsEmitted records changelog events accepted for
	// fact emission during one fetch span.
	SpanAttrJiraChangelogEventsEmitted = "jira.changelog_events_emitted"
	// SpanAttrJiraRemoteLinksEmitted records remote links accepted for fact
	// emission during one fetch span.
	SpanAttrJiraRemoteLinksEmitted = "jira.remote_links_emitted"
	// SpanAttrJiraRemoteLinksRejected records malformed or unsafe remote links
	// rejected before fact emission during one fetch span.
	SpanAttrJiraRemoteLinksRejected = "jira.remote_links_rejected"
	// SpanAttrJiraUnsupportedProviderLinks records remote links whose provider
	// shape remains unsupported and provenance-only.
	SpanAttrJiraUnsupportedProviderLinks = "jira.unsupported_provider_links"
	// SpanAttrJiraMetadataPages records bounded Jira metadata pages scanned
	// during one fetch span.
	SpanAttrJiraMetadataPages = "jira.metadata_pages"
	// SpanAttrJiraMetadataObjectsScanned records Jira metadata definitions read
	// before redaction and source fact emission.
	SpanAttrJiraMetadataObjectsScanned = "jira.metadata_objects_scanned"
	// SpanAttrJiraMetadataObjectsEmitted records Jira metadata facts and
	// metadata warnings accepted for source fact emission.
	SpanAttrJiraMetadataObjectsEmitted = "jira.metadata_objects_emitted"
	// SpanAttrJiraUnsupportedMetadata records metadata endpoints or definitions
	// that Jira reported as unsupported for the configured target.
	SpanAttrJiraUnsupportedMetadata = "jira.unsupported_metadata"
	// SpanAttrJiraPermissionHiddenMetadata records metadata hidden by Jira
	// permissions so readers can distinguish it from empty metadata.
	SpanAttrJiraPermissionHiddenMetadata = "jira.permission_hidden_metadata"
	// SpanAttrJiraStaleMetadata records metadata collected through a stale
	// source window.
	SpanAttrJiraStaleMetadata = "jira.stale_metadata"
	// SpanAttrJiraMetadataRedactions records private metadata values represented
	// by fingerprints or presence flags instead of raw payload fields.
	SpanAttrJiraMetadataRedactions = "jira.metadata_redactions"
	// SpanAttrJiraPartialFailures records partial Jira fetch failures after
	// accepting some bounded source pages.
	SpanAttrJiraPartialFailures = "jira.partial_failures"
	// SpanAttrJiraRateLimits records Jira rate-limit failures during one fetch
	// span.
	SpanAttrJiraRateLimits = "jira.rate_limits"
	// SpanAttrJiraRetryAfterSeconds records bounded provider retry guidance for
	// rate-limited Jira fetches.
	SpanAttrJiraRetryAfterSeconds = "jira.retry_after_seconds"
	// SpanAttrJiraStaleWindows records fetches whose computed updated window is
	// older than the configured lookback at claim execution time.
	SpanAttrJiraStaleWindows = "jira.stale_windows"
)
