package telemetry

const (
	// SpanJiraObserve wraps one claimed Jira work-item evidence observation
	// from workflow claim through source fact envelope production.
	SpanJiraObserve = "jira.observe"
	// SpanJiraFetch wraps one bounded Jira Cloud REST fetch.
	SpanJiraFetch = "jira.fetch"
)
