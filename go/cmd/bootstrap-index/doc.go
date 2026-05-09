// Package main runs the eshu-bootstrap-index binary, which performs a
// one-shot end-to-end indexing pass: collection, source-local projection,
// relationship-evidence backfill, deployment-mapping reopen, and IaC
// reachability materialization.
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
package main
