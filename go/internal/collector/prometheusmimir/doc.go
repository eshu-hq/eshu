// Package prometheusmimir collects bounded live Prometheus and Mimir metadata
// as observability source facts.
//
// The package reads configured Prometheus-compatible API targets and emits
// metadata-only observability.source_instance, observability.observed_target,
// observability.observed_rule, and observability.coverage_warning facts. It
// never persists metric samples, exemplars, raw PromQL, scrape target URLs,
// target label values, tenant IDs, tenant secrets, or alert payload bodies.
// Reducers and query surfaces own declared/applied/observed comparison and
// user-facing observability truth.
package prometheusmimir
