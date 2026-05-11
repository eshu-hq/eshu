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
// unless explicitly disabled for fallback comparison. The NornicDB
// canonical entity phases additionally dispatch grouped chunks across a
// bounded worker pool sized by ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY;
// chunks inside one entity label MERGE on disjoint entity_id keys so the
// parallel path is safe. The pool stays open for the lifetime of one
// entity-phase call and consumes chunks from a streaming channel as the
// producer buffers them, so the slowest chunk in one batch no longer
// stalls workers that have already finished their share.
// It is the only
// long-running runtime that mounts the
// workspace PVC in Kubernetes, runs as
// a StatefulSet, and shuts down cleanly on SIGINT or SIGTERM.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to 127.0.0.1
// for port-only inputs so the default does not reach beyond the local host.
package main
