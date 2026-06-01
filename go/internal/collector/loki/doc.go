// Package loki collects bounded live Loki metadata as observability source
// facts.
//
// The package reads configured Loki API targets and emits metadata-only
// observability.source_instance, observability.observed_log_signal,
// observability.observed_rule, and observability.coverage_warning facts. It
// never persists log lines, raw LogQL, private URLs, label values, tenant IDs,
// tenant secrets, credentials, or provider response bodies. Reducers and query
// surfaces own declared/applied/observed comparison and user-facing
// observability truth.
package loki
