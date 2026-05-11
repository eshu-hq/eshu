// Package main runs the eshu-bootstrap-index binary, which performs a
// one-shot end-to-end indexing pass: collection, source-local projection,
// relationship-evidence backfill, IaC reachability materialization,
// deployment-mapping reopen, and config_state_drift intent enqueue (Phase 3.5
// trigger for the reducer's Terraform drift handler).
//
// When invoked with --version or -v, it prints the embedded application
// version through the test-covered printBootstrapIndexVersionFlag helper and
// exits before opening stores. Otherwise the binary opens Postgres and the
// graph backend, runs collector and projector goroutines concurrently against
// a Postgres FOR UPDATE SKIP LOCKED queue, and then drives the post-collection
// passes that the facts-first ordering documented in CLAUDE.md requires.
// Projector work superseded by a newer same-scope generation exits that worker
// item without acking stale graph state. Its canonical writer configuration
// uses the same graph-property filtering, NornicDB phase-group policy, and
// row-scoped batched entity containment as the ingester path, so bootstrap and
// steady-state projection keep the same write contract. The binary exits when
// the queue drains; it is not a steady-state runtime and does not mount the
// shared `/healthz`, `/readyz`, or `/admin/status` admin surface.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to 127.0.0.1
// for port-only inputs so the default does not reach beyond the local host.
package main
