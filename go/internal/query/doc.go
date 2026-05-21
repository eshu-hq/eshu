// Package query owns Eshu's HTTP read surface and the read models behind API,
// MCP, and CLI query workflows.
//
// The package mounts /api/v0 routes, assembles the static OpenAPI document,
// negotiates the canonical {data, truth, error} response envelope, and enforces
// capability gates for each runtime profile. Handlers read through ports such
// as GraphQuery and ContentStore rather than concrete Neo4j, NornicDB, or
// Postgres drivers, so backend-specific behavior stays behind narrow adapter
// seams.
//
// Handler behavior, OpenAPI fragments, docs/public/reference/http-api.md,
// truth-envelope fields, and MCP tool dispatch must stay aligned whenever a
// public route or response shape changes. Code-quality and dead-code responses
// also preserve language maturity, exactness blockers, modeled roots, and
// source handles so callers can distinguish cleanup-ready findings from
// ambiguous or suppressed evidence.
package query
