// Package main runs the eshu-reducer binary, the long-running runtime that
// drains the reducer fact-work queue, executes domain handlers, materializes
// cross-domain truth, and writes shared edges into the configured graph
// backend.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before runtime setup. Otherwise the binary boots OTEL
// telemetry, opens Postgres and the graph backend,
// wires the reducer service with shared-projection, code-call,
// repo-dependency, and graph-projection-phase repair runners (plus the
// Terraform config-vs-state drift handler wired with the
// PostgresTerraformBackendQuery and PostgresDriftEvidenceLoader adapters),
// and hosts it
// through app.NewHostedWithStatusServer so it exposes the shared `/healthz`,
// `/readyz`, `/metrics`, and `/admin/status` admin surface. NornicDB reducer
// workers default to NumCPU, while Neo4j remains capped lower by default; the
// local-authoritative NornicDB profile also wires a reducer graph-drain gate for
// code-call projection so graph write lanes do not compete unnecessarily.
// SIGINT and SIGTERM trigger clean shutdown through the hosted runtime drain.
package main
