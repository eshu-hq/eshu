// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package replatformingtools defines pure route selection for the MCP
// replatforming-planning family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order (both tools stay at the
// root in tools_iac.go), global route fanout, the private replatformingRoute
// adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, and telemetry. The query package owns the bounded reads and
// scope validation behind each /api/v0/replatforming/... path. This package
// runs no query and must keep every tool name, request path, and body key
// stable.
//
// The two tools — compose_replatforming_plan and get_replatforming_rollups —
// build related but distinct request shapes: the plan carries the full
// scope-selector set (scope_kind, scope_id, account_id, region,
// service_name, workload_id, repo_id, environment, arn, resource_id) because
// it can anchor on a single resource, while the rollup is deliberately
// coarser (scope_id, account_id, region only) because it always summarizes
// across a scope rather than one resource — it never forwards arn even when
// the caller sends one. list_aws_runtime_drift_findings sits next to these
// two tools in the pre-extraction root switch and shares the "not part of
// the IaC-management family" grouping, but it is not part of this family: it
// builds a narrower body against a different path
// (/api/v0/aws/runtime-drift/findings) and stays in the parent's
// dispatch_iac.go.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "25" becomes the default rather than
// an error. Neither tool validates its arguments before building a request —
// scope validation (for example an unsupported or missing scope_kind on the
// plan) happens in the internal/query handler, not here — so Route reports
// only (Request, bool), never an error.
package replatformingtools
