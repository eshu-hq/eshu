// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the eshu-workflow-coordinator binary, the long-running
// runtime that reconciles declarative collector instance state and, in
// active mode, plans supported collector work, hands off webhook freshness
// triggers, reaps expired claims, and recomputes workflow-run completeness.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before runtime setup. Otherwise the binary boots OTEL
// telemetry, opens Postgres, builds coordinator.Service
// from the configured workflow store, governance audit sink, and metrics, and
// hosts it through app.NewHostedWithStatusServer so it exposes the shared `/healthz`,
// `/readyz`, `/metrics`, and `/admin/status` admin surface. Deployment mode
// (dark by default, active when the deployment knobs documented in the
// runtime contract are set) gates scheduled planning, freshness handoff, reap,
// and run-reconciliation loops. When a hosted tenant boundary is configured,
// the binary also wires the tenant grant store so coordinator planning and
// claim completion can reject missing, revoked, or stale scope grants; trigger
// normalization, provider API calls, and collector lease ownership remain
// outside this binary. SIGINT and SIGTERM trigger clean shutdown through the
// hosted runtime drain.
package main
