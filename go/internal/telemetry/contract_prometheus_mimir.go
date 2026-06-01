package telemetry

const (
	// SpanPrometheusMimirObserve wraps one claimed Prometheus/Mimir
	// observation from workflow claim through observability source fact
	// envelope production.
	SpanPrometheusMimirObserve = "prometheus_mimir.observe"
	// SpanPrometheusMimirFetch wraps one bounded Prometheus-compatible REST
	// metadata fetch.
	SpanPrometheusMimirFetch = "prometheus_mimir.fetch"
)
