// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package playbook implements the deterministic query-playbook catalog and
// resolver (#6642, split off the #6060 lane A query-root restructure):
// Definition, Catalog, CatalogVersions, ToolNames, Lookup, Resolve, Validate,
// and Handler's two routes (GET /api/v0/query-playbooks -> list,
// POST /api/v0/query-playbooks/resolve -> resolve).
//
// A playbook is data, not executable code: it names the ordered first-class
// tool calls a workflow takes (never raw Cypher), their bounded parameters
// with default limits, the expected AnswerTruthClass and evidence per step,
// optional drilldowns, and the declared failure modes with recommended
// fallbacks. Resolve turns a Definition plus declared inputs into a
// ResolvedPlaybook of fully specified, bounded calls without reading any
// external or live-backend state, so equal inputs always yield an equal
// result. Every response's truth envelope is built against Capability's
// registered ceiling; BuildTruthEnvelope panics if the capability is
// unregistered. The routes read only the in-process catalog, never Postgres
// or a graph backend, so every capability is exact at every profile.
//
// This package imports querycontract (profiles, envelopes, capability
// registration, truth/error contracts, the shared AnswerTruthClass
// taxonomy); it MUST NOT import the query root, or root would cycle back
// through its own compatibility aliases in query_playbook_alias.go, which
// import this package for the type aliases and forwarders that root, the
// staying investigation_workflow family, and the cmd/api and cmd/mcp-server
// wiring still use. See README.md for the file layout and move evidence, and
// AGENTS.md for the per-symbol export rationale.
package playbook
