// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package auditstore persists validation-safe hosted governance audit events
// (authorization decisions, not a login flow) in a private
// governance_audit_events sink, derives deterministic event ids for
// retry-idempotent Append, and exposes only authorized bounded detailed reads
// (List) plus aggregate summaries (Summary, SummaryForTenant) for the API,
// MCP server, workflow coordinator, and admin CLI status surfaces.
//
// GovernanceAuditStore.Append validates every event with
// governanceaudit.NormalizeEvent before it reaches SQL, so a caller can never
// persist a raw principal, source name, prompt, provider response,
// credential handle, private URL, or token value. List reads back with
// governanceaudit.NormalizeStoredEvent instead: an event_type, actor_class,
// scope_class, or decision value this build's registry does not know is kept
// verbatim rather than failing the page, and List logs one WARN per affected
// field per call (#6574) so an operator can tell a rolling upgrade apart from
// a real problem without a line per row.
//
// GovernanceAuditQuery.TenantID scopes List and SummaryForTenant to one
// tenant; global/NULL-tenant events are visible only to the shared operator
// (no TenantID filter), never to a tenant-admin caller (#3717).
//
// This package must not import the parent postgres package.
package auditstore
