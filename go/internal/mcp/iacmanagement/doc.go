// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iacmanagementtools defines pure route selection for the MCP
// IaC-management family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order (the seven tool
// definitions stay at the root in tools_codebase.go and tools_iac.go),
// global route fanout, the private iacManagementRoute adapter, HTTP
// dispatch, authorization, timeouts, response budgets, envelopes, and
// telemetry. The query package owns the bounded reads behind each
// /api/v0/iac/..., /api/v0/terraform/..., and
// /api/v0/replatforming/ownership-packets path. This package runs no query
// and must keep every tool name, request path, and body key stable.
//
// The seven tools — find_dead_iac, find_unmanaged_resources,
// get_iac_management_status, explain_iac_management_status,
// propose_terraform_import_plan, list_terraform_config_state_drift_findings,
// and find_unmanaged_resource_owners — each build a distinct body shape
// except get_iac_management_status and explain_iac_management_status, which
// share managementStatusBody because both resolve one AWS stable resource
// identity to the same fixed one-item page and differ only in which handler
// renders the result. The sibling replatforming tools
// (compose_replatforming_plan, list_aws_runtime_drift_findings, and
// get_replatforming_rollups) are deliberately excluded from this family:
// each builds its body from its own root helper that no tool in this
// package shares.
//
// Numeric coercion follows routecontract.Arguments: int, int64, and float64
// are honoured, a float64 truncates toward zero, and every other type falls
// back to the default, so a stringified "100" becomes the default rather
// than an error.
package iacmanagementtools
