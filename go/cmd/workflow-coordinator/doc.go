// Package main runs the eshu-workflow-coordinator binary, the long-running
// runtime that reconciles declarative collector instance state and, in
// active mode, hands off webhook freshness triggers, reaps expired claims, and
// recomputes workflow-run completeness.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before runtime setup. Otherwise the binary boots OTEL
// telemetry, opens Postgres, builds coordinator.Service
// from the configured store and metrics, and hosts it through
// app.NewHostedWithStatusServer so it exposes the shared `/healthz`,
// `/readyz`, `/metrics`, and `/admin/status` admin surface. Deployment mode
// (dark by default, active when the deployment knobs documented in the
// runtime contract are set) gates freshness handoff, reap, and
// run-reconciliation loops; trigger normalization and collector lease ownership
// remain outside this binary. SIGINT and SIGTERM trigger clean shutdown through
// the hosted runtime drain.
package main
