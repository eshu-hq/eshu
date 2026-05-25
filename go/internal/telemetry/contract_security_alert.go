package telemetry

const (
	// SpanSecurityAlertObserve wraps one claimed hosted provider security-alert
	// observation from workflow claim through source fact envelope production.
	SpanSecurityAlertObserve = "security_alert.observe"
	// SpanSecurityAlertFetch wraps one bounded hosted provider alert fetch.
	SpanSecurityAlertFetch = "security_alert.fetch"
)
