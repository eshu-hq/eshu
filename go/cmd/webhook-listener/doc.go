// Package main runs the eshu-webhook-listener binary, the public webhook intake
// runtime for GitHub and GitLab repository-refresh triggers.
//
// The runtime verifies provider authentication, normalizes webhook payloads,
// persists trigger decisions in Postgres, and exposes the shared Eshu admin
// surface. Provider delivery identity is required before normalization; GitLab
// delivery identity prefers Idempotency-Key so provider retries dedupe against
// the same durable trigger. The runtime does not mount the repository
// workspace, connect to the graph backend, or mark webhook metadata as graph
// truth.
package main
