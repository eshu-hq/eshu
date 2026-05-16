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
// PostgresTerraformBackendQuery and PostgresDriftEvidenceLoader adapters;
// the loader's PriorConfigDepth field is set from ESHU_DRIFT_PRIOR_CONFIG_DEPTH
// via parsePriorConfigDepth (default 10, 0 means use default, invalid input
// falls back to 0 with a WARN log); the loader receives the runtime tracer
// so its four-input join surfaces as one SpanReducerDriftEvidenceLoad span
// over InstrumentedDB children, plus the runtime *telemetry.Instruments
// handle so the module-aware join in issue #169 can increment
// eshu_dp_drift_unresolved_module_calls_total when a module call's source
// resolves to a registry, git URL, archive, or cross-repo path; the AWS runtime
// drift durable fact writer is wired for issue #39, and the SBOM/attestation
// attachment writer publishes digest-subject attachment facts before
// vulnerability-impact graph work lands),
// and hosts it
// through app.NewHostedWithStatusServer so it exposes the shared `/healthz`,
// `/readyz`, `/metrics`, and `/admin/status` admin surface. NornicDB reducer
// workers default to NumCPU, while Neo4j remains capped lower by default; the
// local-authoritative NornicDB profile also wires a reducer graph-drain gate for
// code-call projection so graph write lanes do not compete unnecessarily.
// SIGINT and SIGTERM trigger clean shutdown through the hosted runtime drain.
//
// When ESHU_PPROF_ADDR is set, the binary also exposes an opt-in
// net/http/pprof endpoint via runtime.NewPprofServer, bound to 127.0.0.1
// for port-only inputs so the default does not reach beyond the local host.
package main
