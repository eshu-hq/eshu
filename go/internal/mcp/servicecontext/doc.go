// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicecontexttools defines pure route selection for the MCP
// service-context family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration (the four tool definitions live in the
// sibling `service` package's ContextTools and IntelligenceTools
// constructors), global route fanout, the private serviceContextRoute
// adapter in dispatch_service_selector.go, HTTP dispatch, authorization,
// timeouts, response budgets, envelopes, and telemetry. The query package
// (internal/query for get_service_context, get_service_story, and
// investigate_service; internal/serviceintelhttp for
// get_service_intelligence_report) owns the bounded reads behind each
// /api/v0/services/... and /api/v0/investigations/services/... path. This
// package runs no query and must keep every tool name, request path, query
// key, and body key stable.
//
// The four tools -- get_service_context, get_service_story,
// get_service_intelligence_report, and investigate_service -- share one
// selector model: a caller passes workload_id (get_service_context,
// get_service_story) or falls back to service_name (get_service_story,
// get_service_intelligence_report, investigate_service), and a canonical
// "workload:*" selector is additionally forwarded as the service_id query
// parameter so target-story and investigation readbacks do not fall back to
// name-only service matching. get_service_context and get_service_story
// validate that the selector is non-blank before building a request;
// investigate_service does not, matching the pre-extraction root switch arm
// each replaces. The service catalog tool
// (list_service_catalog_correlations) is deliberately excluded from this
// family even though it lives in the same `service` registration package:
// its routing stays in dispatch_repositories.go and
// dispatch_service_catalog.go, and it shares no selector logic with the four
// tools here. get_workload_context and get_workload_story are also excluded:
// they map workload_id directly to /api/v0/workloads/{id}/... with no
// selector normalization, qualified-identifier stripping, or repository
// fallback, so they share no helper with this package and stay in the root
// switch in dispatch.go.
package servicecontexttools
