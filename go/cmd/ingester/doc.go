// Package main runs the eshu-ingester binary, the long-running runtime that
// owns repository sync, parsing, fact emission, and source-local projection
// into the configured graph backend.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before runtime setup. Otherwise the binary boots OTEL
// telemetry, opens Postgres and the canonical graph
// writer, registers queue observable gauges, and hosts the collector +
// projector service through app.NewHostedWithStatusServer so it exposes the
// shared `/healthz`, `/readyz`, `/metrics`, and `/admin/status` admin
// surface together with the `/admin/recovery` route. For local-authoritative
// NornicDB runs, projector workers default to NumCPU unless explicitly
// configured; NornicDB phase grouping keeps canonical retractions outside
// matching upsert groups, directory/file writes stay in separate bounded
// phases, and entity containment is batched into row-scoped entity upserts
// unless explicitly disabled for fallback comparison. It is the only
// long-running runtime that mounts the
// workspace PVC in Kubernetes, runs as
// a StatefulSet, and shuts down cleanly on SIGINT or SIGTERM.
package main
