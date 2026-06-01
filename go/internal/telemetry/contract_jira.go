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
