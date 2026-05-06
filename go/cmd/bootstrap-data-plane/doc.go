// Package main runs the eshu-bootstrap-data-plane binary, which applies the
// Eshu Postgres and graph-backend schema DDL and exits.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before opening stores. Otherwise the binary opens Postgres
// through the runtime config helpers, applies the
// fact-store, queue, content, and audit DDL via postgres.ApplyBootstrap, then
// opens the configured graph backend (Neo4j or NornicDB) and applies the
// schema bootstrap through graph.EnsureSchemaWithBackend. All DDL uses
// CREATE ... IF NOT EXISTS so the binary is idempotent and safe to run as a
// Kubernetes initContainer or Compose `db-migrate` service before the
// long-running runtimes start.
package main
