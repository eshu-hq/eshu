// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package alerttools defines pure route selection and the aggregate
// tool definitions for the MCP security-alert reconciliation family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns the root registration wrapper and client-visible order,
// global route fanout, the private adapter, HTTP dispatch, authorization,
// timeouts, response budgets, envelopes, summaries, and telemetry. The query
// package owns the bounded
// reads behind the paths: the reducer-owned provider security-alert
// reconciliation list, and its whole-scope count and grouped inventory.
//
// Three tools travel together here. The listing pages by the
// after_reconciliation_id cursor with a limit defaulting to 50 and requires
// one of repository_id, provider, package_id, cve_id, or ghsa_id as a scope
// anchor -- except an empty scoped-token grant, which is answered with an
// empty page before the anchor is checked. The count and the inventory share
// the same seven filters and carry no scope requirement at all; the
// inventory adds a group_by falling back to reconciliation_status, a limit
// defaulting to 100, and an offset defaulting to 0, while the count carries
// no paging key because its handler reads none.
package alerttools
