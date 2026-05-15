// Package main runs the eshu-webhook-listener binary, the public webhook intake
// runtime for GitHub, GitLab, Bitbucket, and AWS freshness triggers.
//
// The runtime verifies provider authentication, normalizes webhook payloads,
// persists trigger decisions in Postgres, and exposes the shared Eshu admin
// surface. Provider delivery identity is required before normalization; GitLab
// delivery identity prefers Idempotency-Key so provider retries dedupe against
// the same durable trigger, and Bitbucket delivery identity prefers
// X-Request-UUID with X-Hub-Signature verification. The runtime does not mount
// the repository workspace, connect to the graph backend, or mark webhook
// metadata as graph truth. AWS EventBridge and AWS Config deliveries are
// normalized into service-tuple wake-up triggers and never write graph truth
// directly. Request body handling returns 413 only when MaxRequestBodyBytes is
// exceeded; other body read failures are rejected as bad requests. Provider
// intake records bounded OTEL counters, histograms, and spans through
// telemetry.Instruments and telemetry.SpanWebhookHandle/
// telemetry.SpanWebhookStore. Provider, event kind, decision, status, outcome,
// reason, AWS freshness kind, and AWS freshness action are bounded metric
// labels; repository, delivery identity, resource names, and ARNs stay out of
// metrics.
package main
